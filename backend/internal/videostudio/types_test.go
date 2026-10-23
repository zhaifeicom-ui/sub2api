package videostudio

import (
	"context"
	"encoding/json"
	"fmt"
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
func TestPricingAndCapabilities(t *testing.T) {
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

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMovoOrigin(t *testing.T) {
	for _, raw := range []string{"http://movoapi.top", "https://movoapi.top.evil.test", "https://movoapi.top@evil.test", "https://movoapi.top:443", "https://laneai.dev", "https://movoapi.top?key=x"} {
		if _, err := Origin(raw, MovoProtocol); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if base, err := Origin("https://movoapi.top/v1", MovoProtocol); err != nil || base != "https://movoapi.top" {
		t.Fatal(base, err)
	}
}
func TestMovoPayloads(t *testing.T) {
	for _, model := range []string{"MiniMax-H3", "seedance2.5-yx-stable"} {
		t.Run(model, func(t *testing.T) {
			p := movoPayload(model, testRequest().Input, []string{"https://station/image"}, []string{"https://station/video"}, []string{"https://station/audio"})
			if _, exists := p["input"]; exists {
				t.Fatal("obsolete envelope")
			}
			if model == "MiniMax-H3" {
				if p["seconds"] != "4" || p["size"] != "1344x768" {
					t.Fatal(p)
				}
				content := payloadValue[[]map[string]any](t, payloadValue[map[string]any](t, p["metadata"])["content"])
				for i, kind := range []string{"image", "video", "audio"} {
					if content[i]["role"] != "reference_"+kind || payloadValue[map[string]string](t, content[i][kind+"_url"])["url"] != "https://station/"+kind {
						t.Fatal(content)
					}
				}
			} else {
				if p["seconds"] != 4 || len(payloadValue[[]string](t, p["reference_images"])) != 1 {
					t.Fatal(p)
				}
				if model == "seedance2.5-yx-stable" && p["facs"] != true {
					t.Fatal("missing FACS default")
				}
			}
		})
	}
	off := false
	in := testRequest().Input
	in.FACS = &off
	if movoPayload("seedance2.5-yx-stable", in, nil, nil, nil)["facs"] != false {
		t.Fatal("FACS off ignored")
	}
}
func TestMovoTaskContract(t *testing.T) {
	w := NewWorker(nil)
	w.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/videos" || r.Header.Get("Authorization") != "Bearer test" {
			t.Fatal(r.URL)
		}
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p["seconds"] != "4" {
			t.Fatal(p)
		}
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{"id":"job1","status":"queued"}`)), Header: make(http.Header)}, nil
	})
	out, status, err := w.call(context.Background(), "https://movoapi.top", "test", "POST", "/v1/videos", movoPayload("MiniMax-H3", testRequest().Input, nil, nil, nil))
	if err != nil || status != 202 || out.ID != "job1" {
		t.Fatal(out, status, err)
	}
}
func TestSecureOutputURL(t *testing.T) {
	for _, raw := range []string{"http://laneai.dev/video", "http://example.com/video", "https://user:pass@example.com/video"} {
		if _, err := secureOutputURL(raw); err == nil {
			t.Fatal(raw)
		}
	}
	raw := "https://cdn.example.com/video?sig=a%2Bb&exp=1"
	if got, err := secureOutputURL(raw); err != nil || got != raw {
		t.Fatal(got, err)
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
	r.Input.Videos = make([]string, 11)
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
	restored.Shots[0].Videos = make([]string, 11)
	if validateDocument(restored) == nil {
		t.Fatal("excess shot videos accepted")
	}
}

func TestMovoOutputAndErrors(t *testing.T) {
	w := NewWorker(nil)
	var out upstreamResponse
	if err := json.Unmarshal([]byte(`{"status":"completed","metadata":{"url":"https://cdn.example/video.mp4?sig=a%2Bb"}}`), &out); err != nil {
		t.Fatal(err)
	}
	links, err := w.movoOutput(context.Background(), "https://movoapi.top", "secret", "job", out, nil)
	if err != nil || len(links) != 1 || links[0] != "https://cdn.example/video.mp4?sig=a%2Bb" {
		t.Fatal(links, err)
	}
	for _, body := range []string{`{"message":"bad params"}`, `{"error":{"message":"bad params"}}`} {
		var reply upstreamResponse
		if err := json.Unmarshal([]byte(body), &reply); err != nil || reply.Error.Message != "bad params" {
			t.Fatal(reply, err)
		}
	}
}
func TestMovoContentRedirectNeverForwardsKey(t *testing.T) {
	for _, location := range []string{"https://cdn.example/video.mp4?sig=a%2Bb", "https://movoapi.top/v1/videos/job/content", "http://cdn.example/video"} {
		t.Run(location, func(t *testing.T) {
			w := NewWorker(nil)
			calls := 0
			w.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "movoapi.top" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Fatal("credential destination")
				}
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{location}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})
			urls, err := w.movoOutput(context.Background(), "https://movoapi.top", "secret", "job", upstreamResponse{}, nil)
			if calls != 1 {
				t.Fatal("followed redirect")
			}
			if strings.HasPrefix(location, "https://cdn.example/") {
				if err != nil || len(urls) != 1 || urls[0] != location {
					t.Fatal(urls, err)
				}
			} else if err == nil {
				t.Fatal("unsafe redirect accepted")
			}
		})
	}
}
func TestMovoCapabilitiesRequireConfirmedCost(t *testing.T) {
	models := movoModels(map[string]bool{"MiniMax-H3": true, "seedance2.5-yx-stable": true})
	for _, m := range models {
		for res := range m.Rates {
			m.Rates[res] = 1
			m.CostCNY[res] = .5
		}
		if err := ValidateMovoModel(m); err != nil {
			t.Fatal(err)
		}
		if m.ID == "MiniMax-H3" {
			in := testRequest()
			in.Input.Images = make([]string, 9)
			in.Input.Videos = make([]string, 3)
			in.Input.Audios = []string{"extra"}
			if _, err := Quote(Channel{Models: []Model{m}}, in); err == nil {
				t.Fatal("13 references accepted")
			}
		}
	}
}

func TestMovoReferenceBoundaries(t *testing.T) {
	for _, m := range movoModels(map[string]bool{"MiniMax-H3": true, "seedance2.5-yx-stable": true, "seedance-2.0": true}) {
		t.Run(m.ID, func(t *testing.T) {
			m.Disabled = false
			m.SpecConfirmed = true
			res := ""
			for r := range m.Rates {
				m.Rates[r] = 1
				m.CostCNY[r] = .5
				res = r
			}
			if err := ValidateMovoModel(m); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"image", "video", "audio"} {
				max := m.MaxImages
				if kind == "video" {
					max = m.MaxVideos
				}
				if kind == "audio" {
					max = m.MaxAudios
				}
				for _, count := range []int{max, max + 1} {
					r := testRequest()
					r.Model = m.ID
					r.Input.Duration = m.MinSeconds
					r.Input.Resolution = res
					refs := make([]string, count)
					for i := range refs {
						refs[i] = "asset_test"
					}
					switch kind {
					case "image":
						r.Input.Images = refs
					case "video":
						r.Input.Videos = refs
					case "audio":
						r.Input.Audios = refs
					}
					_, err := Quote(Channel{Models: []Model{m}}, r)
					if (err != nil) != (count > max) {
						t.Fatalf("%s count=%d max=%d err=%v", kind, count, max, err)
					}
				}
			}
			if m.ID == "seedance-2.0" {
				m.MaxImages = 10
				if ValidateMovoModel(m) == nil {
					t.Fatal("SD2 allowed 10 images")
				}
			}
		})
	}
}

func TestMovoPayloadReferenceCounts(t *testing.T) {
	refs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("https://station.example/asset-%d", i)
		}
		return out
	}
	for _, model := range []string{"MiniMax-H3", "seedance2.5-yx-stable"} {
		t.Run(model, func(t *testing.T) {
			ni, nv, na := 9, 0, 0
			if model == "MiniMax-H3" {
				nv = 2
				na = 1
			}
			if model == "seedance2.5-yx-stable" {
				ni = 30
				nv = 10
				na = 10
			}
			p := movoPayload(model, testRequest().Input, refs(ni), refs(nv), refs(na))
			if model == "MiniMax-H3" {
				items := payloadValue[[]map[string]any](t, payloadValue[map[string]any](t, p["metadata"])["content"])
				counts := map[string]int{}
				for _, item := range items {
					counts[payloadValue[string](t, item["type"])]++
				}
				if counts["image_url"] != ni || counts["video_url"] != nv || counts["audio_url"] != na {
					t.Fatal(counts)
				}
			} else {
				for _, field := range []struct {
					name  string
					count int
				}{{"reference_images", ni}, {"videos", nv}, {"audios", na}} {
					if field.count == 0 {
						if _, exists := p[field.name]; exists {
							t.Fatal("empty field", field.name)
						}
						continue
					}
					urls := payloadValue[[]string](t, p[field.name])
					if len(urls) != field.count || urls[len(urls)-1] != fmt.Sprintf("https://station.example/asset-%d", field.count-1) {
						t.Fatal(field.name, urls)
					}
				}
			}
		})
	}
}

func payloadValue[T any](t *testing.T, v any) T {
	t.Helper()
	value, ok := v.(T)
	if !ok {
		t.Fatalf("unexpected payload type %T", v)
	}
	return value
}
