package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	SettingKeyPromptInjectionRules = "prompt_injection_rules"
	promptInjectionCacheTTL        = 60 * time.Second
	maxPromptInjectionRules        = 200
	maxPromptInjectionContentRunes = 100000
)

var ErrPromptInjectionConflict = infraerrors.Conflict("PROMPT_INJECTION_CONFLICT", "规则已被其他页面修改，请刷新后重试")

type PromptInjectionConfig struct {
	Version int64                 `json:"version"`
	Rules   []PromptInjectionRule `json:"rules"`
}

type PromptInjectionRule struct {
	Action     string `json:"action,omitempty"`
	Match      string `json:"match,omitempty"`
	WholeInput bool   `json:"whole_input,omitempty"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Scope      string `json:"scope"`
	// GroupIDs allows one rule to target several groups. TargetID is kept for
	// backwards compatibility with rules created by the first version.
	GroupIDs []int64  `json:"group_ids,omitempty"`
	TargetID int64    `json:"target_id"`
	Role     string   `json:"role"`
	Position string   `json:"position"`
	Content  string   `json:"content"`
	Models   []string `json:"models"`
	Priority int      `json:"priority"`
}

type cachedPromptInjectionConfig struct {
	config   PromptInjectionConfig
	loadedAt time.Time
	degraded bool
}

func normalizePromptInjectionConfig(config PromptInjectionConfig) (PromptInjectionConfig, error) {
	if config.Rules == nil {
		config.Rules = []PromptInjectionRule{}
	}
	if len(config.Rules) > maxPromptInjectionRules {
		return config, fmt.Errorf("最多允许 %d 条提示词注入规则", maxPromptInjectionRules)
	}
	seen := make(map[string]struct{}, len(config.Rules))
	for i := range config.Rules {
		rule := &config.Rules[i]
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Scope = strings.ToLower(strings.TrimSpace(rule.Scope))
		rule.Role = strings.ToLower(strings.TrimSpace(rule.Role))
		rule.Position = strings.ToLower(strings.TrimSpace(rule.Position))
		if rule.Action == "" || rule.Action == "append" || rule.Action == "replace_system" {
			rule.Content = strings.TrimSpace(rule.Content)
		}
		if rule.Action == "" {
			rule.Action = "append"
		}
		if rule.Action != "append" && rule.Action != "replace_system" && rule.Action != "replace_input" && rule.Action != "replace_output" {
			return config, fmt.Errorf("规则操作无效")
		}
		if rule.Action == "replace_input" || rule.Action == "replace_output" {
			if (rule.Match == "" && !(rule.Action == "replace_input" && rule.WholeInput)) || len(rule.Match) > 1024 || len(rule.Content) > 4096 {
				return config, fmt.Errorf("替换匹配词不能为空且不可超过1024字节，替换文本不可超过4096字节")
			}
		}
		if rule.Action == "replace_system" {
			rule.Role = "system"
			rule.Position = "prepend"
		}
		if rule.ID == "" {
			return config, fmt.Errorf("第 %d 条规则缺少 ID", i+1)
		}
		if _, ok := seen[rule.ID]; ok {
			return config, fmt.Errorf("规则 ID %q 重复", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if rule.Name == "" {
			return config, fmt.Errorf("规则 %q 缺少名称", rule.ID)
		}
		if rule.Scope != "group" && rule.Scope != "account" {
			return config, fmt.Errorf("规则 %q 的作用范围无效", rule.Name)
		}
		if rule.Scope == "group" {
			if len(rule.GroupIDs) == 0 && rule.TargetID > 0 {
				rule.GroupIDs = []int64{rule.TargetID}
			}
			uniqueGroups := make([]int64, 0, len(rule.GroupIDs))
			seenGroups := make(map[int64]struct{}, len(rule.GroupIDs))
			for _, groupID := range rule.GroupIDs {
				if groupID <= 0 {
					continue
				}
				if _, ok := seenGroups[groupID]; ok {
					continue
				}
				seenGroups[groupID] = struct{}{}
				uniqueGroups = append(uniqueGroups, groupID)
			}
			if len(uniqueGroups) == 0 {
				return config, fmt.Errorf("规则 %q 未选择分组", rule.Name)
			}
			rule.GroupIDs = uniqueGroups
			// Keep the first target populated for older readers and old clients.
			rule.TargetID = rule.GroupIDs[0]
		} else if rule.TargetID <= 0 {
			return config, fmt.Errorf("规则 %q 的目标无效", rule.Name)
		}
		if rule.Role != "system" && rule.Role != "developer" && rule.Role != "user" {
			return config, fmt.Errorf("规则 %q 的消息角色无效", rule.Name)
		}
		if rule.Position != "prepend" && rule.Position != "before_first_user" && rule.Position != "after_last_user" {
			return config, fmt.Errorf("规则 %q 的注入位置无效", rule.Name)
		}
		if rule.Content == "" && (rule.Action == "append" || rule.Action == "replace_system") {
			return config, fmt.Errorf("规则 %q 的提示词内容不能为空", rule.Name)
		}
		if len([]rune(rule.Content)) > maxPromptInjectionContentRunes {
			return config, fmt.Errorf("规则 %q 的提示词内容过长", rule.Name)
		}
		models := make([]string, 0, len(rule.Models))
		for _, model := range rule.Models {
			model = strings.TrimSpace(model)
			if model != "" {
				models = append(models, model)
			}
		}
		rule.Models = models
	}
	return config, nil
}

func (s *SettingService) GetPromptInjectionConfig(ctx context.Context) (PromptInjectionConfig, error) {
	if s == nil || s.settingRepo == nil {
		return PromptInjectionConfig{Rules: []PromptInjectionRule{}}, nil
	}
	if cached, ok := s.promptInjectionCache.Load().(*cachedPromptInjectionConfig); ok && time.Since(cached.loadedAt) < promptInjectionCacheTTL {
		return cached.config, nil
	}
	value, err, _ := s.promptInjectionSF.Do("load", func() (any, error) {
		s.promptInjectionMu.Lock()
		defer s.promptInjectionMu.Unlock()
		if cached, ok := s.promptInjectionCache.Load().(*cachedPromptInjectionConfig); ok && time.Since(cached.loadedAt) < promptInjectionCacheTTL {
			return cached.config, nil
		}
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		raw, err := s.settingRepo.GetValue(readCtx, SettingKeyPromptInjectionRules)
		if errors.Is(err, ErrSettingNotFound) {
			config := PromptInjectionConfig{Rules: []PromptInjectionRule{}}
			s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: config, loadedAt: time.Now()})
			return config, nil
		}
		if err != nil {
			return PromptInjectionConfig{}, fmt.Errorf("读取提示词注入配置: %w", err)
		}
		var config PromptInjectionConfig
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return PromptInjectionConfig{}, fmt.Errorf("解析提示词注入配置: %w", err)
		}
		config, err = normalizePromptInjectionConfig(config)
		if err != nil {
			return PromptInjectionConfig{}, err
		}
		s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: config, loadedAt: time.Now()})
		return config, nil
	})
	if err != nil {
		s.promptInjectionMu.Lock()
		defer s.promptInjectionMu.Unlock()
		if cached, ok := s.promptInjectionCache.Load().(*cachedPromptInjectionConfig); ok {
			if time.Since(cached.loadedAt) >= promptInjectionCacheTTL {
				s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: cached.config, loadedAt: time.Now(), degraded: true})
			}
			slog.Warn("prompt injection using last valid configuration", "version", cached.config.Version)
			return cached.config, nil
		}
		return PromptInjectionConfig{}, err
	}
	config, ok := value.(PromptInjectionConfig)
	if !ok {
		return PromptInjectionConfig{}, fmt.Errorf("提示词注入配置缓存类型错误: %T", value)
	}
	return config, nil
}
func (s *SettingService) PromptInjectionDegraded() bool {
	if s == nil {
		return false
	}
	if c, ok := s.promptInjectionCache.Load().(*cachedPromptInjectionConfig); ok {
		return c.degraded
	}
	return false
}

func (s *SettingService) SetPromptInjectionConfig(ctx context.Context, config PromptInjectionConfig) (PromptInjectionConfig, error) {
	if s == nil || s.settingRepo == nil {
		return config, errors.New("设置服务不可用")
	}
	s.promptInjectionMu.Lock()
	defer s.promptInjectionMu.Unlock()
	oldRaw, readErr := s.settingRepo.GetValue(ctx, SettingKeyPromptInjectionRules)
	if readErr != nil && !errors.Is(readErr, ErrSettingNotFound) {
		return config, readErr
	}
	var old PromptInjectionConfig
	if oldRaw != "" {
		if err := json.Unmarshal([]byte(oldRaw), &old); err != nil {
			return config, err
		}
	}
	if config.Version != old.Version {
		return config, ErrPromptInjectionConflict
	}
	config.Version = old.Version + 1
	config, err := normalizePromptInjectionConfig(config)
	if err != nil {
		return config, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return config, fmt.Errorf("序列化提示词注入配置: %w", err)
	}
	if s == nil || s.settingRepo == nil {
		return config, errors.New("设置服务不可用")
	}
	var saveErr error
	if repo, ok := s.settingRepo.(interface {
		CompareAndSetValue(context.Context, string, string, string) (bool, error)
	}); ok {
		var changed bool
		changed, saveErr = repo.CompareAndSetValue(ctx, SettingKeyPromptInjectionRules, oldRaw, string(raw))
		if saveErr == nil && !changed {
			saveErr = ErrPromptInjectionConflict
		}
	} else {
		saveErr = s.settingRepo.Set(ctx, SettingKeyPromptInjectionRules, string(raw))
	}
	if err := saveErr; err != nil {
		return config, fmt.Errorf("保存提示词注入配置: %w", err)
	}
	s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: config, loadedAt: time.Now()})
	return config, nil
}
func (s *SettingService) RefreshPromptInjectionConfig(ctx context.Context) (PromptInjectionConfig, error) {
	if s != nil {
		s.promptInjectionMu.Lock()
		if c, ok := s.promptInjectionCache.Load().(*cachedPromptInjectionConfig); ok {
			s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: c.config, loadedAt: time.Time{}, degraded: c.degraded})
		}
		s.promptInjectionMu.Unlock()
	}
	return s.GetPromptInjectionConfig(ctx)
}

func wildcardModelMatch(pattern, model string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	model = strings.ToLower(strings.TrimSpace(model))
	if pattern == "" || pattern == "*" {
		return true
	}
	expression := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	matched, err := regexp.MatchString(expression, model)
	return err == nil && matched
}

func matchingPromptInjectionRules(config PromptInjectionConfig, groupID *int64, accountID int64, model string) []PromptInjectionRule {
	matched := make([]PromptInjectionRule, 0)
	for _, rule := range config.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Scope == "group" {
			targetMatched := false
			groupIDs := rule.GroupIDs
			if len(groupIDs) == 0 && rule.TargetID > 0 {
				groupIDs = []int64{rule.TargetID}
			}
			for _, targetGroupID := range groupIDs {
				if groupID != nil && targetGroupID == *groupID {
					targetMatched = true
					break
				}
			}
			if targetMatched {
				matched = append(matched, rule)
			}
			continue
		}
		if rule.TargetID != accountID {
			continue
		}
		// Account-scoped rules remain compatible with the original model filter.
		modelMatched := len(rule.Models) == 0
		for _, pattern := range rule.Models {
			if wildcardModelMatch(pattern, model) {
				modelMatched = true
				break
			}
		}
		if modelMatched {
			matched = append(matched, rule)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Priority != matched[j].Priority {
			return matched[i].Priority > matched[j].Priority
		}
		if matched[i].Scope != matched[j].Scope {
			return matched[i].Scope == "group"
		}
		return matched[i].ID < matched[j].ID
	})
	return matched
}

func ApplyPromptInjections(body []byte, protocol string, rules []PromptInjectionRule) ([]byte, error) {
	inputRules := []PromptInjectionRule{}
	for _, r := range rules {
		if r.Action != "replace_output" {
			inputRules = append(inputRules, r)
		}
	}
	rules = inputRules
	if len(rules) == 0 {
		return body, nil
	}
	var root map[string]any
	if !json.Valid(body) {
		return nil, fmt.Errorf("请求 JSON 无效")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("解析待注入请求: %w", err)
	}
	if root == nil {
		return nil, fmt.Errorf("请求必须是 JSON 对象")
	}
	field := "input"
	if protocol == "chat_completions" || protocol == "anthropic" {
		field = "messages"
	}
	var original []any
	switch value := root[field].(type) {
	case nil:
		original = []any{}
	case []any:
		original = value
	case string:
		original = []any{map[string]any{"role": "user", "content": value}}
	default:
		return nil, fmt.Errorf("请求字段 %s 必须是消息数组或字符串", field)
	}
	buckets := map[string][]any{"prepend": {}, "before_first_user": {}, "after_last_user": {}}
	var replacement []string
	if protocol == "anthropic" && root["system"] != nil {
		original = append([]any{map[string]any{"role": "system", "content": root["system"]}}, original...)
	}
	for _, rule := range rules {
		switch rule.Action {
		case "replace_output":
			continue
		case "replace_input":
			if rule.WholeInput {
				for i := len(original) - 1; i >= 0; i-- {
					if m, ok := original[i].(map[string]any); ok && m["role"] == "user" && managedContentHasText(m["content"]) {
						m["content"] = replaceWholeManagedContent(m["content"], rule.Content)
						break
					}
				}
				continue
			}
			for _, item := range original {
				if m, ok := item.(map[string]any); ok && m["role"] == "user" {
					next, err := replaceManagedContent(m["content"], rule.Match, rule.Content)
					if err != nil {
						return nil, err
					}
					m["content"] = next
				}
			}
			continue
		case "replace_system":
			replacement = append(replacement, rule.Content)
			continue
		}
		buckets[rule.Position] = append(buckets[rule.Position], map[string]any{"role": rule.Role, "content": rule.Content})
	}
	if len(replacement) > 0 {
		delete(root, "instructions")
		filtered := make([]any, 0, len(original))
		for _, item := range original {
			if m, ok := item.(map[string]any); ok && m["role"] == "system" {
				continue
			}
			filtered = append(filtered, item)
		}
		original = filtered
		if protocol == "responses" {
			root["instructions"] = strings.Join(replacement, "\n\n")
		} else {
			buckets["prepend"] = append([]any{map[string]any{"role": "system", "content": strings.Join(replacement, "\n\n")}}, buckets["prepend"]...)
		}
	}
	firstUser, lastUser := len(original), -1
	for i, item := range original {
		message, ok := item.(map[string]any)
		if !ok || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(message["role"])), "user") {
			continue
		}
		if firstUser == len(original) {
			firstUser = i
		}
		lastUser = i
	}
	result := append([]any{}, buckets["prepend"]...)
	for i, item := range original {
		if i == firstUser {
			result = append(result, buckets["before_first_user"]...)
		}
		result = append(result, item)
		if i == lastUser {
			result = append(result, buckets["after_last_user"]...)
		}
	}
	if firstUser == len(original) {
		result = append(result, buckets["before_first_user"]...)
	}
	if lastUser == -1 {
		result = append(result, buckets["after_last_user"]...)
	}
	root[field] = result
	if protocol == "anthropic" {
		var blocks []any
		messages := []any{}
		for _, item := range result {
			m, ok := item.(map[string]any)
			if ok && (m["role"] == "system" || m["role"] == "developer") {
				switch v := m["content"].(type) {
				case string:
					blocks = append(blocks, map[string]any{"type": "text", "text": v})
				case []any:
					blocks = append(blocks, v...)
				}
				continue
			}
			messages = append(messages, item)
		}
		root["messages"] = messages
		if len(blocks) == 0 {
			delete(root, "system")
		} else {
			root["system"] = blocks
		}
	}
	updated, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("序列化注入后的请求: %w", err)
	}
	return updated, nil
}

func (s *OpenAIGatewayService) applyManagedPromptInjections(ctx context.Context, c interface{ Get(string) (any, bool) }, account *Account, model, protocol string, body []byte) ([]byte, error) {
	alreadyApplied := false
	if c != nil {
		if done, ok := c.Get("managed_policy_applied"); ok && done == true {
			alreadyApplied = true
		}
	}
	if s == nil || s.settingService == nil || account == nil {
		return body, nil
	}
	config, err := s.settingService.GetPromptInjectionConfig(ctx)
	if err != nil {
		return nil, err
	}
	var groupID *int64
	if c != nil {
		if apiKey := getAPIKeyFromContext(c); apiKey != nil {
			groupID = apiKey.GroupID
		}
	}
	rules := matchingPromptInjectionRules(config, groupID, account.ID, model)
	if alreadyApplied {
		filtered := []PromptInjectionRule{}
		for _, r := range rules {
			if r.Scope == "account" {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}
	slog.Info("prompt injection matched", "account_id", account.ID, "group_id", groupID, "version", config.Version, "rule_count", len(rules), "protocol", protocol)
	return ApplyPromptInjections(body, protocol, rules)
}

func replaceManagedContent(content any, match, replacement string) (any, error) {
	if match == "" {
		return content, fmt.Errorf("匹配文字不能为空")
	}
	replace := func(text string) (string, error) {
		count := strings.Count(text, match)
		if count == 0 {
			return text, nil
		}
		size := int64(len(text)) + int64(count)*int64(len(replacement)-len(match))
		if size > 4*1024*1024 {
			return "", fmt.Errorf("输入替换结果过大")
		}
		return strings.ReplaceAll(text, match, replacement), nil
	}
	if text, ok := content.(string); ok {
		return replace(text)
	}
	if parts, ok := content.([]any); ok {
		for _, part := range parts {
			if p, ok := part.(map[string]any); ok && (p["type"] == "text" || p["type"] == "input_text") {
				if text, ok := p["text"].(string); ok {
					next, err := replace(text)
					if err != nil {
						return nil, err
					}
					p["text"] = next
				}
			}
		}
	}
	return content, nil
}
func managedContentHasText(content any) bool {
	if _, ok := content.(string); ok {
		return true
	}
	if parts, ok := content.([]any); ok {
		for _, v := range parts {
			if p, ok := v.(map[string]any); ok && (p["type"] == "text" || p["type"] == "input_text") {
				return true
			}
		}
	}
	return false
}
func replaceWholeManagedContent(content any, value string) any {
	if _, ok := content.(string); ok {
		return value
	}
	parts, _ := content.([]any)
	result := []any{}
	inserted := false
	for _, v := range parts {
		if p, ok := v.(map[string]any); ok && (p["type"] == "text" || p["type"] == "input_text") {
			if !inserted {
				p["text"] = value
				result = append(result, p)
				inserted = true
			}
			continue
		}
		result = append(result, v)
	}
	return result
}
func ManagedGroupRules(config PromptInjectionConfig, groupID int64) []PromptInjectionRule {
	return matchingPromptInjectionRules(config, &groupID, 0, "")
}
