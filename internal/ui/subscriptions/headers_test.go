package subscriptions

import (
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

func TestEditRequestHeaders(t *testing.T) {
	s, f, tt := setup(t, "en")
	uaHelp := strings.ReplaceAll(s.text.HeaderUaHelp, "{version}", client.Version())
	tt.SetSize(1000, 2400)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("Request headers") || !tt.HasText(s.text.HeadersHelp) || tt.HasText("User-Agent") {
		t.Fatalf("closed group: %v", tt.Texts())
	}
	if err := tt.Click("Request headers"); err != nil {
		t.Fatal(err)
	}
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
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].FetchUserAgent == nil || *p[0].FetchUserAgent != "MyReader/1.0" ||
		p[0].FetchReferer == nil || *p[0].FetchReferer != "https://go.dev/" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// A subscription with headers opens the group, and untouched values are not sent.
	src := f.sources[1]
	src.URL, src.RSSHubPath = "https://example.org/feed", ""
	src.FetchUserAgent = "MyReader/1.0"
	s.Edit(src)
	tt.Frame()
	if !tt.HasText(uaHelp) {
		t.Fatalf("filled group closed: %v", tt.Texts())
	}

	// The server's refusal opens the group and marks the field.
	s.Edit(f.sources[0])
	tt.Frame()
	f.updateErr = &client.APIError{Status: 400, Code: "invalid_fetch_user_agent"}
	focusField(t, tt, "Title")
	tt.Type("x")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText(s.text.HeaderUaError) {
		t.Fatalf("no refusal: %v", tt.Texts())
	}
}

func TestEditRequestHeadersHiddenForARoute(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	tt.SetSize(1000, 2400)
	s.Edit(f.sources[1])
	tt.Frame()
	f.settle(tt)
	if tt.HasText("请求头") {
		t.Fatalf("route shows the group: %v", tt.Texts())
	}
	s.Edit(f.sources[0])
	tt.Frame()
	if !tt.HasText("请求头") {
		t.Fatalf("no group in Chinese: %v", tt.Texts())
	}
}
