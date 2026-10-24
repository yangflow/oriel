package reader

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// long is an article that scrolls, whatever the window or the text size.
func long(progress *float64) client.Article {
	var body strings.Builder
	for i := range 80 {
		body.WriteString("<p>Paragraph ")
		body.WriteString(strings.Repeat("x", i%7+1))
		body.WriteString(" of a long article that goes on for a few lines so that the page is much taller than the window it is read in.</p>")
	}
	a := client.Article{Entry: entry(), ExtractedHTML: body.String()}
	a.ExtractStatus, a.ReadProgress = "ok", progress
	return a
}

// openLong opens a in a reader whose window also draws the toasts, and settles the loading and the layout.
func openLong(t *testing.T, language string, a client.Article) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New(language, f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav); theme.Toasts(c) }, 1100, 800)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	for range 4 {
		tt.Frame()
	}
	return s, f, tt
}

func at(v float64) *float64 { return &v }

func TestResumesWhereLeftOff(t *testing.T) {
	a := long(at(0.5))
	s, f, tt := openLong(t, "en", a)
	if s.scroll.MaxY <= 0 {
		t.Fatalf("the article does not scroll: %+v", s.scroll)
	}
	if got := float64(s.scroll.Y / s.scroll.MaxY); math.Abs(got-0.5) > 0.02 {
		t.Fatalf("restored at %.3f, want 0.5", got)
	}
	expect(t, tt, "Back where you left off")
	f.wait(tt, 3*saveAfter)
	if len(f.saved) != 0 {
		t.Fatalf("restoring stored %v", f.saved)
	}
}

func TestResumeInChinese(t *testing.T) {
	_, _, tt := openLong(t, "zh-Hans", long(at(0.3)))
	expect(t, tt, "回到上次读到的位置")
}

func TestOpensAtTopUnlessInRange(t *testing.T) {
	for name, progress := range map[string]*float64{"unset": nil, "near top": at(0.01), "zero": at(0), "finished": at(0.95), "done": at(1)} {
		s, f, tt := openLong(t, "en", long(progress))
		if s.scroll.Y != 0 {
			t.Errorf("%s: opens at %v", name, s.scroll.Y)
		}
		absent(t, tt, "Back where you left off")
		f.wait(tt, 3*saveAfter)
		if len(f.saved) != 0 {
			t.Errorf("%s: stored %v", name, f.saved)
		}
	}
}

func TestResumesAfterFullTextReplacesFeed(t *testing.T) {
	a := long(at(0.5))
	feed := a
	feed.ExtractStatus, feed.ExtractedHTML, feed.ContentHTML = "pending", "", a.ExtractedHTML
	f := &fake{now: now, article: feed, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("en", f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav); theme.Toasts(c) }, 1100, 800)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	for range 4 {
		tt.Frame()
	}
	if s.scroll.Y != 0 {
		t.Fatalf("restored in the text of the feed: %v", s.scroll.Y)
	}
	absent(t, tt, "Back where you left off")
	f.article = a
	f.wait(tt, pollEvery)
	f.settle(tt)
	for range 4 {
		tt.Frame()
	}
	if got := float64(s.scroll.Y / s.scroll.MaxY); math.Abs(got-0.5) > 0.02 {
		t.Fatalf("restored at %.3f, want 0.5", got)
	}
	expect(t, tt, "Back where you left off")
}

func TestNoResumeAfterScrolling(t *testing.T) {
	a := long(at(0.5))
	feed := a
	feed.ExtractStatus, feed.ExtractedHTML, feed.ContentHTML = "pending", "", a.ExtractedHTML
	f := &fake{now: now, article: feed, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("en", f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav); theme.Toasts(c) }, 1100, 800)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	scrollDown(t, s, tt)
	moved := s.scroll.Y
	f.article = a
	f.wait(tt, pollEvery)
	f.settle(tt)
	for range 4 {
		tt.Frame()
	}
	absent(t, tt, "Back where you left off")
	if s.scroll.Y != moved {
		t.Fatalf("the page moved from %v to %v", moved, s.scroll.Y)
	}
}

