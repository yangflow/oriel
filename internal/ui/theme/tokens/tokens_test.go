package tokens

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestGeneratedIsCurrent fails when tokens.json changed and go generate was not run.
func TestGeneratedIsCurrent(t *testing.T) {
	want, err := exec.Command("go", "run", "./gen", "-out", "-").Output()
	if err != nil {
		t.Fatalf("go run ./gen: %v", err)
	}
	got, err := os.ReadFile("tokens_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may turn the file's line ends into CRLF; the generator writes LF.
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatal("tokens_gen.go is not what design/tokens.json generates; run go generate in internal/ui/theme/tokens")
	}
}

func luminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func ratio(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// bed builds a ground like "paper + selected": each later token laid over the one before.
func bed(expr string, named func(string) (ui.Color, bool)) (ui.Color, error) {
	var out ui.Color
	for i, name := range strings.Split(expr, " + ") {
		c, ok := named(name)
		if !ok {
			return out, fmt.Errorf("bed %q: no token %q", expr, name)
		}
		if i == 0 {
			out = c
		} else {
			out = c.Over(out)
		}
	}
	return out, nil
}

// audit checks every target against its grounds, with named looking tokens up. A preset is checked on the main
// grounds that are not floating, for the targets that are neither the pane's nor on a token.
func audit(t *testing.T, where string, preset bool, named func(string) (ui.Color, bool)) {
	t.Helper()
	for _, target := range AuditTargets {
		isPane := strings.HasPrefix(target.Token, "pane-")
		if preset && (isPane || target.On != "") {
			continue
		}
		fg, ok := named(target.Token)
		if !ok {
			t.Errorf("%s: no token %q", where, target.Token)
			continue
		}
		grounds := AuditGrounds["main"]
		switch {
		case target.On != "":
			grounds = []string{target.On}
		case isPane:
			grounds = AuditGrounds["pane"]
		}
		for _, expr := range grounds {
			if preset && strings.HasPrefix(expr, "float") {
				continue
			}
			bg, err := bed(expr, named)
			if err != nil {
				t.Error(err)
				continue
			}
			if r := ratio(fg.Over(bg), bg); r < target.Min {
				t.Errorf("%s: %s on %s is %.2f:1, below %.1f:1", where, target.Token, expr, r, target.Min)
			}
		}
	}
}

// TestContrastAudit holds every text token to its floor on every ground it is drawn on, in both schemes, in the pane
// and in every reading preset. A preset takes the main scheme's value for a token it does not set, and its alpha
// fills are its ink at the main ladder's alphas.
func TestContrastAudit(t *testing.T) {
	for i, s := range []*Scheme{&SchemeLight, &SchemeDark} {
		where := [2]string{"light", "dark"}[i]
		audit(t, where, false, s.Named)
		for _, name := range PresetNames {
			p := Presets[name][i]
			named := func(token string) (ui.Color, bool) {
				if c, ok := p.Named(token); ok {
					return c, true
				}
				if c, ok := s.Named(token); ok && c.A < 255 {
					return p.Ink.Alpha(float32(c.A) / 255), true
				}
				return s.Named(token)
			}
			audit(t, where+" "+name, true, named)
		}
	}
}

// TestRolesCoverTheme checks that the tokens marked mygo set the MyGo roles Oriel's palette owns.
func TestRolesCoverTheme(t *testing.T) {
	var th ui.Theme
	SchemeLight.SetRoles(&th)
	SetMetrics(&th)
	for name, c := range map[string]ui.Color{
		"Background": th.Background, "Surface": th.Surface, "Text": th.Text, "TextMuted": th.TextMuted,
		"Accent": th.Accent, "AccentText": th.AccentText, "Border": th.Border, "Focus": th.Focus,
		"Inverse": th.Inverse, "InverseText": th.InverseText, "Danger": th.Danger, "Scrollbar": th.Scrollbar,
	} {
		if c == (ui.Color{}) {
			t.Errorf("no token sets %s", name)
		}
	}
	if th.Spacing != Space1 || th.Radius != RadiusControl {
		t.Errorf("metrics: Spacing %v, Radius %v", th.Spacing, th.Radius)
	}
	var pane ui.Theme
	SchemeLight.SetPaneRoles(&pane)
	if pane.Background != SchemeLight.Pane || pane.Text != SchemeLight.PaneFg {
		t.Errorf("pane roles: Background %v, Text %v", pane.Background, pane.Text)
	}
}
