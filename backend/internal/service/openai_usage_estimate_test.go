package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCCUsageEstimateScopeAndText(t *testing.T) {
	t.Setenv("SUB2API_ESTIMATED_USAGE_GROUP_IDS", "55")
	require.True(t, ccUsageEstimateEnabled(55))
	require.False(t, ccUsageEstimateEnabled(5))
	require.Equal(t, 2, estimateCCInput([]byte(`{"messages":[{"content":[{"text":"abcdefgh"},{"image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`)))
	require.Equal(t, 0, estimateCCInput([]byte(`invalid`)))
}

func TestRecordUsageConservativeEstimate(t *testing.T) {
	t.Setenv("SUB2API_ESTIMATED_USAGE_GROUP_IDS", "55")
	for _, tc := range []struct {
		name      string
		group     int64
		missing   bool
		output    int
		estimated bool
	}{
		{"enabled", 55, true, 100, true},
		{"other_group", 56, true, 100, false},
		{"authoritative_zero", 55, false, 100, false},
		{"no_generated_output", 55, true, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(repo, users, &openAIRecordUsageSubRepoStub{}, nil)
			result := &OpenAIForwardResult{Model: "gpt-5.1", RequestID: "estimate-test", Stream: true, UsageMissing: tc.missing, EstimatedInputTokens: 275000, EstimatedOutputTokens: tc.output}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 2, GroupID: &tc.group}, User: &User{ID: 1}, Account: &Account{ID: 3}})
			require.NoError(t, err)
			require.Zero(t, result.Usage.InputTokens, "do not mutate forwarding result")
			if tc.estimated {
				require.Equal(t, "estimated_usage_cache_input", *repo.lastLog.BillingTier)
				require.Zero(t, repo.lastLog.InputTokens)
				require.Equal(t, 275000, repo.lastLog.CacheReadTokens)
				require.Equal(t, 100, repo.lastLog.OutputTokens)
				require.Greater(t, repo.lastLog.ActualCost, 0.0)
				require.Equal(t, 1, users.deductCalls)
			} else {
				require.Nil(t, repo.lastLog.BillingTier)
				require.Zero(t, repo.lastLog.ActualCost)
			}
		})
	}
}
