package service

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// chatToolPlaceholderStreamFilter delays punctuation-only assistant content
// until the response proves whether it is a tool call. If a later delta emits
// tool_calls, the placeholder is discarded; otherwise it is released unchanged.
// This avoids leaking provider placeholders such as content:"." into clients
// without hiding a legitimate punctuation-only text response.
type chatToolPlaceholderStreamFilter struct {
	pending       []string
	toolCallSeen  bool
	dropNextBlank bool
}

func (f *chatToolPlaceholderStreamFilter) Process(line string) []string {
	placeholder, toolCall, terminal := classifyChatToolPlaceholderSSELine(line)

	if toolCall && placeholder {
		line = stripPlaceholderContentFromToolCallSSELine(line)
		placeholder = false
	}

	if f.toolCallSeen {
		if placeholder {
			f.dropNextBlank = true
			return nil
		}
		if line == "" && f.dropNextBlank {
			f.dropNextBlank = false
			return nil
		}
		f.dropNextBlank = false
		return []string{line}
	}

	if len(f.pending) == 0 {
		if placeholder {
			f.pending = append(f.pending, line)
			f.dropNextBlank = true
			return nil
		}
		if toolCall {
			f.toolCallSeen = true
		}
		return []string{line}
	}

	if line == "" && f.dropNextBlank {
		f.pending = append(f.pending, line)
		f.dropNextBlank = false
		return nil
	}
	if placeholder {
		f.pending = append(f.pending, line)
		f.dropNextBlank = true
		return nil
	}

	if toolCall {
		f.pending = nil
		f.toolCallSeen = true
		f.dropNextBlank = false
		return []string{line}
	}
	if terminal || chatSSELineHasSubstantiveContent(line) {
		out := f.Flush()
		return append(out, line)
	}
	// Role, reasoning and usage deltas can be passed through immediately. Only
	// the tiny placeholder event itself is retained, so a provider cannot make
	// the gateway buffer an unbounded reasoning stream while deciding to call a
	// tool.
	return []string{line}
}

func (f *chatToolPlaceholderStreamFilter) Flush() []string {
	if len(f.pending) == 0 {
		return nil
	}
	out := make([]string, 0, len(f.pending))
	out = append(out, f.pending...)
	f.pending = nil
	f.dropNextBlank = false
	return out
}

func classifyChatToolPlaceholderSSELine(line string) (placeholder, toolCall, terminal bool) {
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok {
		return false, false, false
	}
	payload = strings.TrimSpace(payload)
	if payload == "[DONE]" {
		return false, false, true
	}
	if payload == "" || !gjson.Valid(payload) {
		return false, false, false
	}
	choices := gjson.Get(payload, "choices")
	if !choices.IsArray() {
		return false, false, false
	}
	hasContent := false
	allPlaceholder := true
	for _, choice := range choices.Array() {
		if calls := choice.Get("delta.tool_calls"); calls.IsArray() && len(calls.Array()) > 0 {
			toolCall = true
		}
		if finish := strings.TrimSpace(choice.Get("finish_reason").String()); finish != "" {
			terminal = true
		}
		content := choice.Get("delta.content")
		if content.Exists() && content.Type == gjson.String {
			hasContent = true
			if !isToolPlaceholderContent(content.String()) {
				allPlaceholder = false
			}
		}
	}
	return hasContent && allPlaceholder, toolCall, terminal
}

func chatSSELineHasSubstantiveContent(line string) bool {
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok || !gjson.Valid(payload) {
		return false
	}
	for _, choice := range gjson.Get(payload, "choices").Array() {
		content := choice.Get("delta.content")
		if content.Exists() && content.Type == gjson.String && !isToolPlaceholderContent(content.String()) {
			return true
		}
	}
	return false
}

func isToolPlaceholderContent(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	for _, r := range content {
		if unicode.IsSpace(r) {
			continue
		}
		switch r {
		case '.', '。', '…', '·':
		default:
			return false
		}
	}
	return true
}

func stripPlaceholderContentFromToolCallSSELine(line string) string {
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok {
		return line
	}
	updated := []byte(payload)
	changed := false
	for index, choice := range gjson.Get(payload, "choices").Array() {
		content := choice.Get("delta.content")
		calls := choice.Get("delta.tool_calls")
		if !content.Exists() || content.Type != gjson.String || !isToolPlaceholderContent(content.String()) || !calls.IsArray() || len(calls.Array()) == 0 {
			continue
		}
		next, err := sjson.DeleteBytes(updated, "choices."+strconv.Itoa(index)+".delta.content")
		if err != nil {
			return line
		}
		updated = next
		changed = true
	}
	if !changed {
		return line
	}
	prefixLen := len(line) - len(payload)
	if prefixLen < 0 {
		return line
	}
	return line[:prefixLen] + string(updated)
}

// stripPlaceholderContentFromToolResponse handles non-streaming Chat
// Completions responses. Only choices that actually contain tool_calls are
// touched; ordinary text replies containing punctuation remain unchanged.
func stripPlaceholderContentFromToolResponse(body []byte) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		return body, false
	}
	updated := body
	changed := false
	for index, choice := range gjson.GetBytes(body, "choices").Array() {
		content := choice.Get("message.content")
		calls := choice.Get("message.tool_calls")
		if !content.Exists() || content.Type != gjson.String || !isToolPlaceholderContent(content.String()) || !calls.IsArray() || len(calls.Array()) == 0 {
			continue
		}
		next, err := sjson.SetBytes(updated, "choices."+strconv.Itoa(index)+".message.content", nil)
		if err != nil {
			return body, false
		}
		updated = next
		changed = true
	}
	return updated, changed
}
