package service

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"os"
	"strconv"
	"strings"
)

const ccEstimatedInputContextKey = "cc_estimated_input_tokens"

func ccChunkOutputUnits(chunk *apicompat.ChatCompletionsChunk) int {
	units := 0
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil {
			units += ccTextUnits(*choice.Delta.Content)
		}
		if choice.Delta.ReasoningContent != nil {
			units += ccTextUnits(*choice.Delta.ReasoningContent)
		} else if choice.Delta.Reasoning != nil {
			units += ccTextUnits(*choice.Delta.Reasoning)
		}
		for _, call := range choice.Delta.ToolCalls {
			units += ccTextUnits(call.Function.Arguments)
		}
	}
	return units
}

// Explicit, deployment-controlled opt-in. Never estimate for other groups.
func ccUsageEstimateEnabled(groupID int64) bool {
	for _, id := range strings.Split(os.Getenv("SUB2API_ESTIMATED_USAGE_GROUP_IDS"), ",") {
		if strings.TrimSpace(id) == strconv.FormatInt(groupID, 10) {
			return true
		}
	}
	return false
}

// Count text without retaining the request or output. ASCII ~4 chars/token;
// non-ASCII ~1 char/token. This is not an authoritative model tokenizer.
func ccTextUnits(text string) int {
	n := 0
	for _, r := range text {
		if r < 128 {
			n++
		} else {
			n += 4
		}
	}
	return n
}

func estimateCCInput(body []byte) int {
	var req map[string]json.RawMessage
	if json.Unmarshal(body, &req) != nil {
		return 0
	}
	units := 0
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			units += ccTextUnits(x)
		case []any:
			for _, child := range x {
				walk(child)
			}
		case map[string]any:
			for key, child := range x {
				// Never tokenize image/audio payloads, URLs, or identifiers.
				if key != "image_url" && key != "input_audio" && key != "data" && key != "url" && key != "id" && key != "tool_call_id" {
					walk(child)
				}
			}
		}
	}
	for _, key := range []string{"messages", "tools"} {
		var value any
		if json.Unmarshal(req[key], &value) == nil {
			walk(value)
		}
	}
	return (units + 3) / 4
}
