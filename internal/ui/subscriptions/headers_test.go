package subscriptions

import (
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Request headers row of the page opens its boxes, which wait for Save headers; a refusal marks the field.
func TestPageRequestHeaders(t *testing.T) {
	s, f, tt := paged(t, "en", 0, nil)
	uaHelp := strings.ReplaceAll(s.text.HeaderUaHelp, "{version}", client.Version())
	if !tt.HasText(s.text.Headers) || tt.HasText("User-Agent") {
		t.Fatalf("closed row: %v", tt.Texts())
	}
	mustClick(t, tt, s.text.Headers)
	tt.Frame()
	for _, text := range []string{"User-Agent", "Referer", uaHelp, s.text.HeaderRefererHelp, s.text.HeadersNote} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	focusField(t, tt, "User-Agent")
	tt.Type(" MyReader/1.0 ")
	focusField(t, tt, "Referer")
	tt.Type("https://go.dev/")
	mustClick(t, tt, s.text.SaveHeaders)
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].FetchUserAgent == nil || *p[0].FetchUserAgent != "MyReader/1.0" ||
		p[0].FetchReferer == nil || *p[0].FetchReferer != "https://go.dev/" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// The server's refusal marks the field and keeps the text.
	focusField(t, tt, "User-Agent")
	tt.Type("x")
	f.updateErr = &client.APIError{Status: 400, Code: "invalid_fetch_user_agent"}
	mustClick(t, tt, s.text.SaveHeaders)
	f.settle(tt)
	if !tt.HasText(s.text.HeaderUaError) || !s.pg.e.headerErr[0] || !s.Unsaved() {
		t.Fatalf("no refusal: %v", tt.Texts())
	}
}

func TestPageRequestHeadersHiddenForARoute(t *testing.T) {
	s, _, tt := paged(t, "zh-Hans", 1, nil)
	if tt.HasText(s.text.Headers) {
		t.Fatalf("route shows the row: %v", tt.Texts())
	}
	s, _, tt = paged(t, "zh-Hans", 0, nil)
	if !tt.HasText(s.text.Headers) {
		t.Fatalf("no row in Chinese: %v", tt.Texts())
	}
}
