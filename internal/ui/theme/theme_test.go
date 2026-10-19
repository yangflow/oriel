package theme

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func applied(t *testing.T, dark bool, prefs ui.Preferences) ui.Theme {
	t.Helper()
	var got ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		Apply(c)
		got = *c.Theme()
		ui.Text(c, "x")
	}, 200, 100)
	tt.SetDark(dark)
	tt.SetPreferences(prefs)
	tt.Frame()
	return got
}

func TestPalette(t *testing.T) {
	light := applied(t, false, ui.Preferences{})
	if light.Dark || light.Accent != ui.Hex("#0f766e") || light.Background != ui.Hex("#fbfaf7") || light.Radius != 8 {
		t.Errorf("light: %+v", light)
	}
	dark := applied(t, true, ui.Preferences{})
	if !dark.Dark || dark.Accent != ui.Hex("#4fbfad") || dark.Background != ui.Hex("#161513") {
		t.Errorf("dark: %+v", dark)
	}
}

func TestKeepsTextScale(t *testing.T) {
	normal := applied(t, false, ui.Preferences{})
	large := applied(t, false, ui.Preferences{TextScale: 2})
	if large.FontSize <= normal.FontSize {
		t.Errorf("text scale lost: %v, %v", normal.FontSize, large.FontSize)
	}
}

func TestHighContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		normal := applied(t, dark, ui.Preferences{})
		strong := applied(t, dark, ui.Preferences{HighContrast: true})
		if strong.Border == normal.Border || strong.TextMuted == normal.TextMuted || strong.Focus.A != 255 {
			t.Errorf("dark %v: high contrast changed nothing", dark)
		}
	}
}

func TestSoft(t *testing.T) {
	th := applied(t, false, ui.Preferences{})
	if s := Soft(&th, th.Accent); s == th.Accent || s == th.Background {
		t.Errorf("soft accent %v", s)
	}
}

func TestChosen(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := applied(t, dark, ui.Preferences{})
		if c := Chosen(&th); c.A == 0 || c.A == 255 {
			t.Errorf("dark %v: chosen fill %v is not a tint", dark, c)
		}
	}
}

func TestHan(t *testing.T) {
	for text, want := range map[string]bool{"Oriel 发布": true, "Hello, world": false, "": false, "かな": false} {
		if Han(text) != want {
			t.Errorf("Han(%q) = %v", text, !want)
		}
	}
}
