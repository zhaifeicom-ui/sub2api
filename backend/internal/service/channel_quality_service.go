package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const channelQualityTimeout = 10 * time.Minute
const channelQualityOutputTokens = 32768

func isChannelQualityModel(model string) bool {
	return model == "gpt-6-astra" || model == "gpt-6.1-sol"
}

var (
	ErrChannelQualityInvalid  = infraerrors.BadRequest("CHANNEL_QUALITY_INVALID", "invalid channel quality-test configuration or model")
	ErrChannelQualityDenied   = infraerrors.Forbidden("CHANNEL_QUALITY_DENIED", "quality testing is not available for this channel")
	ErrChannelQualityBusy     = infraerrors.Conflict("CHANNEL_QUALITY_BUSY", "another quality test is running; try again later")
	ErrChannelQualityCooldown = infraerrors.Conflict("CHANNEL_QUALITY_COOLDOWN", "this model is cooling down; wait before testing again")
	ErrChannelQualityConflict = infraerrors.Conflict("CHANNEL_QUALITY_CONFIG_CONFLICT", "configuration changed; reload and try again")
	ErrChannelQualityNoModel  = infraerrors.BadRequest("CHANNEL_QUALITY_NO_MODEL", "this channel does not have the selected GPT-6 model or a usable API key")
	ErrChannelQualityNotFound = infraerrors.NotFound("CHANNEL_QUALITY_NOT_FOUND", "channel quality configuration or run not found")
)

type ChannelQualityConfig struct {
	MonitorID           int64    `json:"monitor_id"`
	MonitorName         string   `json:"monitor_name"`
	Provider            string   `json:"provider"`
	Enabled             bool     `json:"enabled"`
	Models              []string `json:"models"`
	CustomerAccess      string   `json:"customer_access"`
	CooldownSeconds     int      `json:"cooldown_seconds"`
	AutoEnabled         bool     `json:"auto_enabled"`
	AutoIntervalSeconds int      `json:"auto_interval_seconds"`
	Version             int      `json:"version"`
	AvailableModels     []string `json:"available_models"`
}

