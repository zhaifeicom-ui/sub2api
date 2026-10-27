package videostudio

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMovoCatalog(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test" {
			t.Fatal(r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"MiniMax-H3"},{"id":"seedance2.5-yx-stable"},{"id":"seedance-2.0"},{"id":"unknown"}]}`)), Header: make(http.Header)}, nil
	})}
	models, err := discover(context.Background(), "https://movoapi.top", "test", client)
	if err != nil || calls != 1 || len(models) != 2 {
		t.Fatal(models, calls, err)
	}
	for _, m := range models {
		if m.ID == "seedance-2.0" {
			t.Fatal("excluded model imported")
		}
		if ValidateMovoModel(m) == nil {
			t.Fatal("unpriced model allowed")
		}
		for _, price := range m.Rates {
			if price != 0 {
				t.Fatal("invented account price")
			}
		}
	}
}
