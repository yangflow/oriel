package subscriptions

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Translate titles switch of the page applies at once and sends only itself.
func TestPageTranslateTitles(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, _ := setup(t, language)
			f.settings.AIBaseURL, f.settings.AIModel = "https://api.example.com/v1", "small"
			s.OpenSource(f.sources[0])
			tt := pageTester(s)
			f.settle(tt)
			for _, text := range []string{s.text.TranslateTitles, s.text.TranslateTitlesHelp} {
				if !tt.HasText(text) {
					t.Errorf("no %q in %v", text, tt.Texts())
				}
			}
			if s.pg.e.titles {
				t.Fatal("the switch starts on")
			}
			s.pg.e.titles = true // as the switch does
			tt.Frame()
			f.settle(tt)
			p := f.patches[1]
			if len(p) != 1 || p[0].TranslateTitles == nil || !*p[0].TranslateTitles || p[0].AutoSummary != nil || p[0].Title != nil {
				t.Fatalf("patches %+v", p)
			}
		})
	}
}

// A title saved on the page leaves the saved Translate titles alone.
func TestPageLeavesTranslateTitlesAloneWhenUntouched(t *testing.T) {
	s, f, tt := paged(t, "en", 0, func(src *client.Source) { src.TranslateTitles = true })
	if !s.pg.e.titles {
		t.Fatal("the switch does not show the saved value")
	}
	s.pg.e.title = "Renamed"
	tt.Frame()
	mustClick(t, tt, s.text.SaveTitle)
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].TranslateTitles != nil {
		t.Fatalf("patches %+v", p)
	}
}
