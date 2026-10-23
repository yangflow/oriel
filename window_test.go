package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/locale"
)

// The main window remembers where the reader left it, and never opens below its minimum size.
func TestMainWindowOptions(t *testing.T) {
	o := mainWindowOptions(nil)
	if o.StateKey == "" {
		t.Fatal("the main window has no StateKey, so its size and position are forgotten")
	}
	if o.Width != 960 || o.Height != 640 {
		t.Errorf("default size = %dx%d, want 960x640", o.Width, o.Height)
	}
	if o.MinWidth != 640 || o.MinHeight != 480 {
		t.Errorf("minimum size = %dx%d, want 640x480", o.MinWidth, o.MinHeight)
	}
}

// MyGo's own words, such as the find bar's, are in the interface language: every language Oriel offers has them.
func TestMyGoControlsSpeakTheInterfaceLanguage(t *testing.T) {
	query, open, current := "", true, 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.FindBar(c, &open, &query, 0, &current)
	}, 600, 200)
	tt.SetLocale("en")
	if !tt.HasText("Done") {
		t.Fatalf("in English: %q", tt.Texts())
	}
	for _, language := range locale.Languages {
		tt.SetLocale(language)
		if language == "zh-Hans" && (!tt.HasText("完成") || tt.HasText("Done")) {
			t.Errorf("in %s the find bar says %q", language, tt.Texts())
		}
	}
	tt.Type("zzz")
	tt.Frame()
	if !tt.HasText("无匹配项") {
		t.Errorf("no match in Chinese says %q", tt.Texts())
	}
}

// MyGo keeps the language it is given for the next launches, which would pin it against the system's: so System gives it
// none, and only a language the user chose, or a Chinese MyGo would not match, is given.
func TestMyGoLanguageIsGivenOnlyWhenItMustBe(t *testing.T) {
	cases := []struct {
		name      string
		chosen    string
		preferred []string
		want      string
	}{
		{"system, English", "", []string{"en-US"}, ""},
		{"system, Simplified Chinese", "", []string{"zh-Hans-CN", "en"}, ""},
		{"system, another language", "", []string{"fr-CA"}, ""},
		{"system, nothing", "", nil, ""},
		{"system, Traditional Chinese", "", []string{"zh-Hant-TW"}, "zh-Hans"},
		{"English chosen on a Chinese system", "en", []string{"zh-Hans-CN"}, "en"},
		{"Chinese chosen on an English system", "zh-Hans", []string{"en-US"}, "zh-Hans"},
	}
	for _, c := range cases {
		var given []string
		showMyGoLanguage(func(tag string) { given = append(given, tag) }, c.chosen, c.preferred)
		if len(given) != 1 || given[0] != c.want {
			t.Errorf("%s: SetLocale got %q, want %q", c.name, given, c.want)
		}
	}
}

// A language chosen in English shows MyGo's own controls in English, whatever the system reads.
func TestMyGoControlsInEnglishWhenEnglishIsChosen(t *testing.T) {
	query, open, current := "", true, 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.FindBar(c, &open, &query, 0, &current)
	}, 600, 200)
	showMyGoLanguage(tt.SetLocale, "en", []string{"zh-Hans-CN"})
	if !tt.HasText("Done") || tt.HasText("完成") {
		t.Errorf("English chosen: %q", tt.Texts())
	}
	showMyGoLanguage(tt.SetLocale, "zh-Hans", []string{"en-US"})
	if !tt.HasText("完成") {
		t.Errorf("Chinese chosen: %q", tt.Texts())
	}
}
