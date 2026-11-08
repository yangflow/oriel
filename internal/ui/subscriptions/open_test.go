package subscriptions

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Opens in select of the page applies at once and sends only where the entries open.
func TestPageOpenTarget(t *testing.T) {
	s, f, tt := paged(t, "en", 0, nil)
	if !tt.HasText("Opens in") || !tt.HasText("Follow the kind") || !tt.HasText(s.text.OpenTargetHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, "Follow the kind")
	mustClick(t, tt, "In the browser")
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].OpenTarget == nil || *p[0].OpenTarget != "browser" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// A subscription that opens in the reader shows it.
	_, _, tt = paged(t, "en", 1, func(src *client.Source) { src.OpenTarget = "reader" })
	if !tt.HasText("In the reader") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestPageOpenTargetChinese(t *testing.T) {
	s, _, tt := paged(t, "zh-Hans", 0, nil)
	if !tt.HasText("点开时") || !tt.HasText("跟随形态") || !tt.HasText(s.text.OpenTargetHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
}
