package timeline

import (
	"errors"
	"net/url"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// empty is the first-run screen: no subscriptions, a window wide enough for the row of packs.
func empty(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{now: now, patches: map[int64][]client.EntryPatch{}}
	s := New(language, f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	f.settle(t, tt)
	return s, f, tt
}

func TestPacksTable(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range packs {
		if len(p.feeds) == 0 || len(p.feeds) > 6 {
			t.Errorf("pack %s has %d feeds", p.id, len(p.feeds))
		}
		for _, address := range p.feeds {
			u, err := url.Parse(address)
			if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
				t.Errorf("pack %s: %q is not an http(s) address", p.id, address)
			}
			if seen[address] {
				t.Errorf("%q is in more than one place", address)
			}
			seen[address] = true
		}
	}
	for _, language := range []string{"en", "zh-Hans"} {
		s := New(language, (&fake{now: now}).deps())
		for _, p := range packs {
			if text := s.text.Packs[p.id]; text.Name == "" || text.Hint == "" {
				t.Errorf("%s: no name or description for pack %s", language, p.id)
			}
		}
	}
}

func TestPacksShown(t *testing.T) {
	for language, label := range map[string]string{"en": "Or start with a topic", "zh-Hans": "或者从一个主题开始"} {
		s, f, tt := empty(t, language)
		if !tt.HasText(label) {
			t.Fatalf("%s: no %q in %v", language, label, tt.Texts())
		}
		for _, p := range packs {
			if !tt.HasText(s.text.Packs[p.id].Name) {
				t.Fatalf("%s: no pack %s in %v", language, p.id, tt.Texts())
			}
		}
		if len(f.adds) != 0 {
			t.Fatalf("%s: feeds contacted before a pack is chosen: %v", language, f.adds)
		}
	}
}

func TestPacksHiddenWithSubscriptions(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.settle(t, tt)
	if tt.HasText(s.text.PacksLabel) {
		t.Fatalf("packs shown with subscriptions: %v", tt.Texts())
	}
}

func TestPackSubscribes(t *testing.T) {
	s, f, tt := empty(t, "en")
	if err := tt.Click("Technology"); err != nil {
		t.Fatal(err)
	}
	if s.packing != "technology" || !tt.HasText("Adding 5 subscriptions…") {
		t.Fatalf("packing %q, texts %v", s.packing, tt.Texts())
	}
	// Another pack chosen while one is being added does nothing.
	s.subscribePack(packs[1])
	f.settle(t, tt)
	if !slices.Equal(f.adds, packs[0].feeds) {
		t.Fatalf("added %v", f.adds)
	}
	if s.packing != "" || len(s.sources) != len(packs[0].feeds) {
		t.Fatalf("packing %q, sources %d", s.packing, len(s.sources))
	}
	if !tt.HasText("Added 5 subscriptions from Technology") || s.text.PacksLabel != "Or start with a topic" || tt.HasText(s.text.PacksLabel) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestPackPartialFailure(t *testing.T) {
	_, f, tt := empty(t, "en")
	f.addErrs = map[string]error{
		packs[0].feeds[1]: &client.APIError{Status: 422, Code: "invalid_source"},
		packs[0].feeds[2]: errors.New("timeout"),
	}
	if err := tt.Click("Technology"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(f.adds) != len(packs[0].feeds) {
		t.Fatalf("one failure stopped the others: %v", f.adds)
	}
	if !tt.HasText("Added 3 subscriptions from Technology, couldn’t add theverge.com, feeds.arstechnica.com") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestPackSkipsFollowed(t *testing.T) {
	s, f, tt := empty(t, "en")
	f.addErrs = map[string]error{packs[1].feeds[0]: &client.APIError{Status: 409, Code: "source_exists"}}
	if err := tt.Click("Science"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !tt.HasText("Added 3 subscriptions from Science") || len(s.sources) != 3 {
		t.Fatalf("sources %d, texts %v", len(s.sources), tt.Texts())
	}
}

func TestPackAllFail(t *testing.T) {
	s, f, tt := empty(t, "en")
	f.addErrs = map[string]error{}
	for _, address := range packs[2].feeds {
		f.addErrs[address] = errors.New("offline")
	}
	if err := tt.Click("Design"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(s.sources) != 0 || !tt.HasText(s.text.PacksLabel) || !tt.HasText("Couldn’t add smashingmagazine.com, alistapart.com, css-tricks.com, designboom.com") {
		t.Fatalf("sources %d, texts %v", len(s.sources), tt.Texts())
	}
}

func TestPackExpired(t *testing.T) {
	_, f, tt := empty(t, "en")
	f.addErrs = map[string]error{packs[3].feeds[0]: client.ErrUnauthorized}
	if err := tt.Click("News"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if f.expired != 1 || len(f.adds) != 1 {
		t.Fatalf("expired %d, adds %v", f.expired, f.adds)
	}
}
