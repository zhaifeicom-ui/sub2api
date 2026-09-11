package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchingPromptInjectionRules(t *testing.T) {
	groupID := int64(7)
	config := PromptInjectionConfig{Rules: []PromptInjectionRule{
		{ID: "account", Enabled: true, Scope: "account", TargetID: 9, Models: []string{"gpt-5*"}, Priority: 20},
		{ID: "group", Enabled: true, Scope: "group", TargetID: 7, Priority: 20},
		{ID: "other", Enabled: true, Scope: "group", TargetID: 8},
	}}
	got := matchingPromptInjectionRules(config, &groupID, 9, "GPT-5.4")
	require.Len(t, got, 2)
	require.Equal(t, "group", got[0].ID)
	require.Equal(t, "account", got[1].ID)
}

func TestMatchingPromptInjectionRulesSupportsMultipleGroups(t *testing.T) {
	groupID := int64(12)
	config := PromptInjectionConfig{Rules: []PromptInjectionRule{
		{ID: "multi", Enabled: true, Scope: "group", GroupIDs: []int64{7, 12}, TargetID: 7},
	}}

	got := matchingPromptInjectionRules(config, &groupID, 1, "any-model")
	require.Len(t, got, 1)
	require.Equal(t, "multi", got[0].ID)
}

func TestApplyPromptInjectionsChatPositions(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"old"},{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)
	rules := []PromptInjectionRule{
		{Role: "developer", Position: "prepend", Content: "p"},
		{Role: "system", Position: "before_first_user", Content: "b"},
		{Role: "user", Position: "after_last_user", Content: "a"},
	}
	got, err := ApplyPromptInjections(body, "chat_completions", rules)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(got, &root))
	messages := root["messages"].([]any)
	require.Len(t, messages, 6)
	require.Equal(t, "p", messages[0].(map[string]any)["content"])
	require.Equal(t, "b", messages[2].(map[string]any)["content"])
	require.Equal(t, "a", messages[4].(map[string]any)["content"])
}

func TestApplyPromptInjectionsResponsesStringInput(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":"hello"}`)
	rules := []PromptInjectionRule{{Role: "developer", Position: "before_first_user", Content: "policy"}}
	got, err := ApplyPromptInjections(body, "responses", rules)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(got, &root))
	input := root["input"].([]any)
	require.Equal(t, "policy", input[0].(map[string]any)["content"])
	require.Equal(t, "hello", input[1].(map[string]any)["content"])
}

func TestApplyPromptInjectionsNoRulesPreservesBytes(t *testing.T) {
	body := []byte(" {\n  \"input\": \"hello\"\n} ")
	got, err := ApplyPromptInjections(body, "responses", nil)
	require.NoError(t, err)
	require.Equal(t, body, got)
}

func TestApplyPromptInjectionsResponsesArrayInput(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	rules := []PromptInjectionRule{{Role: "system", Position: "after_last_user", Content: "policy"}}
	got, err := ApplyPromptInjections(body, "responses", rules)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(got, &root))
	input := root["input"].([]any)
	require.Len(t, input, 2)
	require.Equal(t, "user", input[0].(map[string]any)["role"])
	require.Equal(t, "policy", input[1].(map[string]any)["content"])
}