type ChannelQualityRun struct {
	ID             int64      `json:"id"`
	MonitorID      int64      `json:"monitor_id"`
	MonitorName    string     `json:"monitor_name"`
	Model          string     `json:"model"`
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

type ChannelQualityRepository interface {
	ListConfigs(context.Context) ([]ChannelQualityConfig, error)
	GetConfig(context.Context, int64) (*ChannelQualityConfig, error)
	SaveConfig(context.Context, ChannelQualityConfig) (*ChannelQualityConfig, error)
	DeleteConfig(context.Context, int64) error
	Reserve(context.Context, *ChannelQualityRun, bool) error
	Finish(context.Context, *ChannelQualityRun) error
	ListRuns(context.Context, int64, string) ([]ChannelQualityRun, error)
	GetRun(context.Context, int64) (*ChannelQualityRun, error)
}

type ChannelQualityScheduleRepository interface {
	WithChannelQualityScheduleLock(context.Context, func(context.Context) error) error
	ClaimChannelQualityDue(context.Context) (*ChannelQualityScheduledCheck, error)
	RecordChannelQualityScheduleOutcome(context.Context, *ChannelQualityScheduledCheck, string) error
}

type ChannelQualityScheduledCheck struct {
	MonitorID int64
	Model     string
}

type ChannelQualityService struct {
	repo       ChannelQualityRepository
	monitors   *ChannelMonitorService
	http       HTTPUpstream
	config     *config.Config
	autoOnce   sync.Once
	autoCancel context.CancelFunc
	autoWG     sync.WaitGroup
}

func NewChannelQualityService(repo ChannelQualityRepository, monitors *ChannelMonitorService, upstream HTTPUpstream, cfg *config.Config) *ChannelQualityService {
	return &ChannelQualityService{repo: repo, monitors: monitors, http: upstream, config: cfg}
}

func channelQualityModels(m *ChannelMonitor) []string {
	if m == nil || !m.Enabled || m.Provider != "openai" || defaultCheckMode(m.CheckMode) == MonitorCheckModeQuota || m.APIKeyDecryptFailed || strings.TrimSpace(m.APIKey) == "" {
		return []string{}
	}
	// PrimaryModel/ExtraModels configure health probes, not upstream access.
	// Quality checks have their own fixed model whitelist and do not change
	// the channel's existing health-check schedule or model configuration.
	return []string{"gpt-6-astra", "gpt-6.1-sol"}
}

func ValidateChannelQualityConfig(c ChannelQualityConfig) error {
	if c.MonitorID <= 0 || c.Version < 0 || len(c.Models) < 1 || len(c.Models) > 2 || c.CooldownSeconds < 300 || c.CooldownSeconds > 86400 {
		return ErrChannelQualityInvalid
	}
	if c.AutoIntervalSeconds != 0 && (c.AutoIntervalSeconds < 600 || c.AutoIntervalSeconds > 31536000) {
		return ErrChannelQualityInvalid
	}
	if c.CustomerAccess != "hidden" && c.CustomerAccess != "view" && c.CustomerAccess != "run" {
		return ErrChannelQualityInvalid
	}
	seen := map[string]bool{}
	for _, model := range c.Models {
		if !isChannelQualityModel(model) || seen[model] {
			return ErrChannelQualityInvalid
		}
		seen[model] = true
	}
	return nil
}

func (s *ChannelQualityService) monitorMetadata(ctx context.Context, id int64) (*ChannelMonitor, error) {
	if s == nil || s.monitors == nil || s.monitors.repo == nil {
		return nil, ErrChannelQualityNoModel
	}
	// Repository returns the encrypted key; config/model reads never decrypt credentials.
	return s.monitors.repo.GetByID(ctx, id)
}

func (s *ChannelQualityService) ListConfigs(ctx context.Context, admin bool) ([]ChannelQualityConfig, error) {
	configs, err := s.repo.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelQualityConfig, 0, len(configs))
	for _, c := range configs {
		if !admin && (!c.Enabled || c.CustomerAccess == "hidden") {
			continue
		}
		m, err := s.monitorMetadata(ctx, c.MonitorID)
		if err == nil {
			c.AvailableModels = channelQualityModels(m)
		} else {
			c.AvailableModels = []string{}
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *ChannelQualityService) SaveConfig(ctx context.Context, c ChannelQualityConfig) (*ChannelQualityConfig, error) {
	if c.AutoIntervalSeconds == 0 {
		c.AutoIntervalSeconds = 600
	}
	if err := ValidateChannelQualityConfig(c); err != nil {
		return nil, err
	}
	m, err := s.monitorMetadata(ctx, c.MonitorID)
	if err != nil {
		return nil, err
	}
	available := channelQualityModels(m)
	for _, model := range c.Models {
		found := false
		for _, allowed := range available {
			found = found || model == allowed
		}
		if !found {
			return nil, ErrChannelQualityNoModel
		}
	}
	return s.repo.SaveConfig(ctx, c)
}

func (s *ChannelQualityService) DeleteConfig(ctx context.Context, monitorID int64) error {
	return s.repo.DeleteConfig(ctx, monitorID)
}

func (s *ChannelQualityService) GetConfig(ctx context.Context, monitorID int64) (*ChannelQualityConfig, error) {
	return s.repo.GetConfig(ctx, monitorID)
}

func (s *ChannelQualityService) ListRuns(ctx context.Context, monitorID int64, model string) ([]ChannelQualityRun, error) {
	if !isChannelQualityModel(model) && model != "gpt-6-sol" {
		return nil, ErrChannelQualityInvalid
	}
	return s.repo.ListRuns(ctx, monitorID, model)
}

func (s *ChannelQualityService) GetRun(ctx context.Context, id int64) (*ChannelQualityRun, error) {
	return s.repo.GetRun(ctx, id)
}

func (s *ChannelQualityService) Start(ctx context.Context, monitorID, userID int64, model string, admin bool) (*ChannelQualityRun, error) {
	if !isChannelQualityModel(model) {
		return nil, ErrChannelQualityInvalid
	}
	cfg, err := s.repo.GetConfig(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || (!admin && cfg.CustomerAccess != "run") || (userID == 0 && !cfg.AutoEnabled) {
		return nil, ErrChannelQualityDenied
	}
	monitor, err := s.monitors.Get(ctx, monitorID)
	if err != nil || monitor.APIKeyDecryptFailed || strings.TrimSpace(monitor.APIKey) == "" {
		return nil, ErrChannelQualityNoModel
	}
	available := channelQualityModels(monitor)
	if !containsString(cfg.Models, model) || !containsString(available, model) {
		return nil, ErrChannelQualityNoModel
	}
	run := &ChannelQualityRun{
		MonitorID: monitorID, MonitorName: monitor.Name, Model: model,
		PromptVersion: QualityPromptVersion, APIMode: defaultAPIMode(monitor.APIMode), Status: "running",
		Automatic: userID == 0,
	}
	if userID > 0 {
		run.RequestedBy = &userID
	}
	if err := s.repo.Reserve(ctx, run, admin); err != nil {
		return nil, err
	}
	job := *run
	channel := *monitor
	go s.execute(job, channel)
	return run, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *ChannelQualityService) execute(run ChannelQualityRun, monitor ChannelMonitor) {
	ctx, cancel := context.WithTimeout(context.Background(), channelQualityTimeout)
	defer cancel()
	run.Status = "failed"
	defer func() {
		if recover() != nil {
			run.ErrorCode = "internal_error"
			run.ErrorMessage = "检测任务异常，请稍后重试"
		}
		now := time.Now()
		run.FinishedAt = &now
		run.DurationMS = int(now.Sub(run.StartedAt).Milliseconds())
		persist, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := s.repo.Finish(persist, &run); err != nil {
			slog.Error("channel quality test persistence failed", "run_id", run.ID)
		}
	}()
	text, input, output, code, err := s.call(ctx, &monitor, run.Model)
	run.InputTokens, run.OutputTokens = input, output
	if err != nil {
		run.ErrorCode = code
		run.ErrorMessage = channelQualityErrorMessage(code, err)
		return
	}
	run.ResponseLength = utf8.RuneCountInString(text)
	source, err := ExtractQualitySource(text)
	if err != nil {
		run.ErrorCode = "invalid_artifact"
		run.ErrorMessage = qualityErrorMessage(run.ErrorCode)
		return
	}
	run.Source = source
	run.Status = "completed"
}

func (s *ChannelQualityService) call(ctx context.Context, monitor *ChannelMonitor, model string) (string, int64, int64, string, error) {
	if monitor.Provider != "openai" || monitor.APIKeyDecryptFailed || strings.TrimSpace(monitor.APIKey) == "" {
		return "", 0, 0, "channel_key_unavailable", ErrChannelQualityNoModel
	}
	base, err := cnValidateProbeURL(s.config, monitor.Endpoint)
	if err != nil {
		return "", 0, 0, "unsafe_endpoint", err
	}
	u, err := url.Parse(base)
	if err != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return "", 0, 0, "unsafe_endpoint", fmt.Errorf("invalid endpoint")
	}
	body := map[string]any{"model": model, "stream": true}
	endpoint := buildOpenAIResponsesURL(base)
	if defaultAPIMode(monitor.APIMode) == MonitorAPIModeResponses {
		body["input"] = QualityPelicanPrompt
		body["max_output_tokens"] = channelQualityOutputTokens
		body["store"] = false
	} else {
		endpoint = buildOpenAIEndpointURL(base, "/v1/chat/completions")
		body["messages"] = []map[string]string{{"role": "user", "content": QualityPelicanPrompt}}
		body["max_completion_tokens"] = channelQualityOutputTokens
		body["stream_options"] = map[string]bool{"include_usage": true}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", 0, 0, "request_failed", err
	}
	for key, value := range monitor.ExtraHeaders {
		if !strings.EqualFold(key, "authorization") && !strings.EqualFold(key, "content-type") && !strings.EqualFold(key, "host") {
			req.Header.Set(key, value)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+monitor.APIKey)
	req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI)))
	// Channel monitor IDs are positive and may collide with account IDs in the
	// shared HTTP transport cache. Use a negative namespace for monitor probes.
	resp, err := s.http.DoWithTLS(req, "", -monitor.ID, 1, nil)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", 0, 0, "timeout", err
		}
		return "", 0, 0, "request_failed", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		message := gjson.GetBytes(data, "error.message").String()
		message = strings.ReplaceAll(message, monitor.APIKey, "***")
		message = redactChannelQualityError(message)
		return "", 0, 0, "upstream_http", fmt.Errorf("HTTP %d%s", resp.StatusCode, message)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		text, input, output, code, err := readChannelQualityStream(ctx, resp.Body, defaultAPIMode(monitor.APIMode))
		if err != nil {
			err = fmt.Errorf("%s", strings.ReplaceAll(err.Error(), monitor.APIKey, "***"))
		}
		return text, input, output, code, err
	}
	// Some compatible upstreams ignore stream=true and return a JSON result.
	data, err := io.ReadAll(io.LimitReader(resp.Body, qualityMaxResponseBytes+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", 0, 0, "timeout", err
		}
		return "", 0, 0, "request_failed", err
	}
	if len(data) > qualityMaxResponseBytes {
		return "", 0, 0, "response_too_large", fmt.Errorf("limit exceeded")
	}
	if !gjson.ValidBytes(data) {
		return "", 0, 0, "invalid_response", fmt.Errorf("invalid JSON")
	}
	if defaultAPIMode(monitor.APIMode) == MonitorAPIModeResponses {
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
					text.WriteString(part.Get("text").String())
				}
			}
		}
		input, output := gjson.GetBytes(data, "usage.input_tokens").Int(), gjson.GetBytes(data, "usage.output_tokens").Int()
		if strings.TrimSpace(text.String()) == "" {
			return "", input, output, "empty_output", fmt.Errorf("no visible output")
		}
		return text.String(), input, output, "", nil
	}
	if reason := gjson.GetBytes(data, "choices.0.finish_reason").String(); reason != "stop" {
		return "", 0, 0, "invalid_response", fmt.Errorf("incomplete response")
	}
	text := gjson.GetBytes(data, "choices.0.message.content").String()
	input, output := gjson.GetBytes(data, "usage.prompt_tokens").Int(), gjson.GetBytes(data, "usage.completion_tokens").Int()
	if strings.TrimSpace(text) == "" {
		return "", input, output, "empty_output", fmt.Errorf("no visible output")
	}
	return text, input, output, "", nil
}

