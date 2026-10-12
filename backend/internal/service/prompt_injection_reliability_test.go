package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type managedConfigRepo struct {
	SettingRepository
	mu      sync.Mutex
	raw     string
	err     error
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *managedConfigRepo) GetValue(context.Context, string) (string, error) {
	r.mu.Lock()
	raw, err := r.raw, r.err
	r.mu.Unlock()
	if r.entered != nil {
		r.once.Do(func() { close(r.entered); <-r.release })
	}
	return raw, err
}
func (r *managedConfigRepo) Set(_ context.Context, _ string, v string) error {
	r.mu.Lock()
	r.raw = v
	r.mu.Unlock()
	return nil
}
func managedTestConfig(content string) PromptInjectionConfig {
	return PromptInjectionConfig{Rules: []PromptInjectionRule{{ID: "r", Name: "r", Enabled: true, Scope: "group", TargetID: 1, Role: "system", Position: "prepend", Content: content}}}
}
func TestManagedCacheCannotOverwriteSavedRules(t *testing.T) {
	raw, _ := json.Marshal(managedTestConfig("old"))
	r := &managedConfigRepo{raw: string(raw), entered: make(chan struct{}), release: make(chan struct{})}
	s := &SettingService{settingRepo: r}
	read := make(chan error, 1)
	go func() { _, e := s.GetPromptInjectionConfig(context.Background()); read <- e }()
	<-r.entered
	saved := make(chan error, 1)
	go func() { _, e := s.SetPromptInjectionConfig(context.Background(), managedTestConfig("new")); saved <- e }()
	close(r.release)
	require.NoError(t, <-read)
	require.NoError(t, <-saved)
	got, e := s.GetPromptInjectionConfig(context.Background())
	require.NoError(t, e)
	require.Equal(t, "new", got.Rules[0].Content)
	_, e = s.SetPromptInjectionConfig(context.Background(), managedTestConfig("stale"))
	require.Error(t, e)
}
func TestManagedCachedRulesSurviveReadFailure(t *testing.T) {
	s := &SettingService{settingRepo: &managedConfigRepo{err: errors.New("db unavailable")}}
	s.promptInjectionCache.Store(&cachedPromptInjectionConfig{config: managedTestConfig("good"), loadedAt: time.Now().Add(-2 * time.Minute)})
	cfg, e := s.GetPromptInjectionConfig(context.Background())
	require.NoError(t, e)
	require.Equal(t, "good", cfg.Rules[0].Content)
	require.True(t, s.PromptInjectionDegraded())
	cfg, e = s.GetPromptInjectionConfig(context.Background())
	require.NoError(t, e)
	require.Equal(t, "good", cfg.Rules[0].Content)
}
