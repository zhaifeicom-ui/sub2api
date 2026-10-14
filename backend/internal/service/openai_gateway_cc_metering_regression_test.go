//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesChatFallbackMissingMetering(t *testing.T) {
	content := "data: {\"id\":\"chat-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"
	usage := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n"
	for _, tc := range []struct {
		name, tail, code string
		wantInput        int
	}{
		{"truncated_without_usage", "", "upstream_usage_missing", 0},
		{"completed_without_usage", "data: [DONE]\n\n", "", 0},
		{"truncated_with_usage", usage, "upstream_stream_incomplete", 10},
		{"complete_with_usage", usage + "data: [DONE]\n\n", "", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(content + tc.tail))}
			result, err := svc.streamChatCompletionsAsResponses(c, resp, "kimi-k3", nil, nil, false, nil, "kimi-k3", "kimi-k3", nil, nil, time.Now())
			require.NotNil(t, result)
			require.Equal(t, tc.wantInput, result.Usage.InputTokens)
			require.Equal(t, tc.wantInput == 0, result.UsageMissing)
			require.Greater(t, result.EstimatedOutputTokens, 0)
			if tc.code == "" {
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), "response.completed")
			} else {
				require.Error(t, err)
				require.Contains(t, rec.Body.String(), tc.code)
				require.Contains(t, rec.Body.String(), "response.failed")
				require.NotContains(t, rec.Body.String(), "response.completed")
				require.NotContains(t, rec.Body.String(), "[DONE]")
			}
		})
	}
}
