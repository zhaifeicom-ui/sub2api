package videostudio

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func testChannel() Channel {
	return Channel{GroupID: 1, AccountID: 1, PublicBaseURL: "https://example.com", Enabled: true, MaxConcurrent: 1, Models: []Model{{ID: "MiniMax-H3", Mode: "second", Rates: map[string]float64{"768p": .22, "2k": .35}, CostCNY: map[string]float64{"768p": .22, "2k": .35}, MinSeconds: 4, MaxSeconds: 30, MaxImages: 9, MaxVideos: 1, MaxAudios: 1, Audio: true}, {ID: "MiniMax-H3 2K", Mode: "job", Rates: map[string]float64{"2k": 2.2}, CostCNY: map[string]float64{"2k": 2.2}, MinSeconds: 1, MaxSeconds: 15, MaxImages: 9, MaxVideos: 3, MaxAudios: 3, Audio: true}}}
}
func testRequest() Request {
	return Request{KeyID: 1, Model: "MiniMax-H3", Input: Input{Prompt: "一只猫在花园散步", Duration: 4, Resolution: "768p", AspectRatio: "16:9"}}
}
func TestLanePricingAndCapabilities(t *testing.T) {
	c := testChannel()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		model, res string
		seconds    int
		want       float64
		bad        bool
	}{{"short per second", "MiniMax-H3", "768p", 4, .88, false}, {"long per second", "MiniMax-H3", "2k", 30, 10.5, false}, {"flat job", "MiniMax-H3 2K", "2k", 15, 2.2, false}, {"wrong resolution", "MiniMax-H3 2K", "768p", 10, 0, true}, {"flat duration cap", "MiniMax-H3 2K", "2k", 16, 0, true}, {"unknown model", "other", "768p", 4, 0, true}, {"under minimum", "MiniMax-H3", "768p", 1, 0, true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRequest()
			r.Model = tt.model
			r.Input.Resolution = tt.res
			r.Input.Duration = tt.seconds
			p, e := Quote(c, r)
			if (e != nil) != tt.bad {
				t.Fatalf("error=%v", e)
			}
			if !tt.bad && math.Abs(p.Amount-tt.want) > 1e-9 {
				t.Fatalf("got %.8f want %.8f", p.Amount, tt.want)
			}
		})
	}
	r := testRequest()
	r.Input.Images = make([]string, 10)
	if _, e := Quote(c, r); e == nil {
		t.Fatal("too many images accepted")
	}
	r = testRequest()
	r.Input.Prompt = strings.Repeat("字", 7001)
	if _, e := Quote(c, r); e == nil {
		t.Fatal("long prompt accepted")
	}
}
func TestLaneOriginRejectsCredentialLeaks(t *testing.T) {
	for _, s := range []string{"http://laneai.dev", "https://laneai.dev.evil.test/v1", "https://laneai.dev@evil.test", "https://127.0.0.1", "https://laneai.dev:443"} {
		if _, e := laneOrigin(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	for _, s := range []string{"https://laneai.dev", "https://laneai.dev/v1"} {
		if got, e := laneOrigin(s); e != nil || got != "https://laneai.dev" {
			t.Fatalf("%s %v", got, e)
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestLaneTaskContractUsesNestedInput(t *testing.T) {
	w := NewWorker(nil)
	w.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/task/submit" || r.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("wrong request %v", r.URL)
		}
		var p map[string]json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
			t.Fatal(e)
		}
		if _, ok := p["input"]; !ok {
			t.Fatal("input envelope missing")
		}
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{"id":"task_a","status":"queued"}`)), Header: make(http.Header)}, nil
	})
	out, status, e := w.call(context.Background(), "https://laneai.dev", "test", "POST", "/v1/task/submit", map[string]any{"model": "MiniMax-H3", "input": testRequest().Input})
	if e != nil || status != 202 || out.ID != "task_a" {
		t.Fatalf("%+v %d %v", out, status, e)
	}
}

func TestLaneTaskMaterialContract(t *testing.T) {
	for _, model := range []string{"MiniMax-H3", "MiniMax-H3 768P", "MiniMax-H3 2K"} {
		t.Run(model, func(t *testing.T) {
			b, err := json.Marshal(laneTaskPayload(model, testRequest().Input, []string{"https://station.example/image"}, []string{"https://station.example/video"}, []string{"https://station.example/audio"}))
			if err != nil {
				t.Fatal(err)
			}
			var p struct {
				Model string                     `json:"model"`
				Input map[string]json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatal(err)
			}
			if p.Model != model {
				t.Fatal("model changed")
			}
			for field, kind := range map[string]string{"materials": "image", "video_references": "video", "audio_references": "audio"} {
				var refs []laneMaterialReference
				if err := json.Unmarshal(p.Input[field], &refs); err != nil {
					t.Fatalf("%s must use MaterialReference objects: %v", field, err)
				}
				if len(refs) != 1 || refs[0].URL != "https://station.example/"+kind {
					t.Fatalf("wrong %s: %+v", field, refs)
				}
			}
			for _, field := range []string{"images", "videos", "audios"} {
				if _, ok := p.Input[field]; ok {
					t.Fatalf("legacy field %s must not be sent", field)
				}
			}
		})
	}
	b, err := json.Marshal(laneTaskPayload("MiniMax-H3", testRequest().Input, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"materials", "video_references", "audio_references", "images", "videos", "audios"} {
		if strings.Contains(string(b), `"`+field+`"`) {
			t.Fatalf("empty reference field %s must be omitted: %s", field, b)
		}
	}
}

func TestSecureOutputURLUpgradesOnlyLaneAIHTTP(t *testing.T) {
	tests := []struct {
		input string
		want  string
		bad   bool
	}{
		{"http://laneai.dev/api/media/async/task?sig=x", "https://laneai.dev/api/media/async/task?sig=x", false},
		{"https://fileview.site/video.mp4", "https://fileview.site/video.mp4", false},
		{"http://example.com/video.mp4", "", true},
		{"https://user:pass@laneai.dev/video.mp4", "", true},
	}
	for _, tc := range tests {
		got, err := secureOutputURL(tc.input)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("secureOutputURL(%q) = %q, %v; want %q, bad=%v", tc.input, got, err, tc.want, tc.bad)
		}
	}
}
func TestProjectValidation(t *testing.T) {
	d := Document{Shots: []Shot{{ID: "one", Duration: 5}, {ID: "one", Duration: 5}}}
	if validateDocument(d) == nil {
		t.Fatal("duplicate shot IDs accepted")
	}
	d.Shots = d.Shots[:1]
	if e := validateDocument(d); e != nil {
		t.Fatal(e)
	}
}

func TestReferenceLimitsArePerModelAndMediaType(t *testing.T) {
	c := testChannel()
	r := testRequest()
	r.Model = "MiniMax-H3 2K"
	r.Input.Resolution = "2k"
	r.Input.Duration = 1
	r.Input.Images = make([]string, 9)
	r.Input.Videos = make([]string, 3)
	r.Input.Audios = make([]string, 3)
	if _, err := Quote(c, r); err != nil {
		t.Fatalf("published per-type maxima rejected: %v", err)
	}
	r.Input.Videos = make([]string, 4)
	if _, err := Quote(c, r); err == nil {
		t.Fatal("excess videos accepted")
	}
	r = testRequest()
	r.Input.Videos = make([]string, 2)
	if _, err := Quote(c, r); err == nil {
		t.Fatal("H3 video limit ignored")
	}
	r.Input.Videos = nil
	r.Input.Audios = make([]string, 2)
	if _, err := Quote(c, r); err == nil {
		t.Fatal("H3 audio limit ignored")
	}
}

func TestProjectPreservesPerShotMedia(t *testing.T) {
	d := Document{Shots: []Shot{{ID: "one", Duration: 5, Videos: []string{"vid"}, Audios: []string{"aud"}}}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var restored Document
	if err = json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Shots[0].Videos) != 1 || len(restored.Shots[0].Audios) != 1 {
		t.Fatal("shot media lost")
	}
	if err = validateDocument(restored); err != nil {
		t.Fatal(err)
	}
	restored.Shots[0].Videos = make([]string, 4)
	if validateDocument(restored) == nil {
		t.Fatal("excess shot videos accepted")
	}
}
