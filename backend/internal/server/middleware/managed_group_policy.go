package middleware

import (
	"bytes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"io"
	"log/slog"
	"strings"
)

// HTTP text protocols only. WebSocket has its separate per-frame injection path.
func ManagedGroupPolicy(settings *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.TrimRight(c.Request.URL.Path, "/")
		if c.Request.Method != "POST" || !(strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/messages")) {
			c.Next()
			return
		}
		v, ok := c.Get("api_key")
		key, _ := v.(*service.APIKey)
		if !ok || key == nil || key.GroupID == nil {
			c.Next()
			return
		}
		cfg, err := settings.GetPromptInjectionConfig(c.Request.Context())
		protocol := "responses"
		if strings.HasSuffix(path, "/chat/completions") {
			protocol = "chat_completions"
		}
		if strings.HasSuffix(path, "/messages") {
			protocol = "anthropic"
		}
		event := service.ManagedPolicyEvent{RequestID: c.Writer.Header().Get("X-Request-ID"), GroupID: *key.GroupID, Status: "configuration_error", Protocol: protocol, RuleIDs: []string{}}
		var probe *service.ManagedTransportProbe
		defer func() {
			// No-rule traffic must not incur a database write on every request.
			if event.Status == "no_match" {
				return
			}
			if probe != nil {
				event.TransportStatus = probe.Status()
			}
			settings.RecordManagedPolicy(c.Request.Context(), event)
		}()
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"message": "Managed prompt configuration unavailable", "type": "api_error"}})
			return
		}
		rules := service.ManagedGroupRules(cfg, *key.GroupID)
		event.Version = cfg.Version
		event.Status = "no_match"
		if len(rules) == 0 {
			c.Next()
			return
		}
		event.Status = "transformation_error"
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatus(400)
			return
		}
		if protocol == "chat_completions" && !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists() {
			protocol = "responses"
		}
		if protocol == "responses" && !gjson.GetBytes(body, "input").Exists() && gjson.GetBytes(body, "messages").Exists() {
			protocol = "chat_completions"
		}
		event.Protocol = protocol
		outputRules := []service.PromptInjectionRule{}
		for _, r := range rules {
			if r.Action == "replace_output" {
				outputRules = append(outputRules, r)
			}
		}
		if len(outputRules) > 0 && gjson.GetBytes(body, "n").Int() > 1 {
			c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"message": "Output replacement currently supports n=1 only", "type": "invalid_request_error"}})
			return
		}
		updated, err := service.ApplyPromptInjections(body, protocol, rules)
		if err != nil {
			c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"message": "Managed prompt transformation failed", "type": "invalid_request_error"}})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(updated))
		probe = service.NewManagedTransportProbe(rules)
		c.Request = c.Request.WithContext(service.WithManagedTransportProbe(c.Request.Context(), probe))
		event.Status = "processed_inbound"
		if settings.PromptInjectionDegraded() {
			event.Status = "processed_inbound_stale_config"
		}
		event.Before = int64(len(body))
		event.After = int64(len(updated))
		c.Request.ContentLength = int64(len(updated))
		c.Request.Header.Del("Content-Length")
		c.Set("managed_policy_applied", true)
		ids := []string{}
		for _, r := range rules {
			ids = append(ids, r.ID)
		}
		c.Set("managed_policy_trace", gin.H{"version": cfg.Version, "rule_ids": ids, "group_id": *key.GroupID, "status": "applied", "protocol": protocol})
		event.RuleIDs = ids
		slog.Info("managed group policy applied", "request_id", c.GetHeader("X-Request-ID"), "group_id", *key.GroupID, "version", cfg.Version, "rule_ids", ids, "protocol", protocol)
		if len(outputRules) == 0 {
			c.Next()
			return
		}
		writer := &managedPolicyWriter{ResponseWriter: c.Writer, rules: outputRules, replacer: service.NewManagedTextReplacer(outputRules)}
		c.Writer = writer
		defer func() {
			writer.finish()
			if writer.writeErr != nil {
				event.Status = "output_write_error"
			} else if writer.bypass || writer.limited {
				event.Status = "output_bypassed_size_limit"
			} else if event.Status == "processed_inbound" {
				event.Status = "processed_inbound_and_output"
			}
		}()
		c.Next()
	}
}

