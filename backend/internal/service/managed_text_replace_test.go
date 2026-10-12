package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestManagedReplacementStreamingEqualsNonstream(t *testing.T) {
	rules := []PromptInjectionRule{{Action: "replace_output", Match: "模型名称", Content: "橡皮泥"}, {Action: "replace_output", Match: "ab", Content: "X"}, {Action: "replace_output", Match: "a", Content: "Y"}}
	for _, text := range []string{"你好模型名称。abab尾巴", "模", "abc", "没有命中", "模型名称模型名称"} {
		r := NewManagedTextReplacer(rules)
		out := ""
		for _, ch := range text {
			out += r.Feed(string(ch), false)
		}
		out += r.Feed("", true)
		require.Equal(t, ReplaceManagedOutputText(text, rules), out)
	}
}
func TestManagedSystemReplacementAndUserTextPreserveTools(t *testing.T) {
	rules := []PromptInjectionRule{{Action: "replace_system", Content: "new", Role: "system", Position: "prepend"}, {Action: "replace_input", Match: "hello", Content: "你好"}}
	data := []byte(`{"instructions":"old instructions","input":[{"role":"system","content":"old"},{"role":"developer","content":"keep developer"},{"role":"user","content":"hello"},{"type":"function_call","call_id":"keep","arguments":"hello"}],"large":9007199254740993}`)
	out, err := ApplyPromptInjections(data, "responses", rules)
	require.NoError(t, err)
	require.Equal(t, "new", gjson.GetBytes(out, "instructions").String())
	require.NotContains(t, string(out), "old")
	require.Contains(t, string(out), "keep developer")
	require.Contains(t, string(out), `"arguments":"hello"`)
	require.Contains(t, string(out), "9007199254740993")
	require.Contains(t, string(out), "你好")
}
func TestManagedWholeInputPreservesHistoryAndImages(t *testing.T) {
	rules := []PromptInjectionRule{{Action: "replace_input", WholeInput: true, Content: "replacement"}}
	raw := []byte(`{"messages":[{"role":"user","content":"history"},{"role":"assistant","content":"reply"},{"role":"user","content":[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}},{"type":"text","text":"second"}]}]}`)
	out, err := ApplyPromptInjections(raw, "chat_completions", rules)
	require.NoError(t, err)
	require.Equal(t, "history", gjson.GetBytes(out, "messages.0.content").String())
	require.Equal(t, "replacement", gjson.GetBytes(out, "messages.2.content.0.text").String())
	require.Equal(t, "https://example.com/a.png", gjson.GetBytes(out, "messages.2.content.1.image_url.url").String())
	require.Equal(t, int64(2), gjson.GetBytes(out, "messages.2.content.#").Int())
}

func TestManagedOutputOnlyChangesAssistantText(t *testing.T) {
	rules := []PromptInjectionRule{{Action: "replace_output", Match: "hello", Content: "你好"}}
	raw := []byte(`{"choices":[{"message":{"role":"assistant","content":"hello","tool_calls":[{"id":"hello","function":{"name":"hello","arguments":"hello"}}]}}],"usage":{"prompt_tokens":10}}`)
	out := ManagedOutputJSON(raw, rules)
	var root map[string]any
	require.NoError(t, json.Unmarshal(out, &root))
	require.Equal(t, "你好", gjson.GetBytes(out, "choices.0.message.content").String())
	require.Equal(t, "hello", gjson.GetBytes(out, "choices.0.message.tool_calls.0.function.arguments").String())
	require.EqualValues(t, 10, gjson.GetBytes(out, "usage.prompt_tokens").Int())
}
