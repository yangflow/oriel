package reader

import (
	"slices"
	"testing"
)

func TestTranslatedTitleHeadsTheArticleWithTheOriginalUnderIt(t *testing.T) {
	for language, want := range map[string]string{"zh-Hans": "Go 1.27 发布了", "en": "Go 1.27 is released"} {
		t.Run(language, func(t *testing.T) {
			a := full()
			a.Entry.Title, a.Entry.TranslatedTitle = "Go 1.27 wurde veröffentlicht", want
			a.ExtractedHTML = "<p>Heute ist es soweit.</p>"
			_, f, tt := setup(t, language, a)
			f.settle(tt)
			if !slices.Contains(tt.Texts(), want) {
				t.Errorf("no %q in %v", want, tt.Texts())
			}
			// The original is one line under the title, and nowhere else.
			n := 0
			for _, text := range tt.Texts() {
				if text == "Go 1.27 wurde veröffentlicht" {
					n++
				}
			}
			if n != 1 {
				t.Errorf("the original shows %d times in %v", n, tt.Texts())
			}
		})
	}
}

func TestUntranslatedTitleHasNoSecondLine(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	n := 0
	for _, text := range tt.Texts() {
		if text == "Go 1.27 is released" {
			n++
		}
	}
	// One is the name of the scrolling page, the other the heading.
	if n != 2 {
		t.Errorf("the title shows %d times in %v", n, tt.Texts())
	}
}
