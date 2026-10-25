package prefs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	want := Preferences{Font: "Gone Sans", ReadingSize: SizeMedium, LineSpacing: SpacingRelaxed, ReadingWidth: WidthStandard, ReadingFont: "Gone Serif", ChineseTypesetting: true, Density: DensityComfortable,
		ArticleViewer: ViewList, VideoViewer: ViewGrid, SocialViewer: ViewCard, ImageViewer: ViewWall, SourceSort: SortTitle, MarkReadWhen: MarkOnOpen, MarkReadDwellSec: 5}
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

// TestDensityAndOrder shows the timeline starts comfortable and newest first, also in a file from before they
// existed, and that a density the settings do not offer reads as comfortable.
func TestDensityAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	if d := Default(); d.Density != DensityComfortable || d.OldestFirst {
		t.Errorf("defaults: %+v", d)
	}
	os.WriteFile(path, []byte(`{"readingSize":"large"}`), 0o600)
	if got := Load(path); got.Density != DensityComfortable || got.OldestFirst {
		t.Errorf("old file: %+v", got)
	}
	os.WriteFile(path, []byte(`{"density":"tiny","oldestFirst":true}`), 0o600)
	if got := Load(path); got.Density != DensityComfortable || !got.OldestFirst {
		t.Errorf("unknown density: %+v", got)
	}
	os.WriteFile(path, []byte(`{"density":"compact"}`), 0o600)
	if got := Load(path); got.Density != DensityCompact || got.OldestFirst {
		t.Errorf("compact: %+v", got)
	}
}

