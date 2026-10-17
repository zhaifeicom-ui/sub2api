package service

import "testing"

func TestNodeRoleSchedules(t *testing.T) {
	for _, tc := range []struct {
		role    string
		enabled bool
	}{{"", true}, {"primary", true}, {"secondary", false}, {" SECONDARY ", false}} {
		t.Run(tc.role, func(t *testing.T) {
			t.Setenv("SUB2API_NODE_ROLE", tc.role)
			if primarySchedulesEnabled() != tc.enabled {
				t.Fatal("unexpected schedule role")
			}
		})
	}
}

func TestMultiNodeDisablesExclusiveStartupCleanup(t *testing.T) {
	t.Setenv("SUB2API_MULTI_NODE", "1")
	if exclusiveStartupSlotCleanupEnabled() {
		t.Fatal("must not delete another node's live slots")
	}
	t.Setenv("SUB2API_MULTI_NODE", "")
	if !exclusiveStartupSlotCleanupEnabled() {
		t.Fatal("single-node behavior changed")
	}
}

func TestSecondaryProvidersDoNotStartPaidSchedules(t *testing.T) {
	t.Setenv("SUB2API_NODE_ROLE", "secondary")
	runner := ProvideScheduledTestRunnerService(nil, nil, nil, nil, nil)
	defer runner.Stop()
	if runner.cron != nil {
		t.Fatal("secondary started scheduled account tests")
	}
	quality := ProvideChannelQualityService(nil, nil, nil, nil)
	defer quality.StopAutomatic()
	if quality.autoCancel != nil {
		t.Fatal("secondary started quality tests")
	}
	monitor := ProvideChannelMonitorRunner(nil, nil, nil)
	defer monitor.Stop()
	if monitor.started {
		t.Fatal("secondary started active channel probes")
	}
}
