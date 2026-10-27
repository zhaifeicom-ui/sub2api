package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

var channelQualitySecretPattern = regexp.MustCompile(`(?i)(?:sk-|ghp_|gho_)[a-z0-9_-]+`)

func redactChannelQualityError(message string) string {
	message = channelQualitySecretPattern.ReplaceAllString(sanitizeUpstreamErrorMessage(message), "***")
	runes := []rune(strings.TrimSpace(message))
	if len(runes) > 180 {
		runes = runes[:180]
	}
	if len(runes) == 0 {
		return ""
	}
	return "：" + string(runes)
}

func channelQualityErrorMessage(code string, err error) string {
	switch code {
	case "timeout":
		return "检测超过 10 分钟，请检查上游负载后重试；不会自动重复调用"
	case "stream_truncated":
		return "上游生成流提前结束，尚未完成 HTML；请检查上游超时设置"
	case "empty_output":
		return "上游结束了请求，但没有返回 HTML 正文（思考内容不作为检测作品）"
	case "token_limit":
		return "上游达到输出 Token 上限，HTML 可能未完成"
	case "upstream_http", "upstream_stream_error":
		if err != nil {
			return "上游请求失败：" + err.Error()
		}
	}
	return qualityErrorMessage(code)
}

// Reads only visible text. Reasoning and tool arguments cannot become an HTML
// artifact. The browser polls the stored job, not this upstream connection.
func readChannelQualityStream(ctx context.Context, body io.Reader, mode string) (string, int64, int64, string, error) {
	limited := &io.LimitedReader{R: body, N: 16 * 1024 * 1024}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), qualityMaxResponseBytes)
	var text strings.Builder
	var frame strings.Builder
	var input, output int64
	terminal, completed := false, false
	code := ""
	appendText := func(value string) error {
		if text.Len()+len(value) > qualityMaxResponseBytes {
			code = "response_too_large"
			return fmt.Errorf("text limit exceeded")
		}
		_, _ = text.WriteString(value)
		return nil
	}
	readUsage := func(usage gjson.Result, responses bool) {
		inPath, outPath := "prompt_tokens", "completion_tokens"
		if responses {
			inPath, outPath = "input_tokens", "output_tokens"
		}
		if value := usage.Get(inPath); value.Exists() {
			input = value.Int()
		}
		if value := usage.Get(outPath); value.Exists() {
			output = value.Int()
		}
	}
	consume := func(payload string) error {
		if payload == "[DONE]" {
			terminal = true
			if mode != MonitorAPIModeResponses {
				completed = true
			}
			return nil
		}
		if !gjson.Valid(payload) {
			code = "invalid_response"
			return fmt.Errorf("invalid SSE JSON")
		}
		root := gjson.Parse(payload)
		if root.Get("error").Exists() || root.Get("type").String() == "response.failed" || root.Get("type").String() == "error" {
			code = "upstream_stream_error"
			message := root.Get("error.message").String()
			if message == "" {
				message = root.Get("response.error.message").String()
			}
			return fmt.Errorf("生成失败%s", redactChannelQualityError(message))
		}
		if mode == MonitorAPIModeResponses {
			readUsage(root.Get("response.usage"), true)
			readUsage(root.Get("usage"), true)
			switch root.Get("type").String() {
			case "response.output_text.delta":
				return appendText(root.Get("delta").String())
			case "response.completed", "response.done":
				status := root.Get("response.status").String()
				if status != "" && status != "completed" {
					code = "invalid_response"
					return fmt.Errorf("incomplete response")
				}
				if text.Len() == 0 {
					for _, item := range root.Get("response.output").Array() {
						if item.Get("type").String() != "message" {
							continue
						}
						for _, part := range item.Get("content").Array() {
							if part.Get("type").String() == "output_text" {
								if err := appendText(part.Get("text").String()); err != nil {
									return err
								}
							}
						}
					}
				}
				terminal, completed = true, true
			case "response.incomplete":
				code = "token_limit"
				return fmt.Errorf("incomplete generation")
			}
			return nil
		}
		readUsage(root.Get("usage"), false)
		for _, choice := range root.Get("choices").Array() {
			if choice.Get("index").Int() != 0 {
				continue
			}
			if err := appendText(choice.Get("delta.content").String()); err != nil {
				return err
			}
			if reason := choice.Get("finish_reason").String(); reason != "" {
				if reason == "stop" {
					completed = true
				} else {
					code = "token_limit"
				}
			}
		}
		return nil
	}
	flushFrame := func() error {
		if frame.Len() == 0 {
			return nil
		}
		payload := strings.TrimSuffix(frame.String(), "\n")
		frame.Reset()
		return consume(payload)
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return text.String(), input, output, "timeout", err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flushFrame(); err != nil {
				return text.String(), input, output, code, err
			}
			if terminal {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			if frame.Len()+len(value) > qualityMaxResponseBytes {
				return text.String(), input, output, "response_too_large", fmt.Errorf("event limit exceeded")
			}
			_, _ = frame.WriteString(value)
			_ = frame.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			code = "timeout"
		} else {
			code = "stream_truncated"
		}
		return text.String(), input, output, code, err
	}
	if err := flushFrame(); err != nil {
		return text.String(), input, output, code, err
	}
	if limited.N <= 0 {
		return text.String(), input, output, "response_too_large", fmt.Errorf("stream limit exceeded")
	}
	if code != "" {
		return text.String(), input, output, code, fmt.Errorf("generation incomplete")
	}
	if !completed {
		return text.String(), input, output, "stream_truncated", fmt.Errorf("missing completion event")
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", input, output, "empty_output", fmt.Errorf("no visible output")
	}
	return text.String(), input, output, "", nil
}
