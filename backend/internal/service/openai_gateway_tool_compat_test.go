//go:build unit

package service

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestValidateChatToolSequenceParallelResults(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"go"},{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}},{"id":"call_b","type":"function","function":{"name":"b","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_b","content":"b"},{"role":"tool","tool_call_id":"call_a","content":"a"},{"role":"assistant","content":"done"}]}`)

	events, err := validateChatToolSequence(body)
	require.NoError(t, err)
	require.Equal(t, []chatToolSequenceEvent{
		{Kind: "call", ID: "call_a"},
		{Kind: "call", ID: "call_b"},
		{Kind: "result", ID: "call_b"},
		{Kind: "result", ID: "call_a"},
	}, events)
}

func TestValidateChatToolSequenceRejectsOrphanResult(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"go"},{"role":"tool","tool_call_id":"missing","content":"x"}]}`)

	_, err := validateChatToolSequence(body)
	require.ErrorContains(t, err, `tool_call_id "missing" does not match`)
}

func TestValidateChatToolSequenceRejectsInterruptedCall(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}}]},{"role":"user","content":"continue"}]}`)

	_, err := validateChatToolSequence(body)
	require.ErrorContains(t, err, `starts a new user message before tool result for "call_a"`)
}

func TestValidateChatToolSequenceRejectsUnfinishedCall(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}}]}]}`)

	_, err := validateChatToolSequence(body)
	require.ErrorContains(t, err, `conversation ends before tool result for "call_a"`)
}

func TestValidateChatToolSequenceAllowsIDReuseAfterCompletedRound(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_a","content":"one"},{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_a","content":"two"}]}`)

	_, err := validateChatToolSequence(body)
	require.NoError(t, err)
}

func TestPromptInjectionPreservesToolSequenceAndStableLongPrefix(t *testing.T) {
	longPrompt := strings.Repeat("stable-policy-", 1024)
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"system","content":"client-system"},{"role":"user","content":"go"},{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_a","content":"ok"},{"role":"assistant","content":"done"}]}`)
	rules := []PromptInjectionRule{{ID: "managed", Role: "system", Position: "before_first_user", Content: longPrompt}}

	before, err := validateChatToolSequence(body)
	require.NoError(t, err)
	first, err := ApplyPromptInjections(body, "chat_completions", rules)
	require.NoError(t, err)
	second, err := ApplyPromptInjections(body, "chat_completions", rules)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, bytes.Count(first, []byte(longPrompt)))
	after, err := validateChatToolSequence(first)
	require.NoError(t, err)
	require.True(t, sameChatToolSequence(before, after))
}

func TestChatToolPlaceholderStreamFilterDropsDotBeforeToolCall(t *testing.T) {
	filter := &chatToolPlaceholderStreamFilter{}
	dot := `data: {"choices":[{"index":0,"delta":{"content":"."}}]}`
	tool := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"a","arguments":""}}]}}]}`

	require.Empty(t, filter.Process(dot))
	require.Empty(t, filter.Process(""))
	out := filter.Process(tool)
	require.Equal(t, []string{tool}, out)
	require.NotContains(t, strings.Join(out, "\n"), `"content":"."`)
	require.Equal(t, []string{""}, filter.Process(""))
}

func TestChatToolPlaceholderStreamFilterRestoresLegitimateDot(t *testing.T) {
	filter := &chatToolPlaceholderStreamFilter{}
	dot := `data: {"choices":[{"index":0,"delta":{"content":"…"}}]}`
	done := `data: [DONE]`

	require.Empty(t, filter.Process(dot))
	require.Empty(t, filter.Process(""))
	require.Equal(t, []string{dot, "", done}, filter.Process(done))
}

func TestChatToolPlaceholderStreamFilterStripsSameChunkOnly(t *testing.T) {
	filter := &chatToolPlaceholderStreamFilter{}
	line := `data: {"choices":[{"index":0,"delta":{"content":"。","tool_calls":[{"index":0,"id":"call_a","function":{"name":"a","arguments":""}}]}}]}`

	out := filter.Process(line)
	require.Len(t, out, 1)
	require.False(t, gjson.Get(strings.TrimPrefix(out[0], "data: "), "choices.0.delta.content").Exists())
	require.Equal(t, "call_a", gjson.Get(strings.TrimPrefix(out[0], "data: "), "choices.0.delta.tool_calls.0.id").String())
}

func TestStripPlaceholderContentFromToolResponse(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":".","tool_calls":[{"id":"call_a","type":"function","function":{"name":"a","arguments":"{}"}}]}},{"message":{"role":"assistant","content":".","finish_reason":"stop"}}]}`)

	updated, changed := stripPlaceholderContentFromToolResponse(body)
	require.True(t, changed)
	require.Equal(t, gjson.Null, gjson.GetBytes(updated, "choices.0.message.content").Type)
	require.Equal(t, ".", gjson.GetBytes(updated, "choices.1.message.content").String())
}
