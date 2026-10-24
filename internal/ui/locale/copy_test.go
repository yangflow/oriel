package locale

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// catalogs reads every screen's copy.json, language by language, key by key.
func catalogs(t *testing.T) map[string]map[string]map[string]string {
	t.Helper()
	files, err := filepath.Glob("../*/copy.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no catalogs found: %v", err)
	}
	all := map[string]map[string]map[string]string{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var byLanguage map[string]map[string]any
		if err := json.Unmarshal(raw, &byLanguage); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		screen := filepath.Base(filepath.Dir(file))
		all[screen] = map[string]map[string]string{}
		for language, entries := range byLanguage {
			all[screen][language] = map[string]string{}
			for key, value := range entries {
				if text, ok := value.(string); ok {
					all[screen][language][key] = text
				}
			}
		}
	}
	return all
}

// each calls fn for every string of every catalog.
func each(t *testing.T, fn func(screen, language, key, text string)) {
	t.Helper()
	for screen, languages := range catalogs(t) {
		for language, entries := range languages {
			for key, text := range entries {
				fn(screen, language, key, text)
			}
		}
	}
}

// The ellipsis is the one character "…" and never three dots.
func TestCopyUsesTheEllipsisCharacter(t *testing.T) {
	each(t, func(screen, language, key, text string) {
		if strings.Contains(text, "...") {
			t.Errorf("%s %s %s: %q uses three dots; write the ellipsis character", screen, language, key, text)
		}
	})
}

var twoCapitals = regexp.MustCompile(`\b[A-Z][a-z]+ [A-Z][a-z]+`)

// Short English labels are in sentence case: "Open settings", "Keyboard shortcuts", "Back up…". A name that is
// written with capitals is listed here by key.
func TestEnglishLabelsAreInSentenceCase(t *testing.T) {
	names := map[string]bool{"reader LanguageZhHans": true, "tray Open": true}
	each(t, func(screen, language, key, text string) {
		if language != "en" || len(text) >= 45 || names[screen+" "+key] {
			return
		}
		if twoCapitals.MatchString(text) {
			t.Errorf("%s %s: %q is not in sentence case", screen, key, text)
		}
	})
}

var cjkBesideLatin = regexp.MustCompile(`[\x{4e00}-\x{9fff}][A-Za-z0-9]|[A-Za-z0-9][\x{4e00}-\x{9fff}]`)

// Chinese strings keep one space between Chinese and Latin letters or digits. Date layouts are exempt.
func TestChineseSpacing(t *testing.T) {
	each(t, func(screen, language, key, text string) {
		if language != "zh-Hans" || strings.HasSuffix(key, "Layout") {
			return
		}
		if m := cjkBesideLatin.FindString(text); m != "" {
			t.Errorf("%s %s: %q has no space in %q", screen, key, text, m)
		}
	})
}

// The address that takes over when the first RSSHub address does not answer is the "fallback address" and, in
// Chinese, the "备用地址". "Backup" is the copy of the data, so it never names it.
func TestFallbackAddressTerm(t *testing.T) {
	each(t, func(screen, language, key, text string) {
		lower := strings.ToLower(text)
		switch language {
		case "en":
			for _, bad := range []string{"backup instance", "backup address", "backup server", "(backup)"} {
				if strings.Contains(lower, bad) {
					t.Errorf("%s %s: %q says %q; the term is fallback address", screen, key, text, bad)
				}
			}
		case "zh-Hans":
			for _, bad := range []string{"备用实例", "备用服务", "（备用）"} {
				if strings.Contains(text, bad) {
					t.Errorf("%s %s: %q says %q; the term is 备用地址", screen, key, text, bad)
				}
			}
		}
	})
}

// The RSSHub address in examples and placeholders is rsshub.example.com, never the public instance that blocks
// automated requests.
func TestRSSHubExampleAddress(t *testing.T) {
	each(t, func(screen, language, key, text string) {
		if strings.Contains(text, "https://rsshub.app") {
			t.Errorf("%s %s %s: %q points at the public instance; use https://rsshub.example.com", screen, language, key, text)
		}
	})
}
