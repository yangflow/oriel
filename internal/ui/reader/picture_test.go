package reader

import (
	"image"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/pictures"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// pictureSetup opens an article of body in a window width wide; unit is how many pixels one design DIP is.
func pictureSetup(t *testing.T, body string, width int) (*Screen, *fake, *ui.Tester, *float32) {
	t.Helper()
	a := client.Article{Entry: entry(), ExtractedHTML: body}
	a.ExtractStatus = "ok"
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("en", f.deps())
	s.Use(f)
	unit := new(float32)
	tt := ui.NewTester(func(c *ui.Context) { *unit = theme.Px(c.Theme(), 1); s.View(c, f.nav) }, width, 800)
	s.Open(a.Entry)
	tt.Frame()
	return s, f, tt, unit
}

// loadText runs the work queued so far: the article loads, and the pictures are asked for but their work is left.
func loadText(f *fake, tt *ui.Tester) {
	work := f.queue[0]
	f.queue = f.queue[1:]
	work()()
	for range 3 {
		tt.Frame()
	}
}

func near(t *testing.T, what string, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 1.5 {
		t.Errorf("%s is %.1f, want %.1f", what, got, want)
	}
}

const sized = `<p>Before.</p><img src="/a.png" alt="Chart" width="600" height="300"><p>After.</p>`

func TestPictureFailedKeepsRoomAndSaysSo(t *testing.T) {
	s, f, tt, unit := pictureSetup(t, sized, 1100)
	f.imageErr = 100
	loadText(f, tt)
	loading, ok := tt.Find("Chart")
	if !ok {
		t.Fatalf("no loading box in %v", tt.Texts())
	}
	after, _ := tt.Find("After.")
	f.settle(tt)
	if p := s.pics.Peek("https://go.dev/a.png"); p == nil || !p.Failed {
		t.Fatalf("the picture did not fail: %+v", p)
	}
	box, ok := tt.Find("Chart. This picture can’t be shown")
	if !ok {
		t.Fatalf("no failed box in %v", tt.Texts())
	}
	near(t, "failed height", box.H, loading.H)
	moved, _ := tt.Find("After.")
	near(t, "the text under it", moved.Y, after.Y)
	expect(t, tt, "Chart", "This picture can’t be shown", "Try again")
	if box.H < 140**unit {
		t.Fatalf("the test box is too low to show the column: %v", box.H)
	}
}

func TestPictureFailedWithoutAlt(t *testing.T) {
	_, f, tt, _ := pictureSetup(t, `<p>Before.</p><img src="/a.png" width="600" height="300">`, 1100)
	f.imageErr = 100
	loadText(f, tt)
	f.settle(tt)
	if _, ok := tt.Find("Picture. It can’t be shown"); !ok {
		t.Fatalf("no failed box in %v", tt.Texts())
	}
	expect(t, tt, "This picture can’t be shown", "Try again")
	if n := strings.Count(strings.Join(tt.Texts(), "|"), "This picture can’t be shown"); n != 1 {
		t.Fatalf("the line shows %d times", n)
	}
}

func TestPictureFailedLowBoxIsOneRow(t *testing.T) {
	_, f, tt, unit := pictureSetup(t, `<img src="/a.png" alt="Banner" width="600" height="60">`, 1100)
	f.imageErr = 100
	loadText(f, tt)
	f.settle(tt)
	box, ok := tt.Find("Banner. This picture can’t be shown")
	if !ok {
		t.Fatalf("no failed box in %v", tt.Texts())
	}
	if box.H >= 140**unit {
		t.Fatalf("height %v is not a strip", box.H)
	}
	// The declared 600 wide is in pixels of the picture, which the text scale does not change, so the box is 60 high.
	near(t, "height", box.H, 60)
	expect(t, tt, "Banner", "Try again")
	for _, text := range tt.Texts() {
		if text == "This picture can’t be shown" {
			t.Fatal("the strip has a second line")
		}
	}
}

func TestPictureFailedTinyIsLeftOut(t *testing.T) {
	_, f, tt, _ := pictureSetup(t, `<p>Before.</p><img src="/a.png" alt="Dot" width="16" height="16"><p>After.</p>`, 1100)
	f.imageErr = 100
	loadText(f, tt)
	f.settle(tt)
	absent(t, tt, "Try again", "This picture can’t be shown")
	if _, ok := tt.Find("Dot"); ok {
		t.Fatal("a box stays for a picture declared under 48")
	}
}

func TestPictureTryAgain(t *testing.T) {
	s, f, tt, _ := pictureSetup(t, sized, 1100)
	f.imageErr = 100
	loadText(f, tt)
	for range 2 * pictures.Tries {
		f.settle(tt)
		f.now = f.now.Add(pictures.Retry)
		tt.Frame()
	}
	f.settle(tt)
	if len(f.pictures) != pictures.Tries {
		t.Fatalf("asked %d times, want %d", len(f.pictures), pictures.Tries)
	}
	f.imageErr = 0
	if err := tt.Click("Try again"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.pictures) != pictures.Tries+1 {
		t.Fatalf("Try again asked %d times in all, want %d", len(f.pictures), pictures.Tries+1)
	}
	if p := s.pics.Peek("https://go.dev/a.png"); p == nil || p.Failed || p.Bitmap == nil {
		t.Fatalf("the picture did not load: %+v", p)
	}
	absent(t, tt, "Try again", "This picture can’t be shown")
}

func TestPictureWithoutSizeIsAFlat180(t *testing.T) {
	const body = `<p>Before.</p><img src="/a.png" alt="Chart"><p>After.</p>`
	for _, tc := range []struct {
		name  string
		width int
		want  func(box ui.Rect, unit float32) float32
	}{
		{"column", 1100, func(_ ui.Rect, unit float32) float32 { return 180 * unit }},
		{"narrow column", 340, func(box ui.Rect, _ float32) float32 { return box.W * 9 / 16 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, f, tt, unit := pictureSetup(t, body, tc.width)
			loadText(f, tt)
			tt.Frame()
			box, ok := tt.Find("Chart")
			if !ok {
				t.Fatalf("no box in %v", tt.Texts())
			}
			if tc.name == "narrow column" && box.W >= 320**unit {
				t.Fatalf("the column is %v wide, not narrow", box.W)
			}
			near(t, "height", box.H, tc.want(box, *unit))
		})
	}
}

func TestPictureSettlesAtItsRealRatio(t *testing.T) {
	// Declared 300 by 300, really 2000 by 1000: whole, at the width of the column, at 2:1.
	_, f, tt, unit := pictureSetup(t, `<p>Before.</p><img src="/a.png" alt="Chart" width="300" height="300">`, 1100)
	f.imageAt = map[string]image.Point{"https://go.dev/a.png": image.Pt(2000, 1000)}
	loadText(f, tt)
	declared, _ := tt.Find("Chart")
	f.settle(tt)
	for range 20 {
		tt.Advance(20 * 1e6)
	}
	box, ok := tt.Find("Chart")
	if !ok {
		t.Fatalf("no picture in %v", tt.Texts())
	}
	near(t, "ratio", box.W/box.H, 2)
	if box.W <= declared.W {
		t.Fatalf("width %v is not more than the declared %v", box.W, declared.W)
	}
	if box.W > 680**unit {
		t.Fatalf("the picture is %v wide, more than the column", box.W)
	}
}

func TestPictureAboveTheViewSettlesWithoutMovingTheText(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<img src="/a.png" alt="Chart">`)
	for i := range 60 {
		body.WriteString("<p>Paragraph ")
		body.WriteString(strings.Repeat("x", i%7+1))
		body.WriteString(" of a long article that goes on for a few lines so that the page is much taller than the window.</p>")
	}
	s, f, tt, _ := pictureSetup(t, body.String(), 1100)
	f.imageAt = map[string]image.Point{"https://go.dev/a.png": image.Pt(2000, 1000)}
	loadText(f, tt)
	tt.Frame()
	s.scroll.Y = 900
	for range 3 {
		tt.Frame()
	}
	if s.scroll.Y != 900 {
		t.Fatalf("the test could not scroll: %v", s.scroll.Y)
	}
	read := "Paragraph xxx of a long article that goes on for a few lines so that the page is much taller than the window."
	at, ok := tt.Find(read)
	if !ok {
		t.Fatalf("no %q on screen in %v", read, tt.Texts())
	}
	f.settle(tt)
	for range 3 {
		tt.Frame()
	}
	if s.scroll.Y == 900 {
		t.Fatal("the offset did not move with the picture")
	}
	now, ok := tt.Find(read)
	if !ok {
		t.Fatalf("the text left the screen: scroll %v", s.scroll.Y)
	}
	near(t, "the paragraph read", now.Y, at.Y)
}

func TestPictureAboveTheViewSettlesWithTheFindBarOpen(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<img src="/a.png" alt="Chart">`)
	for i := range 60 {
		body.WriteString("<p>Paragraph ")
		body.WriteString(strings.Repeat("x", i%7+1))
		body.WriteString(" of a long article that goes on for a few lines so that the page is much taller than the window.</p>")
	}
	s, f, tt, _ := pictureSetup(t, body.String(), 1100)
	f.imageAt = map[string]image.Point{"https://go.dev/a.png": image.Pt(2000, 1000)}
	loadText(f, tt)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	if !s.find.open {
		t.Fatal("find did not open")
	}
	s.scroll.Y = 900
	for range 3 {
		tt.Frame()
	}
	read := "Paragraph xxx of a long article that goes on for a few lines so that the page is much taller than the window."
	at, ok := tt.Find(read)
	if !ok {
		t.Fatalf("no %q on screen in %v", read, tt.Texts())
	}
	f.settle(tt)
	for range 3 {
		tt.Frame()
	}
	now, ok := tt.Find(read)
	if !ok {
		t.Fatalf("the text left the screen: scroll %v", s.scroll.Y)
	}
	near(t, "the paragraph read", now.Y, at.Y)
}