// scrollDown scrolls the page by 300 DIPs.
func scrollDown(t *testing.T, s *Screen, tt *ui.Tester) {
	t.Helper()
	before := s.scroll.Y
	tt.Scroll(550, 400, 0, 300)
	tt.Frame()
	if s.scroll.Y <= before {
		t.Fatal("the page does not scroll")
	}
}

func TestSavesAfterScrollingStops(t *testing.T) {
	s, f, tt := openLong(t, "en", long(nil))
	scrollDown(t, s, tt)
	f.wait(tt, saveAfter/2)
	f.settle(tt)
	if len(f.saved) != 0 {
		t.Fatalf("stored while scrolling: %v", f.saved)
	}
	scrollDown(t, s, tt) // moving again starts the wait over
	f.wait(tt, saveAfter*3/4)
	f.settle(tt)
	if len(f.saved) != 0 {
		t.Fatalf("stored before the offset stayed still: %v", f.saved)
	}
	f.wait(tt, saveAfter/2)
	f.settle(tt)
	want := float64(s.scroll.Y / s.scroll.MaxY)
	if len(f.saved) != 1 || f.saved[0].id != 8 || math.Abs(f.saved[0].progress-want) > 0.001 {
		t.Fatalf("stored %v, want one of %.3f", f.saved, want)
	}
	f.wait(tt, 5*time.Second)
	f.settle(tt)
	if len(f.saved) != 1 {
		t.Fatalf("stored again with no scrolling: %v", f.saved)
	}
}

func TestSavesOnLeaving(t *testing.T) {
	s, f, tt := openLong(t, "en", long(nil))
	scrollDown(t, s, tt)
	other := long(nil)
	other.ID = 9
	s.Open(other.Entry)
	f.settle(tt)
	if len(f.saved) != 1 || f.saved[0].id != 8 || f.saved[0].progress <= 0 {
		t.Fatalf("stored %v on switching", f.saved)
	}
	scrollDown(t, s, tt)
	s.Leave()
	f.settle(tt)
	if len(f.saved) != 2 || f.saved[1].id != 9 {
		t.Fatalf("stored %v on closing", f.saved)
	}
}

func TestKeepsThePositionStoredThisSession(t *testing.T) {
	s, f, tt := openLong(t, "en", long(nil))
	scrollDown(t, s, tt)
	s.Leave()
	f.settle(tt)
	stored := f.saved[0].progress
	// The list still holds the entry without a position; the session knows better.
	s.Open(entry())
	f.settle(tt)
	for range 4 {
		tt.Frame()
	}
	if got := float64(s.scroll.Y / s.scroll.MaxY); math.Abs(got-stored) > 0.02 {
		t.Fatalf("reopened at %.3f, want %.3f", got, stored)
	}
	expect(t, tt, "Back where you left off")
}

func TestLateRefusalOfAServerLeftIsIgnored(t *testing.T) {
	s, f, tt := openLong(t, "en", long(nil))
	var expired []error
	deps := s.deps.Expired
	s.deps.Expired = func(err error) bool { expired = append(expired, err); return deps(err) }
	scrollDown(t, s, tt)
	s.Leave() // the write is in flight
	if len(f.queue) != 1 {
		t.Fatalf("%d jobs queued", len(f.queue))
	}
	f.setErr = errExpired
	s.Use(&fake{now: now}) // another server meanwhile
	f.settle(tt)
	if len(expired) != 0 {
		t.Fatalf("the old server's refusal reached the new session: %v", expired)
	}
	if s.writing {
		t.Fatal("the write of the old server still blocks the new one")
	}
}

// both is a long full text with what the feed carries too.
func both(progress *float64) client.Article {
	a := long(progress)
	a.ContentHTML = "<p>What the feed carries.</p>"
	return a
}

