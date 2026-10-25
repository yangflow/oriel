package timeline

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A subscription chooses where its entries open: follow the kind (articles in the reader, the rest in the browser),
// always in the reader, or always in the browser. Click, Enter and the context menu agree, and Shift turns Enter and
// O around for once.
func TestOpenTarget(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.sources[0].OpenTarget = "browser" // Go Blog: articles
	s.sources[1].OpenTarget = "reader"  // Talks: a video
	article := "Go Blog, Go 1.27 is released, 5 min ago, Unread"

	// A click on an article of a browser subscription opens the browser and marks it read.
	if err := tt.Click(article); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.reading != 0 || !slices.Equal(tt.OpenedURLs(), []string{"https://go.dev/blog/go1.27"}) || !s.entries[0].IsRead {
		t.Fatalf("a click: reading %d, opened %v, read %v", s.reading, tt.OpenedURLs(), s.entries[0].IsRead)
	}
	tt.Key(0, ui.KeyEnter)
	if s.reading != 0 || len(tt.OpenedURLs()) != 2 {
		t.Fatalf("Enter: reading %d, opened %v", s.reading, tt.OpenedURLs())
	}
	// Shift+Enter and Shift+O open it in the reader, once.
	tt.Key(ui.Shift, ui.KeyEnter)
	if s.reading != 8 {
		t.Fatalf("Shift+Enter reads %d", s.reading)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Shift, ui.KeyO)
	if s.reading != 8 {
		t.Fatalf("Shift+O reads %d", s.reading)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEnter)
	if s.reading != 0 || len(tt.OpenedURLs()) != 3 {
		t.Fatalf("the setting stays: reading %d, opened %v", s.reading, tt.OpenedURLs())
	}

	// A video of a reader subscription opens in the reader, and Shift+Enter in the browser.
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyEnter)
	if s.reading != 7 || len(tt.OpenedURLs()) != 3 {
		t.Fatalf("a video in the reader: reading %d, opened %v", s.reading, tt.OpenedURLs())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Shift, ui.KeyEnter)
	if s.reading != 0 || len(tt.OpenedURLs()) != 4 {
		t.Fatalf("Shift+Enter on a video: reading %d, opened %v", s.reading, tt.OpenedURLs())
	}

	// The context menu offers both, whatever the setting.
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Open in reader"); err != nil {
		t.Fatal(err)
	}
	if s.reading != 5 {
		t.Fatalf("the menu reads %d", s.reading)
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Open in browser"); err != nil {
		t.Fatal(err)
	}
	if s.reading != 0 || len(tt.OpenedURLs()) != 5 {
		t.Fatalf("the menu browses: reading %d, opened %v", s.reading, tt.OpenedURLs())
	}
}

// Stepping in the reader skips the entries that open in the browser.
func TestOpenTargetSteps(t *testing.T) {
	s, _, _ := setup(t, "en")
	s.sources[0].OpenTarget = "browser"
	s.sources[1].OpenTarget = "reader"
	s.read(1) // the video
	if i := s.article(1); i != -1 {
		t.Fatalf("the next in the reader is %d", i)
	}
	if i := s.article(-1); i != -1 {
		t.Fatalf("the previous in the reader is %d", i)
	}
	s.sources[0].OpenTarget = "follow"
	if i := s.article(-1); i != 0 {
		t.Fatalf("the previous in the reader is %d", i)
	}
}
