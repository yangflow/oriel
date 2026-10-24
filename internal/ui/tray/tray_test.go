package tray

import (
	"testing"

	"github.com/yangflow/oriel/internal/ui/locale"
)

func TestCatalogIsComplete(t *testing.T) {
	if len(translations) != len(locale.Languages) {
		t.Errorf("catalog has %d languages, want %d", len(translations), len(locale.Languages))
	}
	for _, language := range locale.Languages {
		c := translations[language]
		if c.Unread == "" || c.None == "" || c.Tooltip == "" || c.Open == "" || c.Refresh == "" || c.Quit == "" {
			t.Errorf("%s: incomplete copy", language)
		}
		if language != "en" && c.Quit == translations["en"].Quit {
			t.Errorf("%s: untranslated", language)
		}
	}
}

func TestCount(t *testing.T) {
	for n, want := range map[int]string{-1: "", 0: "", 1: "1", 999: "999", 1000: "999+", 5000: "999+"} {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestWords(t *testing.T) {
	if got := Unread("en", 12); got != "12 unread" {
		t.Errorf("unread = %q", got)
	}
	if got := Unread("zh-Hans", 0); got != "没有未读" {
		t.Errorf("none = %q", got)
	}
	if got := Tooltip("zh-Hans", 3000); got != "Oriel，999+ 条未读" {
		t.Errorf("tooltip = %q", got)
	}
	if got := Tooltip("en", 0); got != "Oriel" {
		t.Errorf("tooltip at zero = %q", got)
	}
	if got := Menu("zh-Hans"); got.Quit != "退出" {
		t.Errorf("menu = %+v", got)
	}
}
