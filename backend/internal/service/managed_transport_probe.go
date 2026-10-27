package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

type managedTransportKey struct{}

// A bounded, read-only diagnostic. It never reads or replaces the live Body.
type ManagedTransportProbe struct {
	mu       sync.Mutex
	expected []string
	status   string
}

func NewManagedTransportProbe(rules []PromptInjectionRule) *ManagedTransportProbe {
	p := &ManagedTransportProbe{status: "not_applicable"}
	for _, r := range rules {
		if (r.Action == "" || r.Action == "append" || r.Action == "replace_system") && r.Content != "" {
			p.expected = append(p.expected, r.Content)
		}
	}
	if len(p.expected) > 0 {
		p.status = "not_observed"
	}
	return p
}

func WithManagedTransportProbe(ctx context.Context, p *ManagedTransportProbe) context.Context {
	return context.WithValue(ctx, managedTransportKey{}, p)
}

func (p *ManagedTransportProbe) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func ObserveManagedTransport(req *http.Request) {
	if req == nil || req.URL == nil || req.Method != http.MethodPost {
		return
	}
	path := strings.TrimRight(req.URL.Path, "/")
	if !strings.HasSuffix(path, "/responses") && !strings.HasSuffix(path, "/chat/completions") && !strings.HasSuffix(path, "/messages") {
		return
	}
	p, _ := req.Context().Value(managedTransportKey{}).(*ManagedTransportProbe)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.expected) == 0 {
		return
	}
	if req.GetBody == nil {
		p.status = "body_unavailable"
		return
	}
	r, err := req.GetBody()
	if err != nil {
		p.status = "body_unavailable"
		return
	}
	defer func() { _ = r.Close() }()
	const limit = 128 * 1024
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		p.status = "read_error"
		return
	}
	if len(body) > limit {
		p.status = "unverified_size_limit"
		return
	}
	var root any
	if json.Unmarshal(body, &root) != nil {
		p.status = "unverified_format"
		return
	}
	texts := []string{}
	var visit func(any, bool, int)
	visit = func(v any, textField bool, depth int) {
		if depth > 32 {
			return
		}
		switch x := v.(type) {
		case string:
			if textField {
				texts = append(texts, x)
			}
		case []any:
			for _, item := range x {
				visit(item, textField, depth+1)
			}
		case map[string]any:
			if kind, _ := x["type"].(string); strings.Contains(kind, "tool") || strings.Contains(kind, "function") {
				return
			}
			for k, item := range x {
				if k == "arguments" || k == "tools" || k == "metadata" {
					continue
				}
				visit(item, k == "text" || k == "content" || k == "instructions" || k == "system", depth+1)
			}
		}
	}
	visit(root, false, 0)
	for _, expected := range p.expected {
		found := false
		for _, value := range texts {
			if strings.Contains(value, expected) {
				found = true
				break
			}
		}
		if !found {
			p.status = "expected_text_not_found"
			return
		}
	}
	p.status = "text_presence_verified"
}
