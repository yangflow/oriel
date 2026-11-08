package subscriptions

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Pace select of the page applies at once and sends only the pace.
func TestPagePace(t *testing.T) {
	s, f, tt := paged(t, "en", 0, func(src *client.Source) { src.DailyCap = 20 }) // the cap shows a number, so only the pace says "Off"
	if s.pg.e.pace != 0 || !tt.HasText("Pace") || !tt.HasText("Off") || !tt.HasText(s.text.PaceHelp) {
		t.Fatalf("pace %d, texts %v", s.pg.e.pace, tt.Texts())
	}
	mustClick(t, tt, "Off")
	mustClick(t, tt, "Daily · 2 d")
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].PaceSec == nil || *p[0].PaceSec != 2*24*3600 || p[0].Title != nil || p[0].KeepDays != nil {
		t.Fatalf("patches %+v", p)
	}
}

// A pace the menu does not list shows as hours or days and can go back to off.
func TestPagePaceUnlisted(t *testing.T) {
	_, f, tt := paged(t, "en", 1, func(src *client.Source) { src.PaceSec, src.DailyCap = 5*3600, 20 })
	if !tt.HasText("5 h") {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, "5 h")
	mustClick(t, tt, "Off")
	f.settle(tt)
	p := f.patches[2]
	if len(p) != 1 || p[0].PaceSec == nil || *p[0].PaceSec != 0 {
		t.Fatalf("patches %+v", p)
	}
}

func TestPagePaceInChinese(t *testing.T) {
	_, _, tt := paged(t, "zh-Hans", 0, func(src *client.Source) { src.PaceSec = 7 * 24 * 3600 })
	if !tt.HasText("周更 · 7 天") || !tt.HasText("节奏") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
