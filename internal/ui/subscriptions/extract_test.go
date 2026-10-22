package subscriptions

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// focusField tabs from the title to the field with the label.
func focusField(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	for range 20 {
		if tt.Focused(label) {
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatalf("%s never got the focus", label)
}

func TestEditExtraction(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 2400)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("Extraction") || !tt.HasText(s.text.ExtractHelp) || tt.HasText("Content selector") {
		t.Fatalf("closed group: %v", tt.Texts())
	}
	if err := tt.Click("Extraction"); err != nil {
		t.Fatal(err)
	}
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
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].ExtractSelector == nil || *p[0].ExtractSelector != "article .body" ||
		p[0].ExtractReferer == nil || *p[0].ExtractReferer != "https://go.dev/" ||
		p[0].ExtractRewrites == nil || *p[0].ExtractRewrites != "www.go.dev => go.dev" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// A subscription with rules opens the group, and untouched rules are not sent.
	src := f.sources[1]
	src.ExtractSelector, src.ExtractReferer, src.ExtractRewrites = ".post", "https://example.org/", "a.org => b.org"
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("Content selector") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if len(f.patches[src.ID]) != 0 {
		t.Fatalf("saving without a change sent %v", f.patches[src.ID])
	}
}

func TestEditExtractionRefused(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 2400)
	src := f.sources[0]
	src.ExtractSelector = "div["
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	focusField(t, tt, "Content selector")
	tt.Type("x")
	f.updateErr = &client.APIError{Status: 400, Code: "invalid_extract_selector"}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText(s.text.ExtractSelectorInvalid) || !s.edit.open {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestEditExtractionInChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	tt.SetSize(1000, 2400)
	src := f.sources[0]
	src.ExtractRewrites = "a.org => b.org"
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	for _, text := range []string{"正文抽取", "正文选择器", "Referer", "链接改写", s.text.ExtractRewritesHelp} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
}
