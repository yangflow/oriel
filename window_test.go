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