func (s *ChannelQualityService) StartAutomatic() {
	s.autoOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.autoCancel = cancel
		s.autoWG.Add(1)
		go func() {
			defer s.autoWG.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			s.automaticTick(ctx)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.automaticTick(ctx)
				}
			}
		}()
	})
}

func (s *ChannelQualityService) StopAutomatic() {
	if s.autoCancel != nil {
		s.autoCancel()
	}
	s.autoWG.Wait()
}

func (s *ChannelQualityService) automaticTick(parent context.Context) {
	repo, ok := s.repo.(ChannelQualityScheduleRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	err := repo.WithChannelQualityScheduleLock(ctx, func(ctx context.Context) error {
		job, err := repo.ClaimChannelQualityDue(ctx)
		if err != nil || job == nil {
			return err
		}
		_, err = s.Start(ctx, job.MonitorID, 0, job.Model, true)
		outcome := "submitted"
		if err != nil {
			outcome = "skipped"
		}
		if writeErr := repo.RecordChannelQualityScheduleOutcome(ctx, job, outcome); writeErr != nil {
			return writeErr
		}
		if err != nil {
			slog.Warn("scheduled channel quality test skipped", "monitor_id", job.MonitorID)
		}
		return nil
	})
	if err != nil && parent.Err() == nil {
		slog.Warn("channel quality scheduler check failed")
	}
}
