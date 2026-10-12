package example

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/locale"
)

func TestCounter(t *testing.T) {
	s := New("en")
	tt := ui.NewTester(s.View, 480, 320)
	if !tt.HasText("0") {
		t.Fatal("initial count is not visible")
	}
	for _, label := range []string{"Increment", "Increment", "Decrement"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if !tt.HasText("1") {
		t.Fatalf("button result: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyDown)
	if !tt.HasText("1") {
		t.Fatalf("keyboard result: %v", tt.Texts())
	}
	if err := tt.Click("Reset"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("0") {
		t.Fatalf("reset result: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyDown)
	if !tt.HasText("-1") {
		t.Fatal("negative count is not visible")
	}
	tt.SetDark(true)
	if !tt.HasText("-1") {
		t.Fatal("appearance change lost state")
	}
}

func TestLanguages(t *testing.T) {
	for _, language := range locale.Languages {
		t.Run(language, func(t *testing.T) {
			s := New(language)
			tt := ui.NewTester(s.View, 480, 320)
			c := translations[language]
			if c.Title == "" || c.Increment == "" || c.Decrement == "" || c.Reset == "" || c.Hint == "" {
				t.Fatal("incomplete copy")
			}
			if language != "en" && c.Title == translations["en"].Title {
				t.Fatal("untranslated title")
			}
			if err := tt.Click(c.Increment); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("1") {
				t.Fatal("localized button did not count")
			}
		})
	}
}

func TestLocaleFallback(t *testing.T) {
	for _, tc := range []struct{ locale, title string }{
		{"en-US", "Example"}, {"fr-CA", "Example"}, {"zh-CN", "示例"}, {"zh-TW", "示例"},
		{"zh-Hant-HK", "示例"}, {"xx", "Example"}, {"", "Example"},
	} {
		s := New(tc.locale)
		tt := ui.NewTester(s.View, 480, 320)
		if !tt.HasText(tc.title) {
			t.Errorf("%s: %v", tc.locale, tt.Texts())
		}
	}
}
