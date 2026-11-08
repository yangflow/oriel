package settings

import (
	"errors"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Fetching group shows the two defaults; each saves only itself, and a refusal puts it back.
func TestFetchDefaults(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := openProxy(t, language, client.Settings{})
			for _, want := range []string{s.text.FetchEvery, s.text.FetchEveryHelp, s.text.FullText, s.text.FullTextHelp, s.text.FetchAuto} {
				if !tt.HasText(want) {
					t.Fatalf("missing %q in %v", want, tt.Texts())
				}
			}
			f.set = nil
			s.every = 1800 // as the picker does
			s.saveFetchInterval()
			f.settle(tt)
			if len(f.set) != 1 || f.set[0].FetchInterval == nil || *f.set[0].FetchInterval != 1800 || f.set[0].FullText != nil {
				t.Fatalf("saved %+v", f.set)
			}
			if !tt.HasText(s.text.FetchDefaultSaved) || !tt.HasText(s.everyName(1800)) {
				t.Fatalf("after saving: %v", tt.Texts())
			}
			f.setErr = errors.New("down")
			s.every = 3600
			s.saveFetchInterval()
			f.settle(tt)
			if s.every != 1800 || !tt.HasText(s.text.SaveFailed) {
				t.Fatalf("a refused interval stayed: %d, %v", s.every, tt.Texts())
			}
			s.saveFullText(false)
			f.settle(tt)
			if !s.settings.FullTextDefault {
				t.Fatal("a refused switch stayed off")
			}
			f.setErr, f.set = nil, nil
			s.saveFullText(false)
			f.settle(tt)
			if len(f.set) != 1 || f.set[0].FullText == nil || *f.set[0].FullText || f.set[0].FetchInterval != nil || s.settings.FullTextDefault {
				t.Fatalf("saved %+v, settings %+v", f.set, s.settings)
			}
		})
	}
}

func TestEveryNames(t *testing.T) {
	s, _, _ := openProxy(t, "en", client.Settings{})
	for sec, want := range map[int64]string{0: "Auto", 900: "15 minutes", 3600: "1 hour", 10800: "3 hours", 86400: "1 day"} {
		if got := s.everyName(sec); got != want {
			t.Errorf("%d: %q, want %q", sec, got, want)
		}
	}
}
