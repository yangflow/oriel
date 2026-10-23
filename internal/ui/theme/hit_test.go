package theme

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHitTarget(t *testing.T) {
	for _, tc := range []struct {
		name         string
		hit          Hit
		wantW, wantH float32
		x, y         float32
	}{
		{"icon 20", Hit{W: 20, H: 20}, 24, 24, -2, -2},
		{"icon 22", Hit{W: 22, H: 22}, 24, 24, -1, -1},
		{"icon 28", Hit{W: 28, H: 28}, 28, 28, 0, 0},
		{"text 20", Hit{H: 20, PadX: 40}, 90, 24, 0, -2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var x, y float32
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Padding(10).Children(func() {
					b := ui.ButtonBase(c).Label("go")
					x, y = tc.hit.Apply(c, b, func() { ui.Box(c).Size(10, 10) })
					b.Margin(y, x, y, x)
				})
			}, 200, 100)
			tt.Frame()
			r, ok := tt.Find("go")
			if !ok {
				t.Fatal("no button")
			}
			if r.W < MinTarget || r.H < MinTarget || r.W != tc.wantW || r.H != tc.wantH {
				t.Errorf("target %v by %v, want %v by %v and at least %d", r.W, r.H, tc.wantW, tc.wantH, MinTarget)
			}
			if x != tc.x || y != tc.y {
				t.Errorf("margins %v, %v, want %v, %v", x, y, tc.x, tc.y)
			}
		})
	}
}

// A button built with a fixed size under MinTarget has a target that is too small: Hit draws the small control in a
// target big enough.
func TestNoSmallButtonSizes(t *testing.T) {
	size := regexp.MustCompile(`ButtonBase\(c\)[^\n]*?\.(Size|Height|Width)\(([^\n]*?)\)\.`)
	num := regexp.MustCompile(`px\(t, (\d+(?:\.\d+)?)\)`)
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			for _, m := range size.FindAllStringSubmatch(line, -1) {
				if m[1] == "Width" {
					continue
				}
				for _, n := range num.FindAllStringSubmatch(m[2], -1) {
					if v, _ := strconv.ParseFloat(n[1], 32); v < MinTarget {
						t.Errorf("%s:%d: a button %s of %s is under %d; draw it with theme.Hit", path, i+1, strings.ToLower(m[1]), n[1], MinTarget)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
