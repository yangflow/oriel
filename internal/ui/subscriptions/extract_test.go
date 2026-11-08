package subscriptions

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// focusField tabs to the field with the label.
func focusField(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	for range 80 {
		if tt.Focused(label) {
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatalf("%s never got the focus", label)
}

// The Extraction row of the page opens its boxes, which wait for Save extraction and send only themselves.
func TestPageExtraction(t *testing.T) {
	s, f, tt := paged(t, "en", 0, nil)
	if !tt.HasText(s.text.Extract) || !tt.HasText(s.text.ExtractRowHelp) || tt.HasText("Content selector") {
		t.Fatalf("closed row: %v", tt.Texts())
	}
	mustClick(t, tt, s.text.Extract)
	tt.Frame()
	for _, text := range []string{"Content selector", "Referer", "Link rewrites", s.text.ExtractSelectorHelp, s.text.ExtractRefererHelp, s.text.ExtractRewritesHelp} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	focusField(t, tt, "Content selector")
	tt.Type(" article .body ")
	focusField(t, tt, "Referer")
	tt.Type("https://go.dev/")
	focusField(t, tt, "Link rewrites")
	tt.Type("www.go.dev => go.dev")
	mustClick(t, tt, s.text.SaveExtract)
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].ExtractSelector == nil || *p[0].ExtractSelector != "article .body" ||
		p[0].ExtractReferer == nil || *p[0].ExtractReferer != "https://go.dev/" ||
		p[0].ExtractRewrites == nil || *p[0].ExtractRewrites != "www.go.dev => go.dev" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}
	if !tt.HasText(s.text.Custom) {
		t.Fatalf("the row does not say Custom once saved: %v", tt.Texts())
	}
}

func TestPageExtractionRefused(t *testing.T) {
	s, f, tt := paged(t, "en", 0, func(src *client.Source) { src.ExtractSelector = "div[" })
	mustClick(t, tt, s.text.Extract)
	tt.Frame()
	focusField(t, tt, "Content selector")
	tt.Type("x")
	f.updateErr = &client.APIError{Status: 400, Code: "invalid_extract_selector"}
	mustClick(t, tt, s.text.SaveExtract)
	f.settle(tt)
	if !tt.HasText(s.text.ExtractSelectorInvalid) || !s.pg.open || !s.Unsaved() {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestPageExtractionInChinese(t *testing.T) {
	s, _, tt := paged(t, "zh-Hans", 0, func(src *client.Source) { src.ExtractRewrites = "a.org => b.org" })
	mustClick(t, tt, s.text.Extract)
	tt.Frame()
	for _, text := range []string{"正文抽取", "正文选择器", "Referer", "链接改写", s.text.ExtractRewritesHelp, s.text.Custom} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
}
