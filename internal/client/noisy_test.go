package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoisySuggestionAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/sources/4/noisy/dismiss":
			w.WriteHeader(http.StatusNoContent)
		case "/api/sources/health":
			w.Write([]byte(`{"windowDays":30,"sources":[{"id":4,"title":"Busy","state":"noisy","days":30,"recent":700,"recentRead":10,"perDay":23,"paceSec":0,"dailyCap":0}]}`))
		default:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"not_noisy"}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "t1"}

	if err := session.DismissNoisy(ctx, 4); err != nil {
		t.Errorf("DismissNoisy: %v", err)
	}
	var apiErr *APIError
	if err := session.DismissNoisy(ctx, 5); !errors.As(err, &apiErr) || apiErr.Code != "not_noisy" {
		t.Errorf("DismissNoisy not noisy: %v", err)
	}
	health, err := session.SourcesHealth(ctx)
	if err != nil || len(health.Sources) != 1 || health.Sources[0].State != HealthNoisy || health.Sources[0].PerDay != 23 {
		t.Errorf("SourcesHealth = %+v, %v", health, err)
	}
	want := "POST /api/sources/4/noisy/dismiss Bearer t1\nPOST /api/sources/5/noisy/dismiss Bearer t1\nGET /api/sources/health Bearer t1"
	if strings.Join(seen, "\n") != want {
		t.Errorf("requests %q", seen)
	}
}
