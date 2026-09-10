package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type PromptInjectionConfig struct {
	Rules []PromptInjectionRule `json:"rules"`
}

type PromptInjectionRule struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Scope    string   `json:"scope"`
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
		rule.Content = strings.TrimSpace(rule.Content)
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
		if rule.TargetID <= 0 {
			return config, fmt.Errorf("规则 %q 的目标无效", rule.Name)
		}
		if rule.Role != "system" && rule.Role != "developer" && rule.Role != "user" {
			return config, fmt.Errorf("规则 %q 的消息角色无效", rule.Name)
		}
		if rule.Position != "prepend" && rule.Position != "before_first_user" && rule.Position != "after_last_user" {
			return config, fmt.Errorf("规则 %q 的注入位置无效", rule.Name)
		}
		if rule.Content == "" {
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
		if cached, ok := s.promptInjectionCache.Load().(*cachedPromptInjectionConfig); ok && time.Since(cached.loadedAt) < promptInjectionCacheTTL {
			return cached.config, nil
		}
		raw, err := s.settingRepo.GetValue(ctx, SettingKeyPromptInjectionRules)
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
		return PromptInjectionConfig{}, err
	}
	return value.(PromptInjectionConfig), nil
}

func (s *SettingService) SetPromptInjectionConfig(ctx context.Context, config PromptInjectionConfig) (PromptInjectionConfig, error) {
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
	if err := s.settingRepo.Set(ctx, SettingKeyPromptInjectionRules, string(raw)); err != nil {
		return config, fmt.Errorf("保存提示词注入配置: %w", err)
	}
	s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: config, loadedAt: time.Now()})
	return config, nil
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
		targetMatched := rule.Scope == "account" && rule.TargetID == accountID
		if rule.Scope == "group" && groupID != nil && rule.TargetID == *groupID {
			targetMatched = true
		}
		if !targetMatched {
			continue
		}
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
	if len(rules) == 0 {
		return body, nil
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("解析待注入请求: %w", err)
	}
	field := "input"
	if protocol == "chat_completions" {
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
	for _, rule := range rules {
		buckets[rule.Position] = append(buckets[rule.Position], map[string]any{"role": rule.Role, "content": rule.Content})
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
	updated, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("序列化注入后的请求: %w", err)
	}
	return updated, nil
}

func (s *OpenAIGatewayService) applyManagedPromptInjections(ctx context.Context, c interface{ Get(string) (any, bool) }, account *Account, model, protocol string, body []byte) ([]byte, error) {
	if s == nil || s.settingService == nil || account == nil {
		return body, nil
	}
	config, err := s.settingService.GetPromptInjectionConfig(ctx)
	if err != nil {
		// 配置读取异常时保持网关可用；管理接口仍会明确返回错误。
		return body, nil
	}
	var groupID *int64
	if c != nil {
		if apiKey := getAPIKeyFromContext(c); apiKey != nil {
			groupID = apiKey.GroupID
		}
	}
	rules := matchingPromptInjectionRules(config, groupID, account.ID, model)
	return ApplyPromptInjections(body, protocol, rules)
}
