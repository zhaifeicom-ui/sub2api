package service

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Literal, non-cascading replacement. Only a partial keyword is buffered.
type ManagedTextReplacer struct {
	rules   []PromptInjectionRule
	pending string
	limited bool
}

func NewManagedTextReplacer(rules []PromptInjectionRule) *ManagedTextReplacer {
	out := []PromptInjectionRule{}
	for _, r := range rules {
		if r.Action == "replace_output" && r.Match != "" {
			out = append(out, r)
		}
	}
	return &ManagedTextReplacer{rules: out}
}
func (r *ManagedTextReplacer) Feed(text string, final bool) string {
	if r.limited {
		return text
	}
	r.pending += text
	original := r.pending
	var out strings.Builder
	for r.pending != "" {
		matched, partial := false, false
		for _, rule := range r.rules {
			if strings.HasPrefix(r.pending, rule.Match) {
				if out.Len()+len(rule.Content) > 256*1024 {
					r.pending = ""
					r.limited = true
					return original
				}
				_, _ = out.WriteString(rule.Content)
				r.pending = r.pending[len(rule.Match):]
				matched = true
				break
			}
			if !final && strings.HasPrefix(rule.Match, r.pending) {
				partial = true
				break
			}
		}
		if matched {
			continue
		}
		if partial && !final {
			break
		}
		_, n := utf8.DecodeRuneInString(r.pending)
		if out.Len()+n > 256*1024 {
			r.pending = ""
			r.limited = true
			return original
		}
		_, _ = out.WriteString(r.pending[:n])
		r.pending = r.pending[n:]
	}
	return out.String()
}
func (r *ManagedTextReplacer) Limited() bool { return r.limited }
func ReplaceManagedOutputText(text string, rules []PromptInjectionRule) string {
	return NewManagedTextReplacer(rules).Feed(text, true)
}

// Explicit visible output fields only; tools, reasoning and usage are untouched.
func ManagedOutputJSON(data []byte, rules []PromptInjectionRule) []byte {
	return managedOutputJSON(data, rules, 0)
}
func managedOutputJSON(data []byte, rules []PromptInjectionRule, depth int) []byte {
	if depth > 3 {
		return data
	}
	if !gjson.ValidBytes(data) {
		return data
	}
	original := data
	var paths []string
	root := gjson.ParseBytes(data)
	for i, choice := range root.Get("choices").Array() {
		if choice.Get("message.content").Type == gjson.String {
			paths = append(paths, "choices."+strconv.Itoa(i)+".message.content")
		}
	}
	addContent := func(base string, content gjson.Result) {
		for i, p := range content.Array() {
			if p.Get("type").String() == "text" || p.Get("type").String() == "output_text" {
				paths = append(paths, base+"."+strconv.Itoa(i)+".text")
			}
		}
	}
	if root.Get("role").String() == "assistant" {
		addContent("content", root.Get("content"))
	}
	if root.Get("type").String() == "response.output_text.done" {
		paths = append(paths, "text")
	}
	if kind := root.Get("type").String(); (kind == "response.content_part.done" || kind == "response.content_part.added") && root.Get("part.type").String() == "output_text" {
		paths = append(paths, "part.text")
	}
	if root.Get("content_block.type").String() == "text" {
		paths = append(paths, "content_block.text")
	}
	for i, item := range root.Get("output").Array() {
		if item.Get("type").String() == "message" && item.Get("role").String() == "assistant" {
			addContent("output."+strconv.Itoa(i)+".content", item.Get("content"))
		}
	}
	if root.Get("response").Exists() {
		next := managedOutputJSON([]byte(root.Get("response").Raw), rules, depth+1)
		if updated, err := sjson.SetRawBytes(data, "response", next); err == nil {
			data = updated
		}
	}
	if item := root.Get("item"); item.Get("type").String() == "message" && item.Get("role").String() == "assistant" {
		addContent("item.content", item.Get("content"))
	}
	if len(paths) > 64 {
		return original
	}
	for _, path := range paths {
		v := gjson.GetBytes(data, path)
		if v.Type == gjson.String {
			next, err := sjson.SetBytes(data, path, ReplaceManagedOutputText(v.String(), rules))
			if err == nil {
				if len(next) > 4*1024*1024 {
					return original
				}
				data = next
			}
		}
	}
	return data
}
func ManagedDeltaPath(data []byte) string {
	r := gjson.ParseBytes(data)
	if r.Get("choices.0.delta.content").Type == gjson.String {
		return "choices.0.delta.content"
	}
	if r.Get("type").String() == "response.output_text.delta" && r.Get("delta").Type == gjson.String {
		return "delta"
	}
	if r.Get("type").String() == "content_block_delta" && r.Get("delta.type").String() == "text_delta" {
		return "delta.text"
	}
	if r.Get("type").String() == "content_block_start" && r.Get("content_block.type").String() == "text" {
		return "content_block.text"
	}
	return ""
}