// TestMarkReadWhen shows an entry is marked read when it opens by default, also in a file from before the setting
// existed, that a choice the settings do not offer reads as the default, and that MarkRead agrees with Normalize.
func TestMarkReadWhen(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	if d := Default(); d.MarkReadWhen != MarkOnOpen || d.MarkReadDwellSec != 5 {
		t.Errorf("defaults: %+v", d)
	}
	os.WriteFile(path, []byte(`{"readingSize":"large"}`), 0o600)
	if got := Load(path); got.MarkReadWhen != MarkOnOpen || got.MarkReadDwellSec != 5 {
		t.Errorf("old file: %+v", got)
	}
	os.WriteFile(path, []byte(`{"markReadWhen":"soon","markReadDwellSec":7}`), 0o600)
	if got := Load(path); got.MarkReadWhen != MarkOnOpen || got.MarkReadDwellSec != 5 {
		t.Errorf("unknown values: %+v", got)
	}
	for _, when := range MarkReadChoices {
		for _, sec := range DwellSeconds {
			p := Preferences{MarkReadWhen: when, MarkReadDwellSec: sec}
			if got, stay := p.MarkRead(); got != when || stay != time.Duration(sec)*time.Second {
				t.Errorf("%s %d s: %s, %v", when, sec, got, stay)
			}
		}
	}
	if when, stay := (Preferences{MarkReadWhen: "x", MarkReadDwellSec: 1}).MarkRead(); when != MarkOnOpen || stay != 5*time.Second {
		t.Errorf("unknown: %s, %v", when, stay)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	p := Preferences{Theme: ThemeDark, Language: LanguageChinese, Font: "Georgia", ReadingSize: SizeXLarge, LineSpacing: SpacingCompact, ReadingWidth: WidthWide, ReadingPreset: PresetSepia, ReadingFont: ReadingSerif, ChineseTypesetting: false,
		ArticleViewer: ViewCompact, VideoViewer: ViewCover, SocialViewer: ViewCompact, ImageViewer: ViewGrid, SourceSort: SortUnread,
		Density: DensityCompact, OldestFirst: true, MarkReadWhen: MarkOnScroll, MarkReadDwellSec: 10}
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

func TestSidebarHidden(t *testing.T) {
	if Default().SidebarHidden {
		t.Fatal("the sidebar is hidden by default")
	}
	path := filepath.Join(t.TempDir(), File)
	p := Default()
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "sidebarHidden") {
		t.Errorf("the default is written: %s", data)
	}
	p.SidebarHidden = true
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !got.SidebarHidden {
		t.Errorf("round trip: %+v", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"sidebarHidden":true`) {
		t.Errorf("file %s", data)
	}
}

func TestSourceSort(t *testing.T) {
	if got := Default().SourceSort; got != SortTitle {
		t.Fatalf("default %q", got)
	}
	path := filepath.Join(t.TempDir(), File)
	for body, want := range map[string]string{
		`{"sourceSort":"unread"}`:   SortUnread,
		`{"sourceSort":"sideways"}`: SortTitle,
		`{}`:                        SortTitle,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Load(path).SourceSort; got != want {
			t.Errorf("%s loads as %q, want %q", body, got, want)
		}
	}
}

// TestLastLooked shows the marker survives a save and a load, is left out while there is none, and that a value that
// cannot be a marker is dropped without costing the other preferences.
func TestLastLooked(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	p := Default()
	Save(path, p)
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "lastLooked") {
		t.Errorf("no marker written: %s", data)
	}
	p.LastLooked = Marker{At: 1700000000000, ID: 812}
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != p {
		t.Errorf("round trip: %+v, want %+v", got, p)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"lastLooked":{"at":1700000000000,"id":812}`) {
		t.Errorf("file %s", data)
	}
	for _, bad := range []string{`"x"`, `{"at":"x"}`, `{"at":-5,"id":1}`, `{"at":5,"id":-1}`, `[]`, `7`, `null`} {
		os.WriteFile(path, []byte(`{"theme":"dark","lastLooked":`+bad+`}`), 0o600)
		got := Load(path)
		if got.LastLooked.Valid() || got.Theme != ThemeDark {
			t.Errorf("%s: %+v", bad, got)
		}
	}
	m := Marker{At: 10, ID: 5}
	for _, c := range []struct {
		at, id int64
		newer  bool
	}{{11, 1, true}, {10, 6, true}, {10, 5, false}, {10, 4, false}, {9, 9, false}} {
		if got := m.Newer(c.at, c.id); got != c.newer {
			t.Errorf("Newer(%d, %d) = %v", c.at, c.id, got)
		}
	}
}

// TestPlaces shows the place of each server survives a save and a load, is left out for a server in the whole
// timeline, that an old file without the field loads, and that a value that cannot be a place is dropped without
// costing the other preferences.
func TestPlaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o600)
	if got := Load(path); got.Theme != ThemeDark || got.Places != "" || got.Places.Get("") != (Place{}) {
		t.Fatalf("an old file: %+v", got)
	}
	p := Default()
	p.Places = p.Places.Set("home.lan:7470", Place{Kind: "video", Filter: FilterUnread, Range: RangeToday})
	p.Places = p.Places.Set("", Place{Kind: "article", Source: 12})
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got != p {
		t.Errorf("round trip: %+v, want %+v", got, p)
	}
	if got.Places.Get("home.lan:7470") != (Place{Kind: "video", Filter: FilterUnread, Range: RangeToday}) ||
		got.Places.Get("") != (Place{Kind: "article", Source: 12}) || got.Places.Get("other") != (Place{}) {
		t.Errorf("places %s", got.Places)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"places":{"":{"kind":"article","source":12},"home.lan:7470":{"kind":"video","filter":"unread","range":"today"}}`) {
		t.Errorf("file %s", data)
	}
	if got := p.Places.Set("r", Place{Range: Range24h}).Get("r"); got != (Place{Range: Range24h}) {
		t.Errorf("a range alone is a place: %+v", got)
	}
	// Back to the whole timeline drops the entry; the last one drops the field.
	p.Places = p.Places.Set("home.lan:7470", Place{}).Set("", Place{})
	Save(path, p)
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "places") || Load(path).Places != "" {
		t.Errorf("empty places written: %s", data)
	}
	// A place it does not know changes nothing.
	if q := p.Places.Set("x", Place{Kind: "podcast"}).Set("x", Place{Source: -1}).Set("x", Place{Filter: "all"}).Set("x", Place{Range: "week"}); q != p.Places {
		t.Errorf("unknown place kept: %s", q)
	}
	for _, bad := range []string{`"x"`, `[]`, `7`, `null`, `{"a":"x"}`, `{"a":{"kind":"podcast"}}`, `{"a":{"source":-1}}`, `{"a":{"filter":"x"}}`, `{"a":{"range":"week"}}`} {
		os.WriteFile(path, []byte(`{"theme":"dark","places":`+bad+`}`), 0o600)
		got := Load(path)
		if got.Places != "" || got.Theme != ThemeDark {
			t.Errorf("%s: %+v", bad, got)
		}
	}
}

func TestDesktop(t *testing.T) {
	if d := Default(); d.OpenAtLogin || d.TrayIcon || d.DockBadge {
		t.Fatalf("the desktop switches are not all off by default: %+v", d)
	}
	path := filepath.Join(t.TempDir(), File)
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, key := range []string{"openAtLogin", "trayIcon", "dockBadge"} {
		if strings.Contains(string(data), key) {
			t.Errorf("the default is written: %s", data)
		}
	}
	p := Default()
	p.OpenAtLogin, p.TrayIcon, p.DockBadge = true, true, true
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !got.OpenAtLogin || !got.TrayIcon || !got.DockBadge {
		t.Errorf("round trip: %+v", got)
	}
	data, _ = os.ReadFile(path)
	for _, want := range []string{`"openAtLogin":true`, `"trayIcon":true`, `"dockBadge":true`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file %s lacks %s", data, want)
		}
	}
	if got := p.Normalize(); got != p {
		t.Errorf("Normalize changed the desktop switches: %+v", got)
	}
}

// TestReadingAppearance shows the line width and the reading appearance start at Standard and Follow theme, also in a
// file from before they existed, that a value this build does not offer reads as those, and that a choice is kept.
func TestReadingAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	for content, want := range map[string][2]string{
		`{}`: {WidthStandard, ""},
		`{"readingWidth":"huge","readingPreset":"neon"}`:    {WidthStandard, ""},
		`{"readingWidth":"narrow","readingPreset":"sepia"}`: {WidthNarrow, PresetSepia},
		`{"readingWidth":"wide","readingPreset":"mist"}`:    {WidthWide, PresetMist},
	} {
		os.WriteFile(path, []byte(content), 0o600)
		if got := Load(path); got.ReadingWidth != want[0] || got.ReadingPreset != want[1] {
			t.Errorf("%s: width %q, preset %q", content, got.ReadingWidth, got.ReadingPreset)
		}
	}
}

// TestColumnWidth shows the three line widths scale the column by 0.85, 1 and 1.2, and that an unknown width is Standard.
func TestColumnWidth(t *testing.T) {
	for width, want := range map[string]float64{WidthNarrow: 578, WidthStandard: 680, WidthWide: 816, "": 680, "huge": 680} {
		p := Preferences{ReadingSize: SizeMedium, ReadingWidth: width}
		if got := p.Column(); got != want {
			t.Errorf("%q: column %v, want %v", width, got, want)
		}
	}
	if got := (Preferences{ReadingSize: SizeSmall, ReadingWidth: WidthWide}).Column(); got != 762 {
		t.Errorf("small and wide: column %v, want 762", got)
	}
}
