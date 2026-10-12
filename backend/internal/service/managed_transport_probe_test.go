package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestManagedTransportProbe(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"present", `{"messages":[{"role":"system","content":"规则\n你好"}]}`, "text_presence_verified"},
		{"missing", `{"messages":[{"role":"user","content":"hello"}]}`, "expected_text_not_found"},
		{"tool", `{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"arguments":"规则\n你好"}}]}]}`, "expected_text_not_found"},
		{"bounded", strings.Repeat("x", 128*1024+1), "unverified_size_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewManagedTransportProbe([]PromptInjectionRule{{Action: "replace_system", Content: "规则\n你好"}})
			r, _ := http.NewRequestWithContext(WithManagedTransportProbe(context.Background(), p), "POST", "https://example.com/v1/chat/completions", strings.NewReader(tc.body))
			ObserveManagedTransport(r)
			if p.Status() != tc.want {
				t.Fatalf("got %s want %s", p.Status(), tc.want)
			}
			live, _ := io.ReadAll(r.Body)
			if string(live) != tc.body {
				t.Fatal("live request body changed")
			}
		})
	}
}

func TestManagedTransportProbeMissingReplayBody(t *testing.T) {
	p := NewManagedTransportProbe([]PromptInjectionRule{{Content: "hello"}})
	r, _ := http.NewRequestWithContext(WithManagedTransportProbe(context.Background(), p), "POST", "https://example.com/v1/responses", io.NopCloser(strings.NewReader(`{}`)))
	ObserveManagedTransport(r)
	if p.Status() != "body_unavailable" {
		t.Fatal(p.Status())
	}
}
