package prefs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, File)
	if got := Load(path); got != Default() {
		t.Errorf("missing file: %+v", got)
	}
	for _, content := range []string{"", "not json", `{"theme":3}`, `[]`} {
		os.WriteFile(path, []byte(content), 0o600)
		if got := Load(path); got != Default() {
			t.Errorf("%q: %+v", content, got)
		}
	}
}

func TestLoadUnknownValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	os.WriteFile(path, []byte(`{"theme":"sepia","language":"fr","font":"Gone Sans","readingSize":"huge","lineSpacing":"relaxed"}`), 0o600)
	want := Preferences{Font: "Gone Sans", ReadingSize: SizeMedium, LineSpacing: SpacingRelaxed}
	if got := Load(path); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	p := Preferences{Theme: ThemeDark, Language: LanguageChinese, Font: "Georgia", ReadingSize: SizeXLarge, LineSpacing: SpacingCompact}
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != p {
		t.Errorf("got %+v, want %+v", got, p)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, %v", info.Mode(), err)
		}
	}
	if err := Save(filepath.Join(t.TempDir(), "missing", File), p); err == nil {
		t.Error("saved into a directory that is not there")
	}
}

func TestDark(t *testing.T) {
	for _, tc := range []struct {
		theme  string
		system bool
		want   bool
	}{{"", false, false}, {"", true, true}, {ThemeLight, true, false}, {ThemeDark, false, true}} {
		if got := (Preferences{Theme: tc.theme}).Dark(tc.system); got != tc.want {
			t.Errorf("%q system %v: %v", tc.theme, tc.system, got)
		}
	}
}

func TestReading(t *testing.T) {
	for size, body := range map[string]float64{SizeSmall: 14.9, SizeMedium: 16, SizeLarge: 18.1, SizeXLarge: 20.3} {
		if got := (Preferences{ReadingSize: size}).Scale() * 16; got < body-1e-9 || got > body+1e-9 {
			t.Errorf("%s: body %v, want %v", size, got, body)
		}
	}
	lines := map[string][2]float64{SpacingCompact: {1.55, 1.7}, SpacingStandard: {1.7, 1.85}, SpacingRelaxed: {1.85, 2.0}}
	for spacing, want := range lines {
		p := Preferences{LineSpacing: spacing}
		if p.Leading(false) != want[0] || p.Leading(true) != want[1] {
			t.Errorf("%s: %v, %v", spacing, p.Leading(false), p.Leading(true))
		}
	}
}
