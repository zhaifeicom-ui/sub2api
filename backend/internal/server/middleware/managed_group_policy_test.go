package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type managedPolicySettingStub struct {
	service.SettingRepository
	raw string
}

type failingManagedWriter struct{ gin.ResponseWriter }

func (w failingManagedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestManagedWriterPropagatesClientWriteFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Header("Content-Type", "text/event-stream")
	w := &managedPolicyWriter{ResponseWriter: failingManagedWriter{c.Writer}, replacer: service.NewManagedTextReplacer(nil)}
	_, err := w.Write([]byte("data: [DONE]\n\n"))
	require.True(t, errors.Is(err, io.ErrClosedPipe))
	_, err = w.Write([]byte("data: [DONE]\n\n"))
	require.True(t, errors.Is(err, io.ErrClosedPipe))
}

func (r managedPolicySettingStub) GetValue(context.Context, string) (string, error) {
	return r.raw, nil
}
func TestManagedMiddlewarePreservesLegacyInputShapes(t *testing.T) {
	config := service.PromptInjectionConfig{Rules: []service.PromptInjectionRule{{ID: "x", Name: "x", Enabled: true, Scope: "group", TargetID: 1, Action: "replace_input", Match: "hello", Content: "你好", Role: "user", Position: "prepend"}}}
	raw, _ := json.Marshal(config)
	for _, tc := range []struct{ path, body, field string }{
		{"/v1/chat/completions", `{"model":"m","input":"hello"}`, "input"},
		{"/v1/responses", `{"model":"m","messages":[{"role":"user","content":"hello"}]}`, "messages"},
	} {
		svc := service.NewSettingService(managedPolicySettingStub{raw: string(raw)}, nil)
		r := gin.New()
		r.POST(tc.path, func(c *gin.Context) { id := int64(1); c.Set("api_key", &service.APIKey{GroupID: &id}); c.Next() }, ManagedGroupPolicy(svc), func(c *gin.Context) {
			b, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			c.Data(200, "application/json", b)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), "你好")
		require.True(t, gjson.Get(w.Body.String(), tc.field).Exists())
	}
}

func TestManagedWriterCrossPacketAndFinalTail(t *testing.T) {
	for _, parts := range [][]string{{"hello ", "model", " name!"}, {"model name!"}, {"mod", "el na", "me!"}} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Header("Content-Type", "text/event-stream")
		rules := []service.PromptInjectionRule{{Action: "replace_output", Match: "model name", Content: "站点"}}
		w := &managedPolicyWriter{ResponseWriter: c.Writer, rules: rules, replacer: service.NewManagedTextReplacer(rules)}
		for _, part := range parts {
			payload := `data: {"choices":[{"index":0,"delta":{"content":"` + part + `"}}]}` + "\n\n"
			for _, b := range []byte(payload) {
				_, err := w.Write([]byte{b})
				require.NoError(t, err)
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.finish()
		text := ""
		for _, line := range strings.Split(recorder.Body.String(), "\n") {
			if strings.HasPrefix(line, "data: {") {
				text += gjson.Get(line[6:], "choices.0.delta.content").String()
			}
		}
		require.Equal(t, service.ReplaceManagedOutputText(strings.Join(parts, ""), rules), text)
		require.Contains(t, recorder.Body.String(), "[DONE]")
	}
}
func TestManagedWriterNeverRewritesToolArgumentsOrUsage(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Header("Content-Type", "text/event-stream")
	rules := []service.PromptInjectionRule{{Action: "replace_output", Match: "secret", Content: "new"}}
	w := &managedPolicyWriter{ResponseWriter: c.Writer, rules: rules, replacer: service.NewManagedTextReplacer(rules)}
	tool := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"secret\",\"function\":{\"arguments\":\"secret\"}}]}}],\"usage\":{\"total_tokens\":99}}\n\n"
	_, _ = w.Write([]byte(tool))
	w.finish()
	require.Equal(t, tool, recorder.Body.String())
}
