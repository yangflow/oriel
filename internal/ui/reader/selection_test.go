package reader

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// words are the labels of the selection card in one language.
type words struct {
	language, translate, explain, copyLabel, copied, retry, settings, aiOff, less, ready string
}

var selectionWords = []words{
	{"en", "Translate selection", "Explain selection", "Copy", "Copied", "Try again", "Open settings",
		"AI isn’t set up", "Select less text", "Explanation ready"},
	{"zh-Hans", "翻译所选", "解释所选", "复制", "已复制", "重试", "打开设置", "还没配置 AI", "请少选一些文字", "解释已生成"},
}

// tall opens the article in a window tall enough to keep the text clear of the toolbar over its foot.
func tall(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, full())
	tt.SetSize(1100, 1600)
	tt.Frame()
	return s, f, tt
}

// selectText drags over the text with label, as far as its middle, and returns what the drag takes.
func selectText(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no %q in %v", label, tt.Texts())
	}
	y := r.Y + r.H/2
	tt.Press(r.X+1, y)
	tt.Move(r.X+r.W-1, y)
	tt.Release(r.X+r.W-1, y)
	tt.Frame()
}

// askAbout selects "Faster builds" and chooses item from the menu of the body.
func askAbout(t *testing.T, tt *ui.Tester, item string) {
	t.Helper()
	selectText(t, tt, "Faster builds")
	r, _ := tt.Find("Faster builds")
	tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	if err := tt.ChooseMenuItem(item); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestSelectionMenu(t *testing.T) {
	for _, w := range selectionWords {
		_, f, tt := tall(t, w.language)
		f.settle(tt)
		r, _ := tt.Find("Faster builds")
		tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
		if menu := tt.Menu(); slices.Contains(menu, w.translate) || slices.Contains(menu, w.explain) {
			t.Errorf("%s: %v without a selection", w.language, menu)
		}
		tt.CloseMenu()
		selectText(t, tt, "Faster builds")
		tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
		menu := tt.Menu()
		if !slices.Contains(menu, w.translate) || !slices.Contains(menu, w.explain) {
			t.Errorf("%s: %v with a selection", w.language, menu)
		}
		if i, j := slices.Index(menu, w.translate), slices.Index(menu, w.explain); i != 0 || j != 1 {
			t.Errorf("%s: %v, want the two first", w.language, menu)
		}
		tt.CloseMenu()
		if len(f.selected) != 0 {
			t.Errorf("%s: asked %v before an item was chosen", w.language, f.selected)
		}
	}
}

func TestSelectionAnswer(t *testing.T) {
	for _, w := range selectionWords {
		s, f, tt := tall(t, w.language)
		f.settle(tt)
		f.selReply = "  An answer.  "
		askAbout(t, tt, w.translate)
		expect(t, tt, "“Faster builds”")
		if len(f.queue) != 1 {
			t.Fatalf("%s: %d jobs queued", w.language, len(f.queue))
		}
		f.settle(tt)
		if len(f.selected) != 1 || !strings.HasPrefix(f.selected[0], "translate: ") || !strings.Contains(f.selected[0], "Faster") ||
			f.selTitles[0] != "Go 1.27 is released" {
			t.Fatalf("%s: asked %v with %v", w.language, f.selected, f.selTitles)
		}
		expect(t, tt, "An answer.", w.copyLabel)
		if _, ok := tt.Find(w.translate); !ok {
			t.Errorf("%s: the card is not named", w.language)
		}
		click(t, tt, w.copyLabel)
		if tt.Clipboard() != "An answer." || !slices.Contains(tt.Announcements(), w.copied) {
			t.Errorf("%s: clipboard %q, announced %v", w.language, tt.Clipboard(), tt.Announcements())
		}
		click(t, tt, s.text.Close)
		absent(t, tt, "An answer.")
		if len(f.calls) != 0 {
			t.Errorf("%s: calls %v", w.language, f.calls)
		}

		askAbout(t, tt, w.explain)
		f.settle(tt)
		if !strings.HasPrefix(f.selected[1], "explain: ") || !slices.Contains(tt.Announcements(), w.ready) {
			t.Errorf("%s: asked %v, announced %v", w.language, f.selected, tt.Announcements())
		}
	}
}

func TestSelectionFailed(t *testing.T) {
	for _, w := range selectionWords {
		s, f, tt := tall(t, w.language)
		f.settle(tt)
		f.selErr = &client.APIError{Status: 502, Code: "ai_rate_limited"}
		askAbout(t, tt, w.translate)
		f.settle(tt)
		expect(t, tt, strings.ReplaceAll(s.text.SelectionFailed, "{reason}", s.reason("ai_rate_limited")), w.retry)
		f.selErr, f.selReply = nil, "Second try."
		click(t, tt, w.retry)
		absent(t, tt, w.retry)
		f.settle(tt)
		expect(t, tt, "Second try.")
		if len(f.selected) != 2 || f.selected[0] != f.selected[1] {
			t.Errorf("%s: asked %v", w.language, f.selected)
		}
	}
}

func TestSelectionAIOff(t *testing.T) {
	for _, w := range selectionWords {
		s, f, tt := tall(t, w.language)
		f.settle(tt)
		f.selErr = &client.APIError{Status: 409, Code: "ai_not_configured"}
		askAbout(t, tt, w.explain)
		f.settle(tt)
		expect(t, tt, w.aiOff, s.text.SelectionAIOff, w.settings)
		click(t, tt, w.settings)
		if !slices.Equal(f.calls, []string{"settings"}) {
			t.Errorf("%s: calls %v", w.language, f.calls)
		}
	}
}

// A selection over the limit is told so, without a request.
func TestSelectionTooLong(t *testing.T) {
	for _, w := range selectionWords {
		s, f, tt := tall(t, w.language)
		f.settle(tt)
		s.ask("translate", strings.Repeat("字", maxSelection+1))
		tt.Frame()
		expect(t, tt, w.less, s.text.SelectLessHint)
		if len(f.queue) != 0 || len(f.selected) != 0 {
			t.Errorf("%s: a request went out: %d queued, %v", w.language, len(f.queue), f.selected)
		}
		s.ask("translate", strings.Repeat("字", maxSelection))
		f.settle(tt)
		absent(t, tt, w.less)
		if len(f.selected) != 1 {
			t.Errorf("%s: asked %d times at the limit", w.language, len(f.selected))
		}
	}
}

func TestSelectionEscape(t *testing.T) {
	_, f, tt := tall(t, "en")
	f.settle(tt)
	f.selReply = "An answer."
	askAbout(t, tt, "Translate selection")
	f.settle(tt)
	tt.Key(0, ui.KeyEscape)
	absent(t, tt, "An answer.")
	if len(f.calls) != 0 {
		t.Fatalf("calls %v: Escape left the reader with the card open", f.calls)
	}
	tt.Key(0, ui.KeyEscape)
	if !slices.Equal(f.calls, []string{"back"}) {
		t.Fatalf("calls %v", f.calls)
	}
}

// The card goes when another entry opens, and an answer still to come is dropped.
func TestSelectionStale(t *testing.T) {
	s, f, tt := tall(t, "en")
	f.settle(tt)
	f.selReply = "An answer."
	askAbout(t, tt, "Translate selection")
	expect(t, tt, "“Faster builds”")
	other := full()
	other.Entry.ID = 9
	other.Entry.Title = "Another"
	f.article, f.nav.Entry = other, other.Entry
	s.Open(other.Entry)
	tt.Frame()
	absent(t, tt, "“Faster builds”")
	f.settle(tt)
	absent(t, tt, "An answer.")

	// Closing the card drops its answer, and asking again drops the one before.
	askAbout(t, tt, "Translate selection")
	click(t, tt, "Close")
	askAbout(t, tt, "Explain selection")
	f.selReply = "The second."
	f.settle(tt)
	absent(t, tt, "An answer.")
	// The three requests were made; only the last answer shows.
	if len(f.selected) != 3 {
		t.Fatalf("asked %v", f.selected)
	}
	expect(t, tt, "The second.")
}
