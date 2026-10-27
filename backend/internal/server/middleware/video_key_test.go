package middleware

import (
	"net/http"
	"testing"
)

func TestVideoKeyEndpointIsolation(t *testing.T) {
	for _, p := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/images/generations", "/v1/task/submit/extra", "/v1/skills"} {
		if isVideoAPIPath(http.MethodPost, p) {
			t.Fatalf("video key entered %s", p)
		}
	}
	for _, p := range []string{"/v1/task/video_1", "/v1/skills", "/v1/skills/guide", "/v1/models"} {
		if !isVideoAPIPath(http.MethodGet, p) {
			t.Fatalf("video key blocked from %s", p)
		}
	}
}
