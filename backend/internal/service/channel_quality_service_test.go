package service

import (
	"reflect"
	"testing"
)

func TestChannelQualityEligibilityDoesNotDependOnHealthModel(t *testing.T) {
	monitor := &ChannelMonitor{Enabled: true, Provider: "openai", PrimaryModel: "gpt-6.1-sol", APIKey: "encrypted-test-key"}
	want := []string{"gpt-6-astra", "gpt-6.1-sol"}
	if got := channelQualityModels(monitor); !reflect.DeepEqual(got, want) {
		t.Fatalf("health model must not hide quality models: %v", got)
	}
	if monitor.PrimaryModel != "gpt-6.1-sol" || len(monitor.ExtraModels) != 0 {
		t.Fatal("quality eligibility changed the health-check configuration")
	}
	for _, test := range []struct {
		name   string
		mutate func(*ChannelMonitor)
	}{
		{"disabled", func(m *ChannelMonitor) { m.Enabled = false }},
		{"quota_only", func(m *ChannelMonitor) { m.CheckMode = MonitorCheckModeQuota }},
		{"missing_key", func(m *ChannelMonitor) { m.APIKey = "" }},
		{"decrypt_failed", func(m *ChannelMonitor) { m.APIKeyDecryptFailed = true }},
		{"other_provider", func(m *ChannelMonitor) { m.Provider = "anthropic" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := *monitor
			test.mutate(&m)
			if got := channelQualityModels(&m); len(got) != 0 {
				t.Fatalf("unavailable channel permits testing: %v", got)
			}
		})
	}
}
