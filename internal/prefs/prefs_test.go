package prefs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// TestChineseTypesetting shows the setting is on by default, also in a file from before it existed, and that off is kept.
func TestChineseTypesetting(t *testing.T) {
	if !Default().ChineseTypesetting {
		t.Error("off by default")
	}
	path := filepath.Join(t.TempDir(), File)
	os.WriteFile(path, []byte(`{"readingSize":"large"}`), 0o600)
	if got := Load(path); !got.ChineseTypesetting || got.ReadingSize != SizeLarge {
		t.Errorf("old file: %+v", got)
	}
	off := Default()
	off.ChineseTypesetting = false
	if err := Save(path, off); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got.ChineseTypesetting {
		t.Errorf("off came back on: %+v", got)
	}
}

func TestLoadUnknownValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	os.WriteFile(path, []byte(`{"theme":"sepia","language":"fr","font":"Gone Sans","readingSize":"huge","lineSpacing":"relaxed","readingFont":"Gone Serif"}`), 0o600)
	want := Preferences{Font: "Gone Sans", ReadingSize: SizeMedium, LineSpacing: SpacingRelaxed, ReadingFont: "Gone Serif", ChineseTypesetting: true,
		ArticleViewer: ViewList, VideoViewer: ViewGrid, SocialViewer: ViewCard, ImageViewer: ViewWall}
	if got := Load(path); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestViewers shows each kind starts at its default viewer, also in a file from before they existed, and that a value
