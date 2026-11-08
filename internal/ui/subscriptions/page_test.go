package subscriptions

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// paging6 opens the page of Bravo, the second of the six subscriptions of listing6, in a window tall enough for the
// whole page (Settings scrolls it).
func paging6(t *testing.T) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := listing6(t, "en")
	mustClick(t, tt, "Bravo")
	tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { s.Page(c) })
		s.View(c)
		theme.Toasts(c)
	}, 1000, 2400)
	f.settle(tt)
	if s.Crumb() != "Bravo" {
		t.Fatalf("crumb %q", s.Crumb())
	}
	return s, f, tt
}

// paged opens the page of setup's subscription i, after change, if any, made it so on the server, in a window tall
// enough for the whole page.
func paged(t *testing.T, language string, i int, change func(*client.Source)) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, _ := setup(t, language)
	if change != nil {
		change(&f.sources[i])
	}
	s.OpenSource(f.sources[i])
	tt := pageTester(s)
	f.settle(tt)
	return s, f, tt
}

// pageTester draws the page of s, with its dialogs and toasts over it, in a window tall enough for all of it.
func pageTester(s *Screen) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { s.Page(c) })
		s.View(c)
		theme.Toasts(c)
	}, 1000, 2800)
	tt.Frame()
	return tt
}

// TestPageShows is the page of a subscription: its head, then General, Fetching, Timeline, Summaries and
// translations and Delete, with no list under it.
func TestPageShows(t *testing.T) {
	s, _, tt := paging6(t)
	for _, text := range []string{s.text.PageGeneral, s.text.PageFetching, s.text.PageTimeline, s.text.AIGroup,
		s.text.PageDelete, s.text.Title, s.text.PauseLabel, s.text.DeleteSubscription} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText("6 subscriptions") || tt.HasText("Charlie") {
		t.Fatalf("the list shows under the page: %v", tt.Texts())
	}
	if s.Unsaved() || s.Typing() {
		t.Fatal("a page just opened has something unsaved or typed")
	}
}

// TestPageSwitchAppliesAtOnce sends a switch as soon as it changes, and only that field.
func TestPageSwitchAppliesAtOnce(t *testing.T) {
	s, f, tt := paging6(t)
	s.pg.e.paused = true // as the switch does
	tt.Frame()
	f.settle(tt)
	if len(f.patches[2]) != 1 {
		t.Fatalf("patches %v", f.patches)
	}
	if p := f.patches[2][0]; p.Paused == nil || !*p.Paused || p.Title != nil || p.HideFromAll != nil || p.Kind != nil {
		t.Fatalf("patch %+v", p)
	}
	if !f.sources[1].Paused || !s.pg.e.paused || s.Unsaved() {
		t.Fatal("the switch did not stay on")
	}
}

// TestPageSwitchRevertsOnFailure puts a refused switch back as the server has it, and says why.
func TestPageSwitchRevertsOnFailure(t *testing.T) {
	s, f, tt := paging6(t)
	f.failOn = map[int64]error{2: client.ErrUnreachable}
	s.pg.e.hide = true
	tt.Frame()
	f.settle(tt)
	tt.Frame()
	if s.pg.e.hide || f.sources[1].HideFromAll {
		t.Fatal("the refused switch stayed on")
	}
	if !tt.HasText(s.text.Unreachable) {
		t.Fatal("no word of the failure")
	}
	if len(f.patches[2]) != 1 {
		t.Fatalf("the switch was sent again: %v", f.patches[2])
	}
}

// TestPageTitleWaitsForSave keeps a typed title until its Save, which sends the title alone.
func TestPageTitleWaitsForSave(t *testing.T) {
	s, f, tt := paging6(t)
	_ = tt.Click(s.text.SaveTitle)
	if len(f.queue) != 0 {
		t.Fatal("Save title sent something before typing")
	}
	s.pg.e.title = "Bravo News"
	tt.Frame()
	if !s.Unsaved() || len(f.queue) != 0 {
		t.Fatalf("unsaved %v, queue %d", s.Unsaved(), len(f.queue))
	}
	if s.Escape() || s.Crumb() != "Bravo" {
		t.Fatal("Escape left a page with a title not saved")
	}
	mustClick(t, tt, s.text.SaveTitle)
	f.settle(tt)
	tt.Frame()
	if len(f.patches[2]) != 1 {
		t.Fatalf("patches %v", f.patches)
	}
	if p := f.patches[2][0]; p.Title == nil || *p.Title != "Bravo News" || p.Paused != nil || p.Kind != nil {
		t.Fatalf("patch %+v", p)
	}
	if s.Unsaved() || s.Crumb() != "Bravo News" || !slices.Contains(tt.Announcements(), s.text.Saved) {
		t.Fatalf("unsaved %v, crumb %q, announcements %v", s.Unsaved(), s.Crumb(), tt.Announcements())
	}
	if !s.Escape() || s.Crumb() != "" {
		t.Fatal("Escape did not go back to the list once saved")
	}
}

