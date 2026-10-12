package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func TestQualityConfigValidation(t *testing.T) {
	valid := GroupQualityConfig{GroupID: 1, Models: []string{"gpt-6-astra", "gpt-6-sol"}, CustomerAccess: "hidden", APIMode: "responses", CooldownSeconds: 600}
	if err := ValidateQualityConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*GroupQualityConfig){
		func(c *GroupQualityConfig) { c.Models = []string{"gpt-6-astra", "gpt-6-astra"} },
		func(c *GroupQualityConfig) { c.Models = []string{"gpt-5"} },
		func(c *GroupQualityConfig) { c.CustomerAccess = "public" },
		func(c *GroupQualityConfig) { c.CooldownSeconds = 0 },
		func(c *GroupQualityConfig) { c.APIMode = "unknown" },
		func(c *GroupQualityConfig) { c.AutoIntervalSeconds = 599 },
		func(c *GroupQualityConfig) { c.AutoIntervalSeconds = 31536001 },
		func(c *GroupQualityConfig) { id := int64(-1); c.AccountID = &id },
	} {
		c := valid
		change(&c)
		if ValidateQualityConfig(c) == nil {
			t.Fatalf("accepted invalid configuration: %+v", c)
		}
	}
	for _, interval := range []int{600, 1800, 3600, 86400, 31536000} {
		c := valid
		c.AutoIntervalSeconds = interval
		if err := ValidateQualityConfig(c); err != nil {
			t.Fatalf("rejected interval %d", interval)
		}
	}
}
func TestQualityArtifactExtraction(t *testing.T) {
	for _, text := range []string{"```html\n<html><body><svg></svg></body></html>\n```", "<SVG><circle/></SVG>"} {
		source, err := ExtractQualitySource(text)
		if err != nil || strings.Contains(source, "```") {
			t.Fatalf("bad artifact: %q %v", source, err)
		}
	}
	for _, text := range []string{"hello", "<html><body>truncated", "<svg truncated", strings.Repeat("x", qualityMaxSourceBytes+1)} {
		if _, err := ExtractQualitySource(text); err == nil {
			t.Fatal("accepted invalid artifact")
		}
	}
}

type qualityHTTPStub struct {
	HTTPUpstream
	response string
	status   int
	request  *http.Request
	body     map[string]any
	proxy    string
}

func (h *qualityHTTPStub) DoWithTLS(req *http.Request, proxy string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	h.request = req
	h.proxy = proxy
	_ = json.NewDecoder(req.Body).Decode(&h.body)
	return &http.Response{StatusCode: h.status, Body: io.NopCloser(strings.NewReader(h.response))}, nil
}
func TestQualityProtocolsAndMappedRequest(t *testing.T) {
	for _, mode := range []string{"responses", "chat_completions"} {
		h := &qualityHTTPStub{status: 200, response: `{"status":"completed","output":[{"type":"reasoning","content":[{"type":"output_text","text":"ignore"}]},{"type":"message","content":[{"type":"output_text","text":"<svg></svg>"}]}],"usage":{"input_tokens":12,"output_tokens":15}}`}
		if mode == "chat_completions" {
			h.response = `{"choices":[{"finish_reason":"stop","message":{"content":"<svg></svg>"}}],"usage":{"prompt_tokens":12,"completion_tokens":15}}`
		}
		s := &GroupQualityService{http: h}
		a := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://upstream.example/v1", "api_key": "test-secret"}}
		text, in, out, code, err := s.call(context.Background(), a, mode, "upstream-alias")
		if err != nil || text != "<svg></svg>" || in != 12 || out != 15 || code != "" {
			t.Fatalf("bad response: %q %v", text, err)
		}
		if h.body["model"] != "upstream-alias" || h.body["stream"] != false || h.request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("bad request")
		}
		if !HTTPUpstreamRedirectsDisabled(h.request.Context()) {
			t.Fatal("redirects must be disabled")
		}
		if strings.Contains(h.request.URL.Path, "/v1/v1/") {
			t.Fatal("duplicated v1")
		}
	}
}
func TestQualityIncompleteAndOversizedResponse(t *testing.T) {
	a := Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://upstream.example", "api_key": "secret"}}
	for _, body := range []string{`{"status":"incomplete","output":[]}`, `not json`, strings.Repeat("x", qualityMaxResponseBytes+1)} {
		s := &GroupQualityService{http: &qualityHTTPStub{status: 200, response: body}}
		if _, _, _, _, err := s.call(context.Background(), a, "responses", "gpt-6-sol"); err == nil {
			t.Fatal("accepted incomplete response")
		}
	}
	s := &GroupQualityService{http: &qualityHTTPStub{status: 401, response: "secret"}}
	_, _, _, code, err := s.call(context.Background(), a, "responses", "gpt-6-sol")
	if err == nil || code != "upstream_http" || strings.Contains(qualityErrorMessage(code), "secret") {
		t.Fatal("upstream error disclosure")
	}
}

type qualityAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (r qualityAccountRepoStub) ListByGroup(context.Context, int64) ([]Account, error) {
	return r.accounts, nil
}

type qualityGroupRepoStub struct{ GroupRepository }

func (r qualityGroupRepoStub) GetByIDLite(context.Context, int64) (*Group, error) {
	return &Group{ID: 1, Platform: PlatformOpenAI, Status: StatusActive}, nil
}
func TestQualityAccountEligibilityAndMapping(t *testing.T) {
	as := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: false, Credentials: map[string]any{"api_key": "k"}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "k", "model_mapping": map[string]any{"gpt-6-sol": "sol-alias"}}},
	}
	s := &GroupQualityService{accounts: qualityAccountRepoStub{accounts: as}, groups: qualityGroupRepoStub{}}
	got, err := s.candidates(context.Background(), 1, &GroupQualityConfig{}, "gpt-6-sol")
	if err != nil || len(got) != 1 || got[0].ID != 3 || got[0].GetMappedModel("gpt-6-sol") != "sol-alias" {
		t.Fatalf("wrong candidate: %+v %v", got, err)
	}
	got, err = s.candidates(context.Background(), 1, &GroupQualityConfig{}, "gpt-6-astra")
	if err != nil || len(got) != 0 {
		t.Fatal("allowed unlisted model")
	}
}
