package subscriptions

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

var issueRoute = client.Route{
	Path: "/github/issue/:user/:repo", Name: "Repository Issues", Namespace: "github", NamespaceName: "GitHub",
	Parameters: []client.RouteParam{{Name: "user", Description: "GitHub user"}, {Name: "repo", Description: "Repository name"}},
}

var trendingRoute = client.Route{
	Path: "/github/trending/:since/:language?", Name: "Trending", Namespace: "github", NamespaceName: "GitHub",
	Parameters: []client.RouteParam{{Name: "since", Description: "Period"}, {Name: "language", Description: "Language", Optional: true}},
}

// settleTyped lets the pause after typing pass and the background work finish.
func settleTyped(f *fake, tt *ui.Tester) {
	tt.Frame()
	f.lateBy += lookAfter
	tt.Frame()
	f.settle(tt)
	tt.Frame()
}

func TestRoutesByName(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.SetSize(1000, 1600)
	f.routes = client.Routes{Available: true, Instance: "https://rsshub.example.com", Total: 2,
		Routes: []client.Route{issueRoute, trendingRoute}}
	tt.Type("github")
	tt.Frame()
	if len(f.searched) != 0 {
		t.Fatalf("searched before the pause: %v", f.searched)
	}
	settleTyped(f, tt)
	if !slices.Equal(f.searched, []string{"github"}) || !tt.HasText("Found 2 routes. Choose one.") ||
		!tt.HasText("GitHub · Repository Issues") || !tt.HasText("/github/trending/:since/:language?") {
		t.Fatalf("searched %v, texts %v", f.searched, tt.Texts())
	}
	// Nothing is chosen yet, so there is nothing to add.
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 0 || s.add.addressErr != "" {
		t.Fatalf("added %+v, error %q", f.added, s.add.addressErr)
	}

	if err := tt.Click("/github/issue/:user/:repo"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.Focused("user") && !tt.HasText("user") {
		t.Fatalf("no parameter fields: %v", tt.Texts())
	}
	// Both parameters are required and empty: no preview, no adding.
	if s.routeComplete() || s.canAdd() {
		t.Fatal("complete without parameters")
	}
	focusParameter(t, tt, "user")
	tt.Type("rsshub")
	focusParameter(t, tt, "repo")
	tt.Type("rsshub")
	tt.Frame()
	if got := s.routePath(); got != "/github/issue/rsshub/rsshub" || !s.canAdd() {
		t.Fatalf("path %q, can add %v", got, s.canAdd())
	}

	published := now.Add(-2 * 3600 * 1e9).UnixMilli()
	f.preview = client.RoutePreview{Title: "Issues", Entries: []client.PreviewEntry{{Title: "First issue", PublishedAt: &published},
		{Title: "Second issue"}}}
	if err := tt.Click("Preview"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.previewed, []string{"/github/issue/rsshub/rsshub"}) || !tt.HasText("Latest from “Issues”") ||
		!tt.HasText("First issue") || !tt.HasText("2 h") || !tt.HasText("Second issue") || len(f.added) != 0 {
		t.Fatalf("previewed %v, added %v, texts %v", f.previewed, f.added, tt.Texts())
	}

	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	want := client.NewSource{RSSHubPath: "/github/issue/rsshub/rsshub", Kind: "article"}
	if len(f.added) != 1 || f.added[0] != want {
		t.Fatalf("added %+v", f.added)
	}
}

func TestRoutesByLinkFillTheParameters(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	r := issueRoute
	r.Values = map[string]string{"user": "rsshub", "repo": "rsshub"}
	f.routes = client.Routes{Available: true, Total: 1, Routes: []client.Route{r}}
	pageAddress(t, f, tt, "https://blog.example/rsshub/rsshub")
	if !slices.Equal(f.looked, []string{"https://blog.example/rsshub/rsshub"}) ||
		!slices.Equal(f.searched, []string{"https://blog.example/rsshub/rsshub"}) {
		t.Fatalf("looked %v, searched %v", f.looked, f.searched)
	}
	if err := tt.Click("/github/issue/:user/:repo"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := s.routePath(); got != "/github/issue/rsshub/rsshub" || !s.canAdd() {
		t.Fatalf("path %q, can add %v, texts %v", got, s.canAdd(), tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].RSSHubPath != "/github/issue/rsshub/rsshub" || f.added[0].URL != "" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestRoutesForALinkStillAddAsTyped(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.routes = client.Routes{Available: true, Total: 1, Routes: []client.Route{issueRoute}}
	pageAddress(t, f, tt, "https://blog.example/rsshub/rsshub")
	if !tt.HasText("Found 1 route. Choose one.") || !s.canAdd() {
		t.Fatalf("can add %v, texts %v", s.canAdd(), tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].URL != "https://blog.example/rsshub/rsshub" || f.added[0].RSSHubPath != "" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestRoutesOptionalParameterDropsItsSegment(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.SetSize(1000, 1600)
	f.routes = client.Routes{Available: true, Total: 1, Routes: []client.Route{trendingRoute}}
	tt.Type("trending")
	settleTyped(f, tt)
	if err := tt.Click("/github/trending/:since/:language?"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	focusParameter(t, tt, "since")
	tt.Type("daily")
	tt.Frame()
	if got := s.routePath(); got != "/github/trending/daily" || !s.canAdd() {
		t.Fatalf("path %q, can add %v", got, s.canAdd())
	}
}

func TestRoutesNoCatalogIsQuiet(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.SetSize(1000, 1400)
	f.routes = client.Routes{}
	tt.Type("github")
	settleTyped(f, tt)
	if !tt.HasText(s.text.NoCatalog) || s.add.addressErr != "" || s.add.formErr != "" {
		t.Fatalf("texts %v", tt.Texts())
	}
	// A route typed by hand still works.
	s.add.address = "/github/issue/rsshub/rsshub"
	tt.Frame()
	if tt.HasText(s.text.NoCatalog) || !s.canAdd() {
		t.Fatalf("texts %v", tt.Texts())
	}

	f.routes, f.routesErr = client.Routes{Available: true}, errors.New("down")
	s.add.address = "nothing"
	settleTyped(f, tt)
	if !tt.HasText(s.text.NoCatalog) || s.add.formErr != "" {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRoutesNoMatch(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.SetSize(1000, 1400)
	f.routes = client.Routes{Available: true}
	tt.Type("zzz")
	settleTyped(f, tt)
	if !tt.HasText(s.text.NoRoutes) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if len(f.added) != 0 || s.add.addressErr != s.text.InvalidAddress {
		t.Fatalf("added %+v, error %q", f.added, s.add.addressErr)
	}
}

func TestRoutePreviewFailure(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.SetSize(1000, 1600)
	r := issueRoute
	r.Values = map[string]string{"user": "a", "repo": "b"}
	f.routes = client.Routes{Available: true, Total: 1, Routes: []client.Route{r}}
	pageAddress(t, f, tt, "https://blog.example/a/b")
	if err := tt.Click("/github/issue/:user/:repo"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f.previewErr = &client.APIError{Status: 502, Code: "route_not_found"}
	if err := tt.Click("Preview"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.PreviewNotFound) {
		t.Fatalf("texts %v", tt.Texts())
	}
	// Editing a parameter drops the old answer.
	s.add.routes.values[0] = "c"
	tt.Frame()
	if tt.HasText(s.text.PreviewNotFound) {
		t.Fatal("the old preview stays")
	}
}

// focusParameter focuses the field under the label of a parameter.
func focusParameter(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	r, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no %q in %v", name, tt.Texts())
	}
	tt.ClickAt(r.X+60, r.Y+r.H+20)
	tt.Frame()
	if !tt.Focused(name) {
		t.Fatalf("%q is not focused", name)
	}
}

func TestRoutesCopyInBothLanguages(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	open(t, s, f, tt)
	tt.SetSize(1000, 1400)
	f.routes = client.Routes{Available: true}
	tt.Type("github")
	settleTyped(f, tt)
	if !tt.HasText(s.text.NoRoutes) || s.text.NoRoutes == translations["en"].NoRoutes {
		t.Fatalf("texts %v", tt.Texts())
	}
}
