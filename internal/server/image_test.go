package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

type images map[string]string

func (im images) Get(_ context.Context, address string) (string, []byte, error) {
	body, ok := im[address]
	if !ok {
		return "", nil, errors.New("unavailable")
	}
	return "image/png", []byte(body), nil
}

func TestImage(t *testing.T) {
	s, _ := newServer(t, "")
	s.images = images{"https://img.example/a.png": "PNG"}

	w := do(s, "GET", "/api/image?url="+url.QueryEscape("https://img.example/a.png"), "", nil)
	if w.Code != http.StatusOK || w.Body.String() != "PNG" {
		t.Fatalf("image = %d %q", w.Code, w.Body.String())
	}
	for header, want := range map[string]string{
		"Content-Type": "image/png", "X-Content-Type-Options": "nosniff", "Cache-Control": "private, max-age=86400",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if w := do(s, "GET", "/api/image?url="+url.QueryEscape("https://img.example/gone.png"), "", nil); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "image_unavailable" {
		t.Errorf("missing image = %d %s", w.Code, w.Body.String())
	}
	for _, address := range []string{"", "file:///etc/passwd", "/relative.png", "https://"} {
		if w := do(s, "GET", "/api/image?url="+url.QueryEscape(address), "", nil); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_url" {
			t.Errorf("%q = %d %s", address, w.Code, w.Body.String())
		}
	}
	s.images = nil
	if w := do(s, "GET", "/api/image?url="+url.QueryEscape("https://img.example/a.png"), "", nil); w.Code != http.StatusBadGateway {
		t.Errorf("without a fetcher = %d", w.Code)
	}
}

type counted struct{ calls int }

func (c *counted) Get(context.Context, string) (string, []byte, error) {
	c.calls++
	return "image/png", []byte("PNG"), nil
}

func TestImageRefusesTrackers(t *testing.T) {
	s, _ := newServer(t, "")
	fetcher := &counted{}
	s.images = fetcher
	for _, address := range []string{
		"https://pixel.wp.com/g.gif?v=ext",
		"https://x.us1.list-manage.com/track/open.php?u=1",
		"https://blog.example.com/__utm.gif",
	} {
		if w := do(s, "GET", "/api/image?url="+url.QueryEscape(address), "", nil); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "image_unavailable" {
			t.Errorf("%s = %d %s", address, w.Code, w.Body.String())
		}
	}
	if fetcher.calls != 0 {
		t.Errorf("trackers were contacted %d times", fetcher.calls)
	}
	if w := do(s, "GET", "/api/image?url="+url.QueryEscape("https://i0.wp.com/example.com/a.jpg"), "", nil); w.Code != http.StatusOK || fetcher.calls != 1 {
		t.Errorf("ordinary picture = %d after %d calls", w.Code, fetcher.calls)
	}
}

func TestImageNeedsSession(t *testing.T) {
	s, _ := newServer(t, "secret")
	s.images = images{"https://img.example/a.png": "PNG"}
	target := "/api/image?url=" + url.QueryEscape("https://img.example/a.png")
	if w := do(s, "GET", target, "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous image = %d", w.Code)
	}
	token := login(t, s, "test")
	if w := do(s, "GET", target, "", map[string]string{"Authorization": "Bearer " + token}); w.Code != http.StatusOK {
		t.Fatalf("signed-in image = %d", w.Code)
	}
}
