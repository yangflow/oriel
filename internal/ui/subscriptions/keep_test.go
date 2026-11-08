package subscriptions

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Keep select of the page applies at once and sends only the keep period.
func TestPageKeep(t *testing.T) {
	s, f, tt := paged(t, "en", 0, nil)
	if s.pg.e.keep != 0 || !tt.HasText("Follow settings · 1 month") || !tt.HasText(s.text.KeepHelp) {
		t.Fatalf("keep %d, texts %v", s.pg.e.keep, tt.Texts())
	}
	mustClick(t, tt, "Follow settings · 1 month")
	mustClick(t, tt, "3 months")
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].KeepDays == nil || *p[0].KeepDays != 90 || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}
}

// A number of days the menu does not list shows as it is and can go back to following the settings.
func TestPageKeepUnlisted(t *testing.T) {
	s, f, tt := paged(t, "en", 1, func(src *client.Source) { src.KeepDays = 45 })
	if !tt.HasText("45 d") {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, "45 d")
	mustClick(t, tt, "Follow settings · 1 month")
	f.settle(tt)
	p := f.patches[2]
	if len(p) != 1 || p[0].KeepDays == nil || *p[0].KeepDays != 0 || s.pg.e.keep != 0 {
		t.Fatalf("patches %+v", p)
	}
}

func TestPageKeepFollowsForeverSettings(t *testing.T) {
	s, f, _ := setup(t, "zh-Hans")
	f.settings.RetentionDays = 0
	s.OpenSource(f.sources[0])
	tt := pageTester(s)
	f.settle(tt)
	if !tt.HasText("跟随设置 · 永久") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
