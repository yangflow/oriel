package reader

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// TestToolbarOrder shows the toolbar keeps one order: back, previous, next, summary, translate, star, read, open,
// copy link and more.
func TestToolbarOrder(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	prev := float32(-1)
	for _, name := range []string{"Back to All", "Previous entry", "Next entry", "Summarize", "Translate", "Star",
		"Mark as unread", "Open in browser", "Copy link", "More"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("no %q in %v", name, tt.Texts())
		}
		if r.X <= prev {
			t.Errorf("%q is at %v, not after the one before it at %v", name, r.X, prev)
		}
		prev = r.X
	}
}

// TestTranslateDisabledInTargetLanguage shows an entry already in the language of the translations keeps its
// Translate button, disabled, named and explained by the reason; one in another language, or in none known, has it
// enabled.
func TestTranslateDisabledInTargetLanguage(t *testing.T) {
	for _, tc := range []struct {
		language string
		done     bool
	}{{"zh-Hans", true}, {"en", false}, {"zh-Hant", false}, {"", false}} {
		art := full()
		art.Language = tc.language
		_, f, tt := setup(t, "en", art)
		f.ai = client.AIResults{TargetLanguage: "zh-Hans"}
		f.settle(tt)
		// The visible label is Translate either way; the reason is only the accessible name (and the tooltip).
		done, plain := slices.Contains(tt.Texts(), "Already in Simplified Chinese"), slices.Contains(tt.Texts(), "Translate")
		if done != tc.done || !plain {
			t.Errorf("language %q: name is the reason = %v, label is Translate = %v, want %v and true", tc.language, done,
				plain, tc.done)
		}
		expect(t, tt, "Summarize")
		if tc.done {
			if err := tt.Click("Translate"); err == nil && len(f.queue) > 0 {
				t.Errorf("the disabled button asked for a translation")
			}
		}
	}
}

// TestTranslateDisabledSaysWhy shows the reason is in the tooltip, in both languages, and the other buttons that
// do not apply stay in place too.
func TestTranslateDisabledSaysWhy(t *testing.T) {
	for _, tc := range []struct{ ui, target, want string }{
		{"en", "zh-Hans", "Already in Simplified Chinese"},
		{"zh-Hans", "zh-Hans", "已经是简体中文"},
		{"en", "en", "Already in English"},
	} {
		art := full()
		art.Language = tc.target
		_, f, tt := setup(t, tc.ui, art)
		f.ai = client.AIResults{TargetLanguage: tc.target}
		f.settle(tt)
		label := map[string]string{"en": "Translate", "zh-Hans": "翻译"}[tc.ui]
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s: no %q in %v", tc.ui, label, tt.Texts())
		}
		if n := count(tt.Texts(), tc.want); n != 1 {
			t.Errorf("%s: the reason %q shows %d times before any hover, want once, as the accessible name", tc.ui, tc.want, n)
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Advance(time.Second)
		if count(tt.Texts(), tc.want) != 2 {
			t.Errorf("%s: the tooltip %q does not show on hover: %v", tc.ui, tc.want, tt.Texts())
		}
	}
}

// TestAIOffKeepsButtonsEnabled shows summary and translate stay clickable while AI is not set up; they lead to Settings.
func TestAIOffKeepsButtonsEnabled(t *testing.T) {
	art := full()
	art.Language = "fr"
	_, f, tt := setup(t, "en", art)
	f.settle(tt)
	click(t, tt, "Summarize")
	f.settle(tt)
	click(t, tt, "Translate")
}

// TestToolbarOverflow shows the narrow main column moves copy link, then open and then mark as read into More, below
// widths 500, 440 and 380, and More lists them above the Markdown items.
func TestToolbarOverflow(t *testing.T) {
	buttons := []string{"Mark as unread", "Open in browser", "Copy link"}
	for _, tc := range []struct {
		column float32
		moved  int
	}{{600, 0}, {500, 0}, {499, 1}, {440, 1}, {439, 2}, {380, 2}, {379, 3}} {
		_, f, tt := setup(t, "en", full())
		f.nav.Column = tc.column
		f.settle(tt)
		tt.Frame()
		for i, name := range buttons {
			_, shown := tt.Find(name)
			if want := i < len(buttons)-tc.moved; shown != want {
				t.Errorf("column %v: %q shows = %v, want %v", tc.column, name, shown, want)
			}
		}
		click(t, tt, "More")
		want := slices.Clone(buttons[len(buttons)-tc.moved:])
		if tc.moved > 0 {
			want = append(want, "-")
		}
		want = append(want, "Copy as Markdown", "Save as Markdown…")
		if got := tt.Menu(); !slices.Equal(got, want) {
			t.Errorf("column %v: More lists %v, want %v", tc.column, got, want)
		}
	}
}

