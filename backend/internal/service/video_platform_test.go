package service

import "testing"

func TestVideoAccountRequiresDedicatedProtocol(t *testing.T) {
	for _, v := range []struct {
		kind, protocol string
		bad            bool
	}{{"apikey", "movo_video", false}, {"apikey", "laneai_task", true}, {"oauth", "laneai_task", true}, {"apikey", "chat_completions", true}, {"apikey", "", true}} {
		_, e := buildAccountForCreate(&CreateAccountInput{Name: "video", Platform: PlatformVideo, Type: v.kind, Credentials: map[string]any{"api_protocol": v.protocol, "base_url": "https://movoapi.top", "api_key": "test-key"}}, map[string]any{})
		if (e != nil) != v.bad {
			t.Fatalf("kind=%s protocol=%s err=%v", v.kind, v.protocol, e)
		}
	}
	if len(defaultModelsListCandidateIDs(PlatformVideo)) != 0 {
		t.Fatal("video inherited default chat models")
	}
}
