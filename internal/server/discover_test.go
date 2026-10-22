package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/yangflow/oriel/internal/fetch"
)

type finder func(address string) ([]fetch.Candidate, error)

func (f finder) Discover(_ context.Context, address string) ([]fetch.Candidate, error) {
	return f(address)
}

func TestDiscover(t *testing.T) {
	s, _ := newServer(t, "")
	s.discover = finder(func(address string) ([]fetch.Candidate, error) {
		switch address {
		case "https://blog.example/":
			return []fetch.Candidate{{URL: "https://blog.example/rss.xml", Title: "Blog", Origin: "alternate"}}, nil
		case "https://space.bilibili.com/1":
			return []fetch.Candidate{{RSSHubPath: "/bilibili/user/video/1", Kind: "video", Origin: "rule", Site: "bilibili",
				Label: "videos", Subject: "1"}}, nil
		case "https://empty.example/":
			return nil, nil
		case "https://private.example/":
			return nil, fmt.Errorf("dial: %w", fetch.ErrBlockedAddress)
		}
		return nil, fetch.ErrPageUnavailable
	})
	get := func(address string) *httptest.ResponseRecorder {
		return do(s, "GET", "/api/discover?url="+url.QueryEscape(address), "", nil)
	}

	w := get("https://blog.example/")
	list, _ := decode(t, w)["candidates"].([]any)
	if w.Code != http.StatusOK || len(list) != 1 {
		t.Fatalf("discover = %d %s", w.Code, w.Body.String())
	}
	first := list[0].(map[string]any)
	if first["url"] != "https://blog.example/rss.xml" || first["title"] != "Blog" || first["origin"] != "alternate" ||
		first["rsshubPath"] != "" {
		t.Errorf("candidate = %v", first)
	}
	first = decode(t, get("https://space.bilibili.com/1"))["candidates"].([]any)[0].(map[string]any)
	if first["rsshubPath"] != "/bilibili/user/video/1" || first["site"] != "bilibili" || first["label"] != "videos" ||
		first["subject"] != "1" || first["kind"] != "video" {
		t.Errorf("rule candidate = %v", first)
	}
	w = get("https://empty.example/")
	if empty, ok := decode(t, w)["candidates"].([]any); w.Code != http.StatusOK || !ok || len(empty) != 0 {
		t.Errorf("empty = %d %q, want an empty list", w.Code, w.Body.String())
	}
	for address, want := range map[string]struct {
		status int
		code   string
	}{
		"https://private.example/": {http.StatusUnprocessableEntity, "address_not_public"},
		"https://down.example/":    {http.StatusBadGateway, "page_unavailable"},
		"":                         {http.StatusBadRequest, "invalid_url"},
		"ftp://x.example/feed":     {http.StatusBadRequest, "invalid_url"},
		"/bilibili/user/video/1":   {http.StatusBadRequest, "invalid_url"},
	} {
		if w := get(address); w.Code != want.status || decode(t, w)["error"] != want.code {
			t.Errorf("%q = %d %s, want %d %s", address, w.Code, w.Body.String(), want.status, want.code)
		}
	}
	s.discover = nil
	if w := get("https://blog.example/"); w.Code != http.StatusBadGateway {
		t.Errorf("without a discoverer = %d", w.Code)
	}
}

func TestDiscoverNeedsASession(t *testing.T) {
	s, _ := newServer(t, "secret")
	if w := do(s, "GET", "/api/discover?url="+url.QueryEscape("https://blog.example/"), "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("without a session = %d", w.Code)
	}
}