// TestOverflowMoved shows a moved button acts from the menu.
func TestOverflowMoved(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.nav.Column = 300
	f.settle(tt)
	tt.Frame()
	click(t, tt, "More")
	if err := tt.ChooseMenuItem("Copy link"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != full().URL {
		t.Errorf("clipboard %q, want the link", got)
	}
}

// TestOverflowDefaultsToWindow shows the window width decides when the timeline names no column.
func TestOverflowDefaultsToWindow(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	tt.SetSize(430, 800)
	tt.Frame()
	if _, ok := tt.Find("Copy link"); ok {
		t.Errorf("Copy link shows in a 430 wide window: %v", tt.Texts())
	}
}

// TestBarEdge shows the bar has no hairline at the top of the article and fades one in once it is scrolled.
func TestBarEdge(t *testing.T) {
	s, _, tt := openLong(t, "en", long(nil))
	row := func() ui.Color {
		img := tt.Image()
		o := img.PixOffset(8, int(theme.Bar))
		return ui.RGBA(img.Pix[o], img.Pix[o+1], img.Pix[o+2], 1)
	}
	top := row()
	scrollDown(t, s, tt)
	tt.Advance(300 * time.Millisecond)
	if scrolled := row(); scrolled == top {
		t.Errorf("no hairline once scrolled: %v the same as at the top", top)
	}
	tt.Scroll(550, 400, 0, -3000)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if again := row(); again != top {
		t.Errorf("the hairline stays at the top: %v, want %v", again, top)
	}
}

func count(texts []string, want string) int {
	n := 0
	for _, text := range texts {
		if text == want {
			n++
		}
	}
	return n
}

// The toolbar lines up with the article column in every window width, with the window controls over it or not: the
// back button begins 28 inside the column's left edge (its icon on the text's 32) and More ends 32 inside the right
// one. The controls move the row only where the column leaves them no room.
func TestToolbarAlignsWithColumn(t *testing.T) {
	for _, tc := range []struct {
		width    int
		controls bool
	}{{1400, false}, {1400, true}, {1000, true}, {800, true}, {700, true}, {640, false}} {
		a := full()
		f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true, Controls: tc.controls}}
		s := New("en", f.deps())
		s.Use(f)
		var column float32
		tt := ui.NewTester(func(c *ui.Context) { column = s.column(c.Theme()); s.View(c, f.nav) }, tc.width, 900)
		s.Open(a.Entry)
		tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78, Right: 0})
		f.settle(tt)
		tt.Frame()
		back, ok1 := tt.Find("Back to All")
		more, ok2 := tt.Find("More")
		source, ok3 := tt.Find(a.Entry.SourceTitle)
		if !ok1 || !ok2 || !ok3 {
			t.Fatalf("width %d: back %v, more %v, source %v in %v", tc.width, ok1, ok2, ok3, tt.Texts())
		}
		w := float32(tc.width)
		col := min(w, column)
		left, right := (w-col)/2+28, (w-col)/2+col-32
		narrow := tc.width < 760
		if narrow {
			left, right = (w-col)/2+12, (w-col)/2+col-16
		}
		if tc.controls {
			left = max(left, 78)
		}
		if got := back.X; got < left-0.5 || got > left+0.5 {
			t.Errorf("width %d controls %v: Back begins at %v, want %v", tc.width, tc.controls, got, left)
		}
		if got := more.X + more.W; got < right-0.5 || got > right+0.5 {
			t.Errorf("width %d controls %v: More ends at %v, want %v", tc.width, tc.controls, got, right)
		}
		if !tc.controls && !narrow && back.X+4 > source.X {
			t.Errorf("width %d: Back icon at %v is right of the source line at %v", tc.width, back.X+4, source.X)
		}
	}
}
