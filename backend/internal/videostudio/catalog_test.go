package videostudio

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestImportCatalogUsesVideoRatesAndKeyScope(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test" {
			t.Fatal("bad catalog request")
		}
		body := `{"data":[{"id":"H3-flat"}]}`
		if r.URL.Path == "/api/models" {
			body = `{"data":[{"model_name":"H3-flat","model_type":"video","endpoint":"/v1/task/submit","price_yuan":0.02,"video_rates":{"job":2.2},"video_resolution_allow":["2k"],"video_duration_min":1,"video_duration_max":15},{"model_name":"not-in-key","model_type":"video","video_rates":{"768p":0.22}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	models, e := discover(context.Background(), "https://laneai.dev/v1", "test", client)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 2 || len(models) != 1 || models[0].Mode != "job" || models[0].Rates["2k"] != 2.2 || models[0].MinSeconds != 1 {
		t.Fatalf("wrong catalog: %+v", models)
	}
}
func TestCatalogRejectsAmbiguousJobPricing(t *testing.T) {
	client := &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"bad"}]}`
		if r.URL.Path == "/api/models" {
			body = `{"data":[{"model_name":"bad","model_type":"video","endpoint":"/v1/task/submit","video_rates":{"job":2.2,"768p":0.22},"video_resolution_allow":["2k"],"video_duration_min":1,"video_duration_max":15}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	if _, e := discover(context.Background(), "https://laneai.dev", "test", client); e == nil {
		t.Fatal("ambiguous price accepted")
	}
}
