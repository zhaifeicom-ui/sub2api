package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const QualityPelicanPrompt = "创建一个完整、可独立运行的 HTML，内容是用 SVG 绘制一只鹈鹕骑自行车的 2D 动画。鹈鹕应具有长喙和喉囊，自行车应有合理的车架、车轮、车把、曲柄和踏板，双脚跟随踏板运动。使用内联 CSS 和 JavaScript，不使用外部资源、不调用工具、不联网。只输出完整 HTML 源码，不要 Markdown 代码围栏或解释。"
const QualityPromptVersion = "pelican-animation-v1"
const qualityMaxResponseBytes = 1024 * 1024
const qualityMaxSourceBytes = 512 * 1024
const qualityTimeout = 5 * time.Minute

var (
	ErrQualityInvalid   = infraerrors.BadRequest("QUALITY_INVALID", "invalid quality-test configuration or model")
	ErrQualityDenied    = infraerrors.Forbidden("QUALITY_DENIED", "quality testing is not available for this group")
	ErrQualityBusy      = infraerrors.Conflict("QUALITY_BUSY", "another quality test is running; try again later")
	ErrQualityCooldown  = infraerrors.Conflict("QUALITY_COOLDOWN", "this model is cooling down; wait before testing again")
	ErrQualityConflict  = infraerrors.Conflict("QUALITY_CONFIG_CONFLICT", "configuration changed; reload and try again")
	ErrQualityNoAccount = infraerrors.BadRequest("QUALITY_NO_ACCOUNT", "no schedulable OpenAI API-key account supports this model")
)

type GroupQualityConfig struct {
	GroupID             int64    `json:"group_id"`
	GroupName           string   `json:"group_name"`
	Platform            string   `json:"platform"`
	Enabled             bool     `json:"enabled"`
	Models              []string `json:"models"`
	CustomerAccess      string   `json:"customer_access"`
	APIMode             string   `json:"api_mode"`
	AccountID           *int64   `json:"account_id,omitempty"`
	CooldownSeconds     int      `json:"cooldown_seconds"`
	AutoEnabled         bool     `json:"auto_enabled"`
	AutoIntervalSeconds int      `json:"auto_interval_seconds"`
	Version             int      `json:"version"`
	AvailableModels     []string `json:"available_models"`
}

type GroupQualityRun struct {
	ID             int64      `json:"id"`
	GroupID        int64      `json:"group_id"`
	Model          string     `json:"model"`
	AccountID      *int64     `json:"account_id,omitempty"`
	UpstreamModel  string     `json:"upstream_model,omitempty"`
	PromptVersion  string     `json:"prompt_version"`
	APIMode        string     `json:"api_mode"`
	RequestedBy    *int64     `json:"requested_by,omitempty"`
	Status         string     `json:"status"`
	Automatic      bool       `json:"automatic"`
	StartedAt      time.Time  `json:"started_at"`
	DeadlineAt     time.Time  `json:"deadline_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	DurationMS     int        `json:"duration_ms"`
	ResponseLength int        `json:"response_length"`
	InputTokens    int64      `json:"input_tokens"`
	OutputTokens   int64      `json:"output_tokens"`
	Source         string     `json:"source,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
}

type GroupQualityRepository interface {
	ListConfigs(context.Context) ([]GroupQualityConfig, error)
	GetConfig(context.Context, int64) (*GroupQualityConfig, error)
	SaveConfig(context.Context, GroupQualityConfig) (*GroupQualityConfig, error)
	Reserve(context.Context, *GroupQualityRun, bool) error
	Finish(context.Context, *GroupQualityRun) error
	ListRuns(context.Context, int64, string) ([]GroupQualityRun, error)
	GetRun(context.Context, int64) (*GroupQualityRun, error)
}

type GroupQualityService struct {
	repo        GroupQualityRepository
	accounts    AccountRepository
	groups      GroupRepository
	http        HTTPUpstream
	config      *config.Config
	concurrency *ConcurrencyService
	tls         *TLSFingerprintProfileService
	autoOnce    sync.Once
	autoCancel  context.CancelFunc
	autoWG      sync.WaitGroup
}

