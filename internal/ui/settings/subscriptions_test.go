package settings

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// fakeSubs stands in for the Subscriptions group: crumb is the name of the page shown ("" for the list), and the
// counters say what the screen asked of it.
type fakeSubs struct {
	crumb                         string
	typing, unsaved, open, taking bool // what Typing, Unsaved, Open and Escape answer
	lists, adds, backs, escapes   int
	views                         int
}

func (p *fakeSubs) OpenList() { p.lists++; p.crumb = "" }
func (p *fakeSubs) Add()      { p.adds++ }
func (p *fakeSubs) Crumb() string {
	return p.crumb
}
func (p *fakeSubs) Back() { p.backs++; p.crumb = "" }
func (p *fakeSubs) Page(c *ui.Context) {
	if p.crumb == "" {
		ui.Text(c, "the list")
		return
	}
	ui.Text(c, "the page")
}
func (p *fakeSubs) View(*ui.Context) { p.views++ }
func (p *fakeSubs) Escape() bool {
	p.escapes++
	return p.taking
}
func (p *fakeSubs) Typing() bool  { return p.typing }
func (p *fakeSubs) Unsaved() bool { return p.unsaved }
func (p *fakeSubs) Open() bool    { return p.open }

// openSubs opens the screen on the Subscriptions group, as the menu does.
func openSubs(t *testing.T) (*Screen, *fake, *fakeSubs, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, "en", true)
	p := &fakeSubs{}
	s.deps.Subscriptions = func() Subscriptions { return p }
	s.OpenPage("subscriptions")
	tt.Frame()
	f.settle(tt)
	return s, f, p, tt
}

// TestSubscriptionsGroup is the group in the sidebar, second, with the list under its lead and Add subscription.
func TestSubscriptionsGroup(t *testing.T) {
	s, _, p, tt := openSubs(t)
	if s.group != subscriptions || p.lists != 1 || p.views == 0 {
		t.Fatalf("group %d, lists %d, views %d", s.group, p.lists, p.views)
	}
	for _, text := range []string{s.text.SubscriptionsGroup, s.text.SubscriptionsLead, s.text.AddSubscription, "the list"} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	if err := tt.Click(s.text.AddSubscription); err != nil {
		t.Fatal(err)
	}
	if p.adds != 1 {
		t.Fatalf("adds %d", p.adds)
	}
	press(t, tt, s.text.Fetching)
	press(t, tt, s.text.SubscriptionsGroup)
	if s.group != subscriptions || p.lists != 2 {
		t.Fatalf("group %d, lists %d", s.group, p.lists)
	}
}

// TestSubscriptionsGroupMissing leaves the group out of the sidebar when there is no timeline behind the settings.
func TestSubscriptionsGroupMissing(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.OpenPage("subscriptions")
	tt.Frame()
	f.settle(tt)
	if s.group != appearance || tt.HasText(s.text.SubscriptionsGroup) {
		t.Fatalf("group %d, texts %v", s.group, tt.Texts())
	}
}

// TestSubscriptionsCrumb is a subscription's page: its name under a "‹ Subscriptions" link back to the list,
// with no lead and no Add subscription.
func TestSubscriptionsCrumb(t *testing.T) {
	s, _, p, tt := openSubs(t)
	p.crumb = "Example Blog"
	tt.Frame()
	if !tt.HasText("Example Blog") || !tt.HasText(s.text.SubscriptionsGroup) || !tt.HasText("the page") ||
		tt.HasText(s.text.SubscriptionsLead) || tt.HasText(s.text.AddSubscription) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click(s.text.BackList); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.backs != 1 || !tt.HasText("the list") {
		t.Fatalf("backs %d, texts %v", p.backs, tt.Texts())
	}
}

// TestSubscriptionsEscape is Escape on the group: the group's field gives up the focus, the group takes it to clear
// a selection or go back to the list, text not saved on a page stays, and only then the timeline comes back. A
// dialog of the group keeps Escape to itself.
func TestSubscriptionsEscape(t *testing.T) {
	s, f, p, tt := openSubs(t)
	p.typing = true
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || p.escapes != 0 {
		t.Fatalf("typing: back %d, escapes %d", f.back, p.escapes)
	}
	p.typing, p.taking = false, true
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || p.escapes != 1 {
		t.Fatalf("taken: back %d, escapes %d", f.back, p.escapes)
	}
	p.taking, p.unsaved = false, true
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || !slices.Contains(tt.Announcements(), s.text.Unsaved) {
		t.Fatalf("unsaved: back %d, announcements %v", f.back, tt.Announcements())
	}
	p.unsaved, p.open = false, true
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || p.escapes != 2 {
		t.Fatalf("dialog: back %d, escapes %d", f.back, p.escapes)
	}
	p.open = false
	tt.Key(0, ui.KeyEscape)
	if f.back != 1 {
		t.Fatalf("back %d", f.back)
	}
}
