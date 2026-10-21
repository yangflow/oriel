package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestImagesSniffAndRefuse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "Oriel/") {
			http.Error(w, "headers", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/pic":
			w.Header().Set("Content-Type", "text/plain") // the declared type is ignored
			w.Write(pngHeader)
		case "/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/big":
			w.Write(pngHeader)
			w.Write(make([]byte, maxBytes))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	im := NewImages("test", "")
	im.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	ctx := context.Background()

	kind, body, err := im.Get(ctx, srv.URL+"/pic")
	if err != nil || kind != "image/png" || len(body) != len(pngHeader) {
		t.Fatalf("pic = %q, %d bytes, %v", kind, len(body), err)
	}
	if _, _, err := im.Get(ctx, srv.URL+"/svg"); !errors.Is(err, ErrNotImage) {
		t.Errorf("svg: %v", err)
	}
	if _, _, err := im.Get(ctx, srv.URL+"/big"); !errors.Is(err, errTooLarge) {
		t.Errorf("big: %v", err)
	}
	if _, _, err := im.Get(ctx, srv.URL+"/gone"); err == nil {
		t.Error("404 was accepted")
	}
	for _, address := range []string{"file:///etc/passwd", "data:image/png;base64,AA", "http://", "::"} {
		if _, _, err := im.Get(ctx, address); err == nil {
			t.Errorf("%q was accepted", address)
		}
	}
}

func TestImagesStayOutside(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(pngHeader) }))
	defer srv.Close()
	if _, _, err := NewImages("test", "").Get(context.Background(), srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback image: %v", err)
	}
}
