package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// chatToolSequenceEvent is deliberately independent of message indexes. Prompt
// injection inserts ordinary messages, so indexes can change while the tool
// protocol sequence must remain byte-for-byte equivalent at the semantic level.
type chatToolSequenceEvent struct {
	Kind string
	ID   string
}

type chatToolSequenceMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	ToolCalls  []struct {
		ID string `json:"id"`
	} `json:"tool_calls"`
}

type chatToolSequenceRequest struct {
	Messages []chatToolSequenceMessage `json:"messages"`
}

// validateChatToolSequence rejects histories that strict OpenAI-compatible
// providers (notably DeepSeek) report as tool_call_sequence_broken. It never
// repairs or removes client messages: a malformed history is returned to the
// client as a precise 400 instead of being forwarded as an opaque upstream
// failure.
func validateChatToolSequence(body []byte) ([]chatToolSequenceEvent, error) {
	var request chatToolSequenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("invalid chat completions JSON: %w", err)
	}

	pending := make(map[string]int)
	events := make([]chatToolSequenceEvent, 0)
	for messageIndex, message := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "tool" && len(pending) > 0 {
			return nil, fmt.Errorf("messages[%d] starts a new %s message before %s", messageIndex, roleOrUnknown(role), pendingToolCallSummary(pending))
		}

		switch role {
		case "assistant":
			for toolIndex, toolCall := range message.ToolCalls {
				id := strings.TrimSpace(toolCall.ID)
				if id == "" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].id is required", messageIndex, toolIndex)
				}
				if firstIndex, duplicate := pending[id]; duplicate {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].id %q duplicates a pending tool call from messages[%d]", messageIndex, toolIndex, id, firstIndex)
				}
				pending[id] = messageIndex
				events = append(events, chatToolSequenceEvent{Kind: "call", ID: id})
			}
		case "tool":
			id := strings.TrimSpace(message.ToolCallID)
			if id == "" {
				return nil, fmt.Errorf("messages[%d].tool_call_id is required", messageIndex)
			}
			if _, ok := pending[id]; !ok {
				return nil, fmt.Errorf("messages[%d].tool_call_id %q does not match a pending assistant tool call", messageIndex, id)
			}
			delete(pending, id)
			events = append(events, chatToolSequenceEvent{Kind: "result", ID: id})
		}
	}

	if len(pending) > 0 {
		return nil, fmt.Errorf("conversation ends before %s", pendingToolCallSummary(pending))
	}
	return events, nil
}

func roleOrUnknown(role string) string {
	if role == "" {
		return "unknown-role"
	}
	return role
}

func pendingToolCallSummary(pending map[string]int) string {
	if len(pending) == 1 {
		for id := range pending {
			return fmt.Sprintf("tool result for %q", id)
		}
	}
	return fmt.Sprintf("all %d pending tool results", len(pending))
}

func sameChatToolSequence(left, right []chatToolSequenceEvent) bool {
	return reflect.DeepEqual(left, right)
}