func NewGroupQualityService(repo GroupQualityRepository, accounts AccountRepository, groups GroupRepository, upstream HTTPUpstream, cfg *config.Config, concurrency *ConcurrencyService, tls *TLSFingerprintProfileService) *GroupQualityService {
	return &GroupQualityService{repo: repo, accounts: accounts, groups: groups, http: upstream, config: cfg, concurrency: concurrency, tls: tls}
}

func IsQualityModel(model string) bool { return model == "gpt-6-astra" || model == "gpt-6-sol" }

func ValidateQualityConfig(c GroupQualityConfig) error {
	if c.AutoIntervalSeconds != 0 && (c.AutoIntervalSeconds < 600 || c.AutoIntervalSeconds > 31536000) {
		return ErrQualityInvalid
	}
	if c.GroupID <= 0 || c.Version < 0 || len(c.Models) < 1 || len(c.Models) > 2 || c.CooldownSeconds < 300 || c.CooldownSeconds > 86400 {
		return ErrQualityInvalid
	}
	seen := map[string]bool{}
	for _, m := range c.Models {
		if !IsQualityModel(m) || seen[m] {
			return ErrQualityInvalid
		}
		seen[m] = true
	}
	if c.CustomerAccess != "hidden" && c.CustomerAccess != "view" && c.CustomerAccess != "run" {
		return ErrQualityInvalid
	}
	if c.APIMode != "responses" && c.APIMode != "chat_completions" {
		return ErrQualityInvalid
	}
	if c.AccountID != nil && *c.AccountID <= 0 {
		return ErrQualityInvalid
	}
	return nil
}