func TestSwitchBetweenFullTextAndFeed(t *testing.T) {
	for _, c := range []struct{ language, full, feed, note string }{
		{"en", "Full text", "Feed", "This isn’t the full text"},
		{"zh-Hans", "全文", "原文", "这不是全文"},
	} {
		s, f, tt := openLong(t, c.language, both(nil))
		expect(t, tt, c.full, c.feed, "Paragraph ")
		// The switch is at the top of the page: read a little, then scroll back up to it.
		scrollDown(t, s, tt)
		s.scroll.Y = 0
		tt.Frame()
		if err := tt.Click(c.feed); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		expect(t, tt, "What the feed carries.")
		absent(t, tt, "Paragraph ", c.note)
		if s.scroll.Y != 0 {
			t.Fatalf("%s: the feed text opens at %v", c.language, s.scroll.Y)
		}
		f.wait(tt, 3*saveAfter)
		f.settle(tt)
		if len(f.saved) != 0 {
			t.Fatalf("%s: stored %v for the top of the full text", c.language, f.saved)
		}
		if err := tt.Click(c.full); err != nil {
			t.Fatal(err)
		}
		for range 4 {
			tt.Frame()
		}
		expect(t, tt, "Paragraph ")
		absent(t, tt, "carries.", "Back where you left off")
		if s.scroll.Y != 0 {
			t.Fatalf("%s: the full text opens at %v", c.language, s.scroll.Y)
		}
		f.wait(tt, 3*saveAfter)
		f.settle(tt)
		if len(f.saved) != 0 {
			t.Fatalf("%s: stored %v", c.language, f.saved)
		}
	}
}

func TestSwitchStoresNothingForTheFeed(t *testing.T) {
	a := both(at(0.5))
	a.ContentHTML = strings.Repeat("<p>What the feed carries, over and over, so that it too fills the window and more.</p>", 60)
	s, f, tt := openLong(t, "en", a)
	expect(t, tt, "Back where you left off")
	s.scroll.Y = 0
	tt.Frame()
	if err := tt.Click("Feed"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		tt.Frame()
	}
	f.settle(tt)
	if len(f.saved) != 1 || f.saved[0].progress != 0 {
		t.Fatalf("the top of the full text is stored once, got %v", f.saved)
	}
	if s.scroll.Y != 0 || s.scroll.MaxY <= 0 {
		t.Fatalf("the feed text is at %v of %v", s.scroll.Y, s.scroll.MaxY)
	}
	scrollDown(t, s, tt)
	f.wait(tt, 3*saveAfter)
	f.settle(tt)
	s.Leave()
	f.settle(tt)
	if len(f.saved) != 1 {
		t.Fatalf("stored %v for the feed text", f.saved)
	}
	// Another article opens on the full text again, and a long one at its own position.
	s.Open(entry())
	f.settle(tt)
	for range 4 {
		tt.Frame()
	}
	expect(t, tt, "Back where you left off", "Paragraph ")
	absent(t, tt, "carries")
}

func TestNoSwitchWithOneText(t *testing.T) {
	one := long(nil)
	one.Summary = ""
	_, f, tt := openLong(t, "en", one)
	absent(t, tt, "Feed")
	a := full()
	a.ContentHTML = "<p>What the feed carries.</p>"
	a.ExtractStatus = "failed"
	a.ExtractedHTML = ""
	_, f, tt = setup(t, "en", a)
	f.settle(tt)
	absent(t, tt, "Full text", "Feed")
}

func TestSmallMovesAreNotStored(t *testing.T) {
	s, f, tt := openLong(t, "en", long(at(0.5)))
	s.scroll.Y += s.scroll.MaxY * 0.004
	tt.Frame()
	f.wait(tt, 2*saveAfter)
	f.settle(tt)
	if len(f.saved) != 0 {
		t.Fatalf("stored %v", f.saved)
	}
}
