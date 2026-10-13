package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

func TestChannelQualityStreamProtocols(t *testing.T) {
	chat := "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"ignored\"}}]}\r\n\r\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"<svg>\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"</svg>\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":34}}\n\ndata: [DONE]"
	responses := "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"ignored\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"<svg></svg>\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"<svg></svg>\"}]}],\"usage\":{\"input_tokens\":12,\"output_tokens\":34}}}\n\n"
	for _, tc := range []struct{ mode, body string }{{MonitorAPIModeChatCompletions, chat}, {MonitorAPIModeResponses, responses}} {
		text, in, out, code, err := readChannelQualityStream(context.Background(), strings.NewReader(tc.body), tc.mode)
		if err != nil || code != "" || text != "<svg></svg>" || in != 12 || out != 34 {
			t.Fatalf("mode=%s text=%q usage=%d/%d code=%s err=%v", tc.mode, text, in, out, code, err)
		}
	}
}

func TestChannelQualityStreamRejectsMissingAnswerAndTruncation(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"analysis\"}}]}\n\ndata: [DONE]\n\n", "empty_output"},
		{"data: {\"choices\":[{\"delta\":{\"content\":\"<svg>\"}}]}\n\n", "stream_truncated"},
		{"data: {\"choices\":[{\"delta\":{\"content\":\"<svg>\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", "token_limit"},
	} {
		_, _, _, code, err := readChannelQualityStream(context.Background(), strings.NewReader(tc.body), MonitorAPIModeChatCompletions)
		if err == nil || code != tc.code {
			t.Fatalf("got=%s want=%s err=%v", code, tc.code, err)
		}
	}
}

func TestChannelQualityUsesStreamingWithTenMinuteBudget(t *testing.T) {
	if channelQualityTimeout <= 167080*time.Millisecond {
		t.Fatal("timeout cannot fit a 167-second generation")
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"<svg></svg>\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}}
	s := &ChannelQualityService{http: upstream}
	m := &ChannelMonitor{ID: 11, Provider: "openai", Endpoint: "https://upstream.example/v1", APIKey: "test-secret", APIMode: MonitorAPIModeChatCompletions}
	_, _, _, code, err := s.call(context.Background(), m, "gpt-6.1-sol")
	if err != nil || code != "" {
		t.Fatalf("code=%s err=%v", code, err)
	}
	if !gjson.GetBytes(upstream.lastBody, "stream").Bool() || gjson.GetBytes(upstream.lastBody, "model").String() != "gpt-6.1-sol" || upstream.lastReq.Header.Get("Accept") != "text/event-stream" {
		t.Fatal("probe did not request the corrected streaming model")
	}
	if upstream.lastReq.URL.Path != "/v1/chat/completions" {
		t.Fatal("incorrect endpoint")
	}
	if isChannelQualityModel("gpt-6-sol") {
		t.Fatal("legacy model still selectable for new checks")
	}
}

func TestChannelQualityHTTPFailureKeepsStatusAndRedactsKey(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 504, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"test-secret and sk-abcdef timed out"}}`))}}
	s := &ChannelQualityService{http: upstream}
	m := &ChannelMonitor{ID: 11, Provider: "openai", Endpoint: "https://upstream.example/v1", APIKey: "test-secret", APIMode: MonitorAPIModeChatCompletions}
	_, _, _, code, err := s.call(context.Background(), m, "gpt-6.1-sol")
	message := channelQualityErrorMessage(code, err)
	if code != "upstream_http" || !strings.Contains(message, "HTTP 504") || strings.Contains(message, "test-secret") || strings.Contains(message, "sk-abcdef") {
		t.Fatalf("unsafe or unhelpful error: %s", message)
	}
}