func (s *GroupQualityService) candidates(ctx context.Context, groupID int64, cfg *GroupQualityConfig, model string) ([]Account, error) {
	g, err := s.groups.GetByIDLite(ctx, groupID)
	if err != nil {
		return nil, err
	}
	// Composite routing and OAuth are intentionally not guessed by this API-key diagnostic.
	if !g.IsActive() || g.Platform != PlatformOpenAI {
		return nil, ErrQualityNoAccount
	}
	accounts, err := s.accounts.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0)
	for _, a := range accounts {
		if qualityEligibleAccount(&a, cfg, model) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func qualityEligibleAccount(a *Account, cfg *GroupQualityConfig, model string) bool {
	return a.Platform == PlatformOpenAI && a.Type == AccountTypeAPIKey && a.IsSchedulable() && a.IsModelSupported(model) && a.GetOpenAIApiKey() != "" &&
		(cfg.AccountID == nil || a.ID == *cfg.AccountID) && (a.ProxyID == nil || a.Proxy != nil)
}

func (s *GroupQualityService) ListConfigs(ctx context.Context, allowed map[int64]bool, admin bool) ([]GroupQualityConfig, error) {
	cs, err := s.repo.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := []GroupQualityConfig{}
	for _, c := range cs {
		// Do not hydrate other groups' credentials for an ordinary user's page.
		if !admin && (!allowed[c.GroupID] || !c.Enabled || c.CustomerAccess == "hidden") {
			continue
		}
		c.AvailableModels = []string{}
		if c.Platform != PlatformOpenAI {
			out = append(out, c)
			continue
		}
		accounts, err := s.accounts.ListByGroup(ctx, c.GroupID)
		if err != nil {
			return nil, err
		}
		for _, model := range []string{"gpt-6-astra", "gpt-6-sol"} {
			for i := range accounts {
				if qualityEligibleAccount(&accounts[i], &c, model) {
					c.AvailableModels = append(c.AvailableModels, model)
					break
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *GroupQualityService) SaveConfig(ctx context.Context, c GroupQualityConfig) (*GroupQualityConfig, error) {
	if c.AutoIntervalSeconds == 0 {
		c.AutoIntervalSeconds = 600
	}
	if err := ValidateQualityConfig(c); err != nil {
		return nil, err
	}
	if _, err := s.groups.GetByIDLite(ctx, c.GroupID); err != nil {
		return nil, err
	}
	if c.AccountID != nil {
		as, err := s.accounts.ListByGroup(ctx, c.GroupID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, a := range as {
			if a.ID == *c.AccountID && a.Platform == PlatformOpenAI && a.Type == AccountTypeAPIKey {
				found = true
			}
		}
		if !found {
			return nil, ErrQualityNoAccount
		}
	}
	return s.repo.SaveConfig(ctx, c)
}

func (s *GroupQualityService) GetConfig(ctx context.Context, id int64) (*GroupQualityConfig, error) {
	return s.repo.GetConfig(ctx, id)
}
func (s *GroupQualityService) ListRuns(ctx context.Context, id int64, model string) ([]GroupQualityRun, error) {
	if !IsQualityModel(model) {
		return nil, ErrQualityInvalid
	}
	return s.repo.ListRuns(ctx, id, model)
}
func (s *GroupQualityService) GetRun(ctx context.Context, id int64) (*GroupQualityRun, error) {
	return s.repo.GetRun(ctx, id)
}

func (s *GroupQualityService) Start(ctx context.Context, groupID, userID int64, model string, admin bool) (*GroupQualityRun, error) {
	if !IsQualityModel(model) {
		return nil, ErrQualityInvalid
	}
	cfg, err := s.repo.GetConfig(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || (!admin && cfg.CustomerAccess != "run") {
		return nil, ErrQualityDenied
	}
	if userID == 0 && !cfg.AutoEnabled {
		return nil, ErrQualityDenied
	}
	as, err := s.candidates(ctx, groupID, cfg, model)
	if err != nil {
		return nil, err
	}
	if len(as) == 0 {
		return nil, ErrQualityNoAccount
	}
	a := as[0]
	r := &GroupQualityRun{GroupID: groupID, Model: model, AccountID: &a.ID, UpstreamModel: a.GetMappedModel(model), PromptVersion: QualityPromptVersion, APIMode: cfg.APIMode, Status: "running"}
	r.Automatic = userID == 0
	if userID > 0 {
		r.RequestedBy = &userID
	}
	if err := s.repo.Reserve(ctx, r, admin); err != nil {
		return nil, err
	}
	// Copy prevents response serialization racing with the background worker.
	job := *r
	go s.execute(job, a)
	return r, nil
}

func (s *GroupQualityService) execute(r GroupQualityRun, a Account) {
	ctx, cancel := context.WithTimeout(context.Background(), qualityTimeout)
	defer cancel()
	r.Status = "failed"
	defer func() {
		if recover() != nil {
			r.ErrorCode = "internal_error"
			r.ErrorMessage = "检测任务异常，请稍后重试"
		}
		now := time.Now()
		r.FinishedAt = &now
		r.DurationMS = int(now.Sub(r.StartedAt).Milliseconds())
		persist, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// The persisted deadline also recovers jobs if the process exits before this update.
		if err := s.repo.Finish(persist, &r); err != nil {
			slog.Error("quality test persistence failed", "run_id", r.ID)
		}
	}()
	if s.concurrency != nil {
		slot, err := s.concurrency.AcquireAccountSlot(ctx, a.ID, a.Concurrency)
		if err != nil || slot == nil || !slot.Acquired {
			r.ErrorCode = "account_busy"
			r.ErrorMessage = "上游账号忙，请冷却后重试"
			return
		}
		defer slot.ReleaseFunc()
	}
	text, input, output, code, err := s.call(ctx, a, r.APIMode, r.UpstreamModel)
	r.InputTokens = input
	r.OutputTokens = output
	if err != nil {
		r.ErrorCode = code
		r.ErrorMessage = qualityErrorMessage(code)
		return
	}
	r.ResponseLength = utf8.RuneCountInString(text)
	source, err := ExtractQualitySource(text)
	if err != nil {
		r.ErrorCode = "invalid_artifact"
		r.ErrorMessage = qualityErrorMessage(r.ErrorCode)
		return
	}
	r.Source = source
	r.Status = "completed"
}

func qualityErrorMessage(code string) string {
	switch code {
	case "unsafe_endpoint":
		return "上游地址不符合出站安全策略"
	case "response_too_large":
		return "输出超过检测大小限制"
	case "upstream_http":
		return "上游返回非成功状态；请检查该渠道监控项的 Key、额度和请求配置"
	case "channel_key_unavailable":
		return "该渠道的 Key 无法读取，请到渠道监控重新保存 API Key"
	case "invalid_artifact":
		return "未生成完整 HTML/SVG，可能输出被截断；不代表已判定降智"
	default:
		return "上游超时、连接失败或响应格式异常；不代表已判定降智"
	}
}

func (s *GroupQualityService) call(ctx context.Context, a Account, mode, model string) (string, int64, int64, string, error) {
	base, err := cnValidateProbeURL(s.config, a.GetOpenAIBaseURL())
	if err != nil {
		return "", 0, 0, "unsafe_endpoint", err
	}
	u, err := url.Parse(base)
	if err != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return "", 0, 0, "unsafe_endpoint", fmt.Errorf("invalid endpoint")
	}
	body := map[string]any{"model": model, "stream": false}
	endpoint := buildOpenAIResponsesURL(base)
	if mode == "responses" {
		body["input"] = QualityPelicanPrompt
		body["max_output_tokens"] = 16000
		body["store"] = false
	} else {
		endpoint = strings.TrimRight(base, "/")
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		endpoint += "/chat/completions"
		body["messages"] = []map[string]string{{"role": "user", "content": QualityPelicanPrompt}}
		body["max_completion_tokens"] = 16000
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", 0, 0, "request_failed", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	a.ApplyHeaderOverrides(req.Header)
	// Protected headers always use the selected account, never client input.
	req.Header.Set("Authorization", "Bearer "+a.GetOpenAIApiKey())
	req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI)))
	proxy := ""
	if a.Proxy != nil {
		proxy = a.Proxy.URL()
	}
	resp, err := s.http.DoWithTLS(req, proxy, a.ID, a.Concurrency, s.tls.ResolveTLSProfile(&a))
	if err != nil {
		return "", 0, 0, "request_failed", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, 0, "upstream_http", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, qualityMaxResponseBytes+1))
	if err != nil {
		return "", 0, 0, "request_failed", err
	}
	if len(data) > qualityMaxResponseBytes {
		return "", 0, 0, "response_too_large", fmt.Errorf("limit exceeded")
	}
	if !gjson.ValidBytes(data) {
		return "", 0, 0, "invalid_response", fmt.Errorf("invalid JSON")
	}
	if mode == "responses" {
		status := gjson.GetBytes(data, "status").String()
		if status != "" && status != "completed" {
			return "", 0, 0, "invalid_response", fmt.Errorf("incomplete response")
		}
		var text strings.Builder
		for _, item := range gjson.GetBytes(data, "output").Array() {
			if item.Get("type").String() != "message" {
				continue
			}
			for _, part := range item.Get("content").Array() {
				if part.Get("type").String() == "output_text" {
					_, _ = text.WriteString(part.Get("text").String())
				}
			}
		}
		return text.String(), gjson.GetBytes(data, "usage.input_tokens").Int(), gjson.GetBytes(data, "usage.output_tokens").Int(), "", nil
	}
	if reason := gjson.GetBytes(data, "choices.0.finish_reason").String(); reason != "stop" {
		return "", 0, 0, "invalid_response", fmt.Errorf("incomplete response")
	}
	return gjson.GetBytes(data, "choices.0.message.content").String(), gjson.GetBytes(data, "usage.prompt_tokens").Int(), gjson.GetBytes(data, "usage.completion_tokens").Int(), "", nil
}

// HTML/SVG presence is artifact validation, NOT an intelligence score.
func ExtractQualitySource(text string) (string, error) {
	if len(text) > qualityMaxSourceBytes {
		return "", ErrQualityInvalid
	}
	lower := strings.ToLower(text)
	for _, pair := range [][2]string{{"<html", "</html>"}, {"<svg", "</svg>"}} {
		start, end := strings.Index(lower, pair[0]), strings.LastIndex(lower, pair[1])
		if start >= 0 && end > start {
			return strings.TrimSpace(text[start : end+len(pair[1])]), nil
		}
	}
	return "", ErrQualityInvalid
}
