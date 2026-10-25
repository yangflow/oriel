package timeline

import (
	"slices"
	"strings"
	"testing"
)

func TestTranslatedTitleShowsWithTheOriginalUnderIt(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	f.entries[0].TranslatedTitle = "Go 1.27 发布了"
	f.entries[1].TranslatedTitle = "实战中的性能分析"
	f.entries[3].TranslatedTitle = "黎明的港口"
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)

	// Articles and videos: the translation is the title, the original a line under it.
	for _, want := range []string{"Go 1.27 发布了", "Go 1.27 is released", "实战中的性能分析", "Profiling in practice"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
	}
	// Images show the translation alone; the original is in the name read aloud.
	if !tt.HasText("黎明的港口") || slices.Contains(tt.Texts(), "Harbor at dawn") {
		t.Errorf("image row: %v", tt.Texts())
	}
	if label := s.label(f.entries[3], f.now); !strings.Contains(label, "黎明的港口") || !strings.Contains(label, "Harbor at dawn") {
		t.Errorf("label = %q", label)
	}
	// An entry without a translation shows its title as before, with no second line.
	if !tt.HasText("Range over func") {
		t.Errorf("untranslated entry lost its title: %v", tt.Texts())
	}
}

func TestTranslatedTitleInEnglish(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.entries[0].Title, f.entries[0].TranslatedTitle = "Go 1.27 wurde veröffentlicht", "Go 1.27 is released"
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText("Go 1.27 is released") || !tt.HasText("Go 1.27 wurde veröffentlicht") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