// TestPageGone closes the page when the subscription is no longer there, and says so.
func TestPageGone(t *testing.T) {
	s, f, tt := paging6(t)
	f.sources = slices.Delete(f.sources, 1, 2)
	tt.Frame()
	if s.pg.open || s.Crumb() != "" || !tt.HasText(s.text.Gone) {
		t.Fatalf("page %v, texts %v", s.pg.open, tt.Texts())
	}
}

// TestPageGoneOnSave closes the page when a save finds the subscription deleted elsewhere.
func TestPageGoneOnSave(t *testing.T) {
	s, f, tt := paging6(t)
	f.failOn = map[int64]error{2: &client.APIError{Status: 404, Code: "not_found"}}
	s.pg.e.paused = true
	tt.Frame()
	f.settle(tt)
	tt.Frame()
	if s.pg.open || !tt.HasText(s.text.Gone) {
		t.Fatalf("page %v, texts %v", s.pg.open, tt.Texts())
	}
}

// TestPageDelete deletes the subscription from its page: the page closes for the list, with the undo toast and not
// the word that it is gone.
func TestPageDelete(t *testing.T) {
	s, f, tt := paging6(t)
	mustClick(t, tt, s.fill(s.text.DeleteSource, "Bravo"))
	tt.Frame()
	// The page's Delete title shares the confirmation's text, so reach the button from Cancel, which has the focus.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.deleted, []int64{2}) || s.pg.open || s.Crumb() != "" {
		t.Fatalf("deleted %v, page %v", f.deleted, s.pg.open)
	}
	if !tt.HasText(s.fill(s.text.Deleted, "Bravo")) || tt.HasText(s.text.Gone) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !tt.HasText("5 subscriptions") {
		t.Fatalf("not back on the list: %v", tt.Texts())
	}
}

// TestPageOpenListCloses is the group opened again from the sidebar or the menu: the list, not the page.
func TestPageOpenListCloses(t *testing.T) {
	s, _, tt := paging6(t)
	s.OpenList()
	tt.Frame()
	if s.pg.open || !tt.HasText("6 subscriptions") {
		t.Fatalf("page %v", s.pg.open)
	}
}

// TestPageHeadStrip is the head card of a failing subscription: its strip with the pill, why with when it was tried
// and will be again, and when it last succeeded; Try again fetches it now. A removed one has its own pill and no next
// try, and a healthy one has no strip at all.
func TestPageHeadStrip(t *testing.T) {
	s, f, tt := paging6(t)
	if tt.HasText("Failed") || tt.HasText(s.text.StatusGone) {
		t.Fatalf("a healthy subscription has a strip: %v", tt.Texts())
	}
	src := &f.sources[1]
	src.LastError, src.ConsecutiveFailures = "status 503", 5
	src.LastFetchedAt = now.Add(-3 * time.Minute).UnixMilli()
	src.NextFetchAt = now.Add(27 * time.Minute).UnixMilli()
	src.LastSuccessAt = now.Add(-48 * time.Hour).UnixMilli()
	tt.Frame()
	ago := func(d time.Duration) string { return locale.Ago(s.lang, now, now.Add(-d)) }
	why := FailureReason(s.lang, *src, s.text.Blocked, s.text.BlockedPublic, s.text.FeedRemoved)
	tried := strings.NewReplacer("{t}", ago(3*time.Minute), "{next}", s.span(27*time.Minute)).Replace(s.text.StripTried)
	for _, text := range []string{"Failed 5 times", why + s.text.Separator + tried,
		strings.ReplaceAll(s.text.LastSuccess, "{t}", ago(48*time.Hour)), s.text.Retry} {
		if !slices.Contains(tt.Texts(), text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	mustClick(t, tt, s.text.Retry)
	f.settle(tt)
	if !slices.Equal(f.fetched, []int64{2}) {
		t.Fatalf("fetched %v", f.fetched)
	}
	src.LastErrorKind = "gone"
	tt.Frame()
	gone := s.text.FeedRemoved + s.text.Separator + strings.ReplaceAll(s.text.StripTriedOnly, "{t}", ago(3*time.Minute))
	if !slices.Contains(tt.Texts(), s.text.StatusGone) || !slices.Contains(tt.Texts(), gone) {
		t.Fatalf("no removed strip in %v", tt.Texts())
	}
}