// the kind does not offer reads as the default.
func TestViewers(t *testing.T) {
	d := Default()
	if d.ArticleViewer != "list" || d.VideoViewer != "grid" || d.SocialViewer != "card" || d.ImageViewer != "wall" {
		t.Errorf("defaults: %+v", d)
	}
	path := filepath.Join(t.TempDir(), File)
	os.WriteFile(path, []byte(`{"readingSize":"large"}`), 0o600)
	if got := Load(path); got.ArticleViewer != ViewList || got.VideoViewer != ViewGrid || got.SocialViewer != ViewCard || got.ImageViewer != ViewWall {
		t.Errorf("old file: %+v", got)
	}
	os.WriteFile(path, []byte(`{"articleViewer":"wall","videoViewer":"card","socialViewer":"Compact","imageViewer":"cover"}`), 0o600)
	if got := Load(path); got.ArticleViewer != ViewList || got.VideoViewer != ViewGrid || got.SocialViewer != ViewCard || got.ImageViewer != ViewWall {
		t.Errorf("unknown values: %+v", got)
	}
	os.WriteFile(path, []byte(`{"articleViewer":"compact","videoViewer":"list","socialViewer":"compact","imageViewer":"card"}`), 0o600)
	if got := Load(path); got.ArticleViewer != ViewCompact || got.VideoViewer != ViewList || got.SocialViewer != ViewCompact || got.ImageViewer != ViewCard {
		t.Errorf("known values: %+v", got)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	p := Preferences{Theme: ThemeDark, Language: LanguageChinese, Font: "Georgia", ReadingSize: SizeXLarge, LineSpacing: SpacingCompact, ReadingFont: ReadingSerif, ChineseTypesetting: false,
		ArticleViewer: ViewCompact, VideoViewer: ViewCover, SocialViewer: ViewCompact, ImageViewer: ViewGrid}
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
		if got := (Preferences{ReadingSize: size}).Scale() * 16; got < body-0.05 || got > body+0.05 {
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

func TestColumn(t *testing.T) {
	for size, want := range map[string]float64{SizeSmall: 635, SizeMedium: 680, SizeLarge: 771, SizeXLarge: 861} {
		if got := (Preferences{ReadingSize: size}).Column(); got != want {
			t.Errorf("%s: column %v, want %v", size, got, want)
		}
	}
}

func TestSerifFamilies(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "Songti SC", "windows": "Georgia", "linux": "Noto Serif"} {
		if got := SerifFamilies(goos); len(got) == 0 || !contains(got, want) {
			t.Errorf("%s: %v lacks %s", goos, got, want)
		}
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func TestReadingFace(t *testing.T) {
	serif := Preferences{ReadingFont: ReadingSerif}
	if got := (Preferences{}).ReadingFace("linux", []string{"Noto Serif"}, "Inter"); got != "" {
		t.Errorf("follow interface: %q", got)
	}
	if got := serif.ReadingFace("linux", []string{"Noto Serif", "Noto Serif CJK SC", "Inter"}, "Inter"); got != "Noto Serif,Noto Serif CJK SC,Inter" {
		t.Errorf("serif: %q", got)
	}
	if got := serif.ReadingFace("linux", []string{"Inter"}, "Inter"); got != "" {
		t.Errorf("no serif installed: %q", got)
	}
	if got := serif.ReadingFace("darwin", []string{"Songti SC"}, ""); !strings.Contains(got, "Songti SC") || !strings.Contains(got, SystemSerif) {
		t.Errorf("darwin: %q", got)
	}
	named := Preferences{ReadingFont: "Gone Serif"}
	if got := named.ReadingFace("linux", []string{"Inter"}, "Inter"); got != "" {
		t.Errorf("missing family: %q", got)
	}
	if got := named.ReadingFace("linux", []string{"Gone Serif", "Inter"}, "Inter"); got != "Gone Serif,Inter" {
		t.Errorf("installed family: %q", got)
	}
	if got := named.ReadingFace("linux", nil, "Inter"); got != "Gone Serif,Inter" {
		t.Errorf("unknown list: %q", got)
	}
}

// TestGrids covers the per-scope switch between the list and the grid: a round trip through the file, that a switch
// equal to the default is removed, and that scopes and values it does not know are ignored.
func TestGrids(t *testing.T) {
	if got := GridScope("video", 0); got != "kind:video" {
		t.Errorf("kind scope %q", got)
	}
	if got := GridScope("image", 12); got != "source:12" {
		t.Errorf("source scope %q", got)
	}
	var g Grids
	g = g.Set("source:12", ViewGrid, ViewList)
	g = g.Set("kind:video", ViewList, ViewGrid)
	if g.Get("source:12") != ViewGrid || g.Get("kind:video") != ViewList || g.Get("kind:image") != "" {
		t.Fatalf("got %q", g)
	}
	path := filepath.Join(t.TempDir(), File)
	p := Default()
	p.Grids = g
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != p {
		t.Errorf("round trip: %+v, want %+v", got, p)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"grids":{"kind:video":"list","source:12":"grid"}`) {
		t.Errorf("file %s", data)
	}
	// Back to what the viewers choose: the entry goes.
	g = g.Set("source:12", ViewList, ViewList).Set("kind:video", ViewGrid, ViewGrid)
	if g != "" {
		t.Errorf("defaults left %q", g)
	}
	p.Grids = g
	Save(path, p)
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "grids") {
		t.Errorf("empty switches written: %s", data)
	}
	// Unknown scopes and values never get in.
	for _, scope := range []string{"kind:article", "kind:", "source:0", "source:-1", "source:x", "source:07", "all"} {
		if got := (Grids("")).Set(scope, ViewGrid, ViewList); got != "" {
			t.Errorf("%q accepted: %q", scope, got)
		}
	}
	if got := (Grids("")).Set("kind:image", "wall", ViewList); got != "" {
		t.Errorf("value accepted: %q", got)
	}
	os.WriteFile(path, []byte(`{"grids":{"kind:video":"cover","kind:image":"grid","source:3":"list","source:z":"grid","tag:a":"grid"}}`), 0o600)
	if got := Load(path).Grids; got != "kind:image=grid,source:3=list" {
		t.Errorf("unknown entries kept: %q", got)
	}
	os.WriteFile(path, []byte(`{"readingSize":"large","grids":["kind:image"]}`), 0o600)
	if got := Load(path); got.Grids != "" || got.ReadingSize != SizeLarge {
		t.Errorf("bad shape: %+v", got)
	}
}
