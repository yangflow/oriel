package theme

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// toastTip shows a toast titled title in a window of width DIPs and reports whether resting the pointer on it shows
// the title in a tooltip as well. Every size comes through Px, so the answer does not depend on the platform's font.
func toastTip(t *testing.T, title string, width int) bool {
	t.Helper()
	added := false
	tt := ui.NewTester(func(c *ui.Context) {
		c.Theme().FontSize = 14
		if !added {
			added = true
			c.AddToast(ui.Toast{ID: "t", Title: title, Timeout: -1})
		}
		Toasts(c)
	}, width, 400)
	tt.Frame()
	tt.Frame()
	count := func() int {
		n := 0
		for _, text := range tt.Texts() {
			if text == title {
				n++
			}
		}
		return n
	}
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("no toast titled %q in %v", title, tt.Texts())
	}
	before := count()
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	return count() > before
}

func TestToastTitleTooltipOnlyWhenCut(t *testing.T) {
	long := strings.Repeat("A long subscription name that goes on and on ", 8)
	if !toastTip(t, long, 420) {
		t.Error("a toast title cut after two lines has no tooltip")
	}
	if toastTip(t, "Fetched 3 entries", 420) {
		t.Error("a toast title that fits has a tooltip")
	}
	two := "Fetched 12 entries from a rather long subscription name that takes two lines"
	if toastTip(t, two, 420) {
		t.Error("a toast title that fits in two lines has a tooltip")
	}
}