type managedPolicyWriter struct {
	gin.ResponseWriter
	rules         []service.PromptInjectionRule
	replacer      *service.ManagedTextReplacer
	buffer        []byte
	pendingData   []byte
	pendingPrefix string
	pendingPath   string
	pendingReady  string
	channel       string
	bypass        bool
	limited       bool
	writeErr      error
}

func (w *managedPolicyWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *managedPolicyWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if w.bypass || w.Status() < 200 || w.Status() >= 300 {
		return w.ResponseWriter.Write(p)
	}
	w.Header().Del("Content-Length")
	w.Header().Del("ETag")
	w.buffer = append(w.buffer, p...)
	// Bound memory even when an upstream emits a huge image/tool event. No media rewriting.
	if len(w.buffer) > 2*1024*1024 {
		w.flushPending()
		w.writeRaw(w.buffer)
		w.buffer = nil
		w.bypass = true
		slog.Warn("managed output replacement bypassed oversized event")
		return len(p), w.writeErr
	}
	if strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		for {
			end := bytes.Index(w.buffer, []byte("\n\n"))
			size := 2
			if cr := bytes.Index(w.buffer, []byte("\r\n\r\n")); cr >= 0 && (end < 0 || cr < end) {
				end = cr
				size = 4
			}
			if end < 0 {
				break
			}
			frame := append([]byte(nil), w.buffer[:end]...)
			w.buffer = w.buffer[end+size:]
			w.frame(frame)
		}
	}
	return len(p), w.writeErr
}
func (w *managedPolicyWriter) Flush() { w.ResponseWriter.Flush() }
func (w *managedPolicyWriter) writeRaw(data []byte) {
	if w.writeErr != nil {
		return
	}
	n, err := w.ResponseWriter.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.writeErr = err
}
func (w *managedPolicyWriter) emit(prefix string, data []byte) {
	w.writeRaw([]byte(prefix + "data: " + string(data) + "\n\n"))
}
func (w *managedPolicyWriter) flushPending() {
	if w.pendingData == nil {
		return
	}
	tail := w.pendingReady + w.replacer.Feed("", true)
	data, _ := sjson.SetBytes(w.pendingData, w.pendingPath, tail)
	w.emit(w.pendingPrefix, data)
	w.pendingData = nil
	w.pendingReady = ""
	w.channel = ""
	w.replacer = service.NewManagedTextReplacer(w.rules)
}
func (w *managedPolicyWriter) frame(raw []byte) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	prefix := ""
	var data []byte
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			if data != nil {
				w.flushPending()
				w.writeRaw(append(raw, []byte("\n\n")...))
				return
			}
			data = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		} else {
			prefix += line + "\n"
		}
	}
	if data == nil {
		w.writeRaw(append(raw, []byte("\n\n")...))
		return
	}
	path := service.ManagedDeltaPath(data)
	if path == "" {
		w.flushPending()
		w.emit(prefix, service.ManagedOutputJSON(data, w.rules))
		return
	}
	channel := path + gjson.GetBytes(data, "item_id").String() + "/" + gjson.GetBytes(data, "content_index").Raw + "/" + gjson.GetBytes(data, "index").Raw
	if strings.HasPrefix(gjson.GetBytes(data, "type").String(), "content_block_") {
		channel = "anthropic/" + gjson.GetBytes(data, "index").Raw
	}
	if w.pendingData != nil && channel != w.channel {
		w.flushPending()
	}
	text := w.replacer.Feed(gjson.GetBytes(data, path).String(), false)
	if w.replacer.Limited() {
		w.limited = true
	}
	if w.pendingData != nil {
		out, _ := sjson.SetBytes(w.pendingData, w.pendingPath, w.pendingReady+text)
		w.emit(w.pendingPrefix, out)
		w.pendingReady = ""
	} else {
		w.pendingReady = text
	}
	// An unsent frame carries any remaining tail, preserving event ordering/IDs.
	if data != nil {
		w.pendingData = data
		w.pendingPrefix = prefix
		w.pendingPath = path
		w.channel = channel
	}
}
func (w *managedPolicyWriter) finish() {
	if w.bypass {
		return
	}
	if strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		w.flushPending()
		if len(w.buffer) > 0 {
			w.writeRaw(w.buffer)
		}
	} else if len(w.buffer) > 0 {
		w.writeRaw(service.ManagedOutputJSON(w.buffer, w.rules))
	}
	w.ResponseWriter.Flush()
}
