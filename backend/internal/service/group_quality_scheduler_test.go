package service

import (
	"context"
	"testing"
)

type qualityScheduleStub struct {
	GroupQualityRepository
	job     *QualityScheduledCheck
	outcome string
	calls   int
}

func (r *qualityScheduleStub) WithScheduleLock(ctx context.Context, f func(context.Context) error) error {
	return f(ctx)
}
func (r *qualityScheduleStub) ClaimDue(context.Context) (*QualityScheduledCheck, error) {
	r.calls++
	return r.job, nil
}
func (r *qualityScheduleStub) GetConfig(context.Context, int64) (*GroupQualityConfig, error) {
	return &GroupQualityConfig{Enabled: true, AutoEnabled: false}, nil
}
func (r *qualityScheduleStub) RecordScheduleOutcome(_ context.Context, _ *QualityScheduledCheck, s string) error {
	r.outcome = s
	return nil
}
func TestQualityAutomaticNoDueJobNeverCallsUpstream(t *testing.T) {
	r := &qualityScheduleStub{}
	s := NewGroupQualityService(r, nil, nil, nil, nil, nil, nil)
	s.automaticTick(context.Background())
	if r.calls != 1 || r.outcome != "" {
		t.Fatal("unexpected automatic execution")
	}
}
func TestQualityAutomaticDisabledAfterClaimDoesNotCallUpstream(t *testing.T) {
	r := &qualityScheduleStub{job: &QualityScheduledCheck{GroupID: 1, Model: "gpt-6-sol"}}
	s := NewGroupQualityService(r, nil, nil, nil, nil, nil, nil)
	s.automaticTick(context.Background())
	if r.outcome != "skipped" {
		t.Fatal("disabled schedule executed")
	}
}
