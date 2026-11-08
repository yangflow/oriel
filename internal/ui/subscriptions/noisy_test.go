package subscriptions

import (
	"slices"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The At most per day select of the page applies at once and sends only the cap.
func TestPageDailyCap(t *testing.T) {
	s, _, tt := paged(t, "en", 0, nil)
	if s.pg.e.dailyCap != 0 || !tt.HasText("At most per day") || !tt.HasText(s.text.CapHelp) {
		t.Fatalf("cap %d, texts %v", s.pg.e.dailyCap, tt.Texts())
	}

	// A cap the menu does not list shows as a number and can be set to a preset.
	_, f, tt := paged(t, "en", 1, func(src *client.Source) { src.DailyCap = 30 })
	if !tt.HasText("30 entries") {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, "30 entries")
	mustClick(t, tt, "10 entries")
	f.settle(tt)
	p := f.patches[2]
	if len(p) != 1 || p[0].DailyCap == nil || *p[0].DailyCap != 10 || p[0].PaceSec != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestPageDailyCapChinese(t *testing.T) {
	s, _, tt := paged(t, "zh-Hans", 0, func(src *client.Source) { src.DailyCap = 20 })
	for _, want := range []string{"每天最多", "20 条", s.text.CapHelp} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
	}
}

func noisyHealth() client.Health {
	return client.Health{WindowDays: 30, Sources: []client.SourceHealth{
		{ID: 1, Title: "Go Blog", Kind: "article", State: client.HealthNoisy, Recent: 700, RecentRead: 14, PerDay: 23},
		{ID: 3, Title: "Haiku", Kind: "article", State: client.HealthNoisy, Recent: 900, RecentRead: 0, PerDay: 30, PaceSec: 3 * 3600},
	}}
}

func TestHealthDialogNoisy(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.health = noisyHealth()
	s.Health()
	tt.Frame()
	f.settle(tt)
	if !slices.Contains(tt.Texts(), "23 a day, you read 2%") {
		t.Fatalf("no noisy reason in %v", tt.Texts())
	}
	// A subscription already at the Breaking pace is not offered to slow down.
	if tt.HasText("Slow down Haiku") {
		t.Fatalf("Haiku offered to slow down in %v", tt.Texts())
	}
	if err := tt.Click("Slow down Go Blog"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].PaceSec == nil || *p[0].PaceSec != 3*3600 || len(s.report.items) != 1 {
		t.Fatalf("patches %+v, items %v", p, s.report.items)
	}
	if err := tt.Click("Dismiss the suggestion for Haiku"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.noisyDismissed, []int64{3}) || len(s.report.items) != 0 {
		t.Fatalf("dismissed %v, items %v", f.noisyDismissed, s.report.items)
	}
}

func TestHealthDialogNoisyGone(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.health = noisyHealth()
	f.noisyErr = &client.APIError{Status: 409, Code: "not_noisy"}
	s.Health()
	tt.Frame()
	f.settle(tt)
	// The server no longer names it: the row goes without an error.
	if err := tt.Click("Dismiss the suggestion for Haiku"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(s.report.items) != 1 || s.toast == s.text.ActionFailed {
		t.Fatalf("items %v, toast %q", s.report.items, s.toast)
	}
}

func TestHealthDialogNoisyChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	f.health = noisyHealth()
	s.Health()
	tt.Frame()
	f.settle(tt)
	for _, want := range []string{"每天 23 条，你读了 2%", "放慢", "暂停", "忽略"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
	}
}
