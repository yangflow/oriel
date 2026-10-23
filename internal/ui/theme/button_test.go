package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/icons"
)

// drawn draws bt in a themed window and returns the rect of the button and the theme.
func drawn(t *testing.T, bt Button, high bool) (ui.Rect, ui.Theme) {
	t.Helper()
	// Find returns the box of the text, so name the button to get the box of the button.
	if bt.Label == "" {
		bt.Label = "button"
	}
	var th ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		Apply(c, prefs.Default())
		th = *c.Theme()
		ui.Row(c).Padding(10).Children(func() { bt.Draw(c) })
	}, 400, 120)
	tt.SetPreferences(ui.Preferences{HighContrast: high})
	tt.Frame()
	r, ok := tt.Find(bt.Label)
	if !ok {
		t.Fatalf("no button %q", bt.Label)
	}
	return r, th
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.51 }

func TestButtonSizes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bt    Button
		wantH float32
	}{
		{"regular", Button{Text: "Save"}, 32},
		{"primary", Button{Style: Primary, Text: "Save"}, 32},
		{"danger", Button{Style: Danger, Text: "Delete"}, 32},
		{"plain", Button{Style: Plain, Text: "More"}, 32},
		{"small", Button{Small: true, Text: "Refresh"}, 26},
		{"small danger", Button{Style: Danger, Small: true, Text: "Sign out"}, 26},
		{"with icon", Button{Icon: icons.Plus, Text: "Add"}, 32},
		{"busy", Button{Busy: true, Text: "Saving"}, 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, th := drawn(t, tc.bt, false)
			if want := Px(&th, tc.wantH); !near(r.H, want) {
				t.Errorf("height %v, want %v", r.H, want)
			}
		})
	}
}

// Every variant of a size has the same height, and a Danger button is not wider than a Secondary one for the same
// text: it has less padding, so its text lines up with the edge of the row.
func TestButtonPadding(t *testing.T) {
	sec, th := drawn(t, Button{Text: "Delete"}, false)
	pri, _ := drawn(t, Button{Style: Primary, Text: "Delete"}, false)
	dan, _ := drawn(t, Button{Style: Danger, Text: "Delete"}, false)
	if !near(sec.W, pri.W) {
		t.Errorf("primary %v wide, secondary %v", pri.W, sec.W)
	}
	if want := 2 * (Px(&th, 14) - th.Space(2)); !near(sec.W-dan.W, want) {
		t.Errorf("danger %v wide, secondary %v, want %v less", dan.W, sec.W, want)
	}
	small, _ := drawn(t, Button{Small: true, Style: Danger, Text: "Delete"}, false)
	smallSecondary, _ := drawn(t, Button{Small: true, Text: "Delete"}, false)
	if !near(small.W, smallSecondary.W) {
		t.Errorf("small danger %v wide, small secondary %v", small.W, smallSecondary.W)
	}
}

func TestButtonIconOnly(t *testing.T) {
	r, th := drawn(t, Button{Style: Plain, Icon: icons.X, Label: "Close"}, false)
	side := Px(&th, 32)
	if !near(r.W, side) || !near(r.H, side) || r.W < MinTarget {
		t.Errorf("icon-only button %v by %v, want %v square", r.W, r.H, side)
	}
	r, th = drawn(t, Button{Small: true, Icon: icons.X, Label: "Close"}, false)
	if side = Px(&th, 26); !near(r.W, side) || !near(r.H, side) || r.W < MinTarget {
		t.Errorf("small icon-only button %v by %v, want %v square", r.W, r.H, side)
	}
}

func TestButtonGrow(t *testing.T) {
	var a, b ui.Rect
	tt := ui.NewTester(func(c *ui.Context) {
		Apply(c, prefs.Default())
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			Button{Grow: true, Text: "Local", Label: "a"}.Draw(c)
			Button{Style: Primary, Grow: true, Text: "Connect", Icon: icons.ArrowRight, IconAfter: true, Label: "b"}.Draw(c)
		})
	}, 400, 120)
	tt.Frame()
	a, _ = tt.Find("a")
	b, _ = tt.Find("b")
	if !near(a.W, b.W) || a.W == 0 {
		t.Errorf("buttons that grow are %v and %v wide", a.W, b.W)
	}
}

func TestButtonLook(t *testing.T) {
	for _, dark := range []bool{false, true} {
		var th ui.Theme
		var got = map[string][2]ui.Color{}
		tt := ui.NewTester(func(c *ui.Context) {
			Apply(c, prefs.Default())
			th = *c.Theme()
			ui.Column(c).Children(func() {
				for name, bt := range map[string]Button{
					"secondary": {Text: "a"}, "primary": {Style: Primary, Text: "b"}, "danger": {Style: Danger, Text: "c"},
					"plain": {Style: Plain, Text: "d"}, "on": {On: true, Text: "e"},
					"disabled": {Disabled: true, Text: "f"}, "disabled primary": {Style: Primary, Disabled: true, Text: "g"},
					"disabled danger": {Style: Danger, Disabled: true, Text: "h"},
				} {
					b := ui.ButtonBase(c).Key(name)
					fill, color := bt.look(c.Theme(), b)
					got[name] = [2]ui.Color{fill, color}
				}
			})
		}, 200, 200)
		tt.SetDark(dark)
		tt.Frame()
		for name, want := range map[string][2]ui.Color{
			"secondary":        {Track(&th), th.Text},
			"primary":          {th.Accent, th.AccentText},
			"danger":           {{}, th.Danger},
			"plain":            {{}, th.Text},
			"on":               {Soft(&th, th.Accent), th.Accent},
			"disabled":         {th.Surface, th.TextMuted},
			"disabled primary": {th.Surface, th.TextMuted},
			"disabled danger":  {{}, th.TextMuted},
		} {
			if got[name] != want {
				t.Errorf("dark %v %s: fill and color %v, want %v", dark, name, got[name], want)
			}
		}
	}
}

// High contrast adds a rim and must not change the size of the button.
func TestButtonHighContrast(t *testing.T) {
	plain, _ := drawn(t, Button{Text: "Save"}, false)
	high, _ := drawn(t, Button{Text: "Save"}, true)
	if !near(plain.H, high.H) {
		t.Errorf("height %v, %v with high contrast", plain.H, high.H)
	}
}
