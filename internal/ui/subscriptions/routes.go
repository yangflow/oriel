package subscriptions

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// The states of the preview of a route.
const (
	previewIdle = iota
	previewLoading
	previewDone
	previewFailed
)

// previewing is the preview of the route path names: the first entries the user's RSSHub returns for it.
type previewing struct {
	state int
	path  string
	data  client.RoutePreview
	err   string
}

// finding is the search for the RSSHub routes that fit query, the trimmed text of the address field: a name, or a
// link no feed was found at. It uses the states of the feed lookup.
type finding struct {
	query     string
	state     int
	typedAt   time.Time
	res       client.Routes
	chosen    int      // the index in res.Routes that Add subscribes to, -1 for none
	values    []string // what the parameters of the chosen route are filled with
	valuesFor int      // the route the values are for, -1 for none
	preview   previewing
}

// What the route search shows under the address field.
const (
	routeNone = iota
	routeSearching
	routeList
	routeNoCatalog // the instance has no catalog Oriel can read; the route is typed by hand
	routeNoMatch
)

// routeQuery returns the text to search routes for, "" when there is none: a name, which no feed address can be, or
// a link at which no feed was found. instant says the search need not wait for the typing to pause.
func (s *Screen) routeQuery() (query string, instant bool) {
	a := s.add
	query = strings.TrimSpace(a.address)
	if query == "" {
		return "", false
	}
	if _, ok := recognize(a.address, a.root); !ok {
		return query, false
	}
	if l := a.look; l.address != "" && (l.state == lookFailed || l.state == lookDone && len(l.found) == 0) {
		return query, true
	}
	return "", false
}

// byName reports whether the address field holds a name rather than an address a feed can be at.
func (s *Screen) byName() bool {
	_, ok := recognize(s.add.address, s.add.root)
	return !ok
}

// watchRoutes keeps the route search in step with the address field, like watch does for the feed lookup.
func (s *Screen) watchRoutes() time.Duration {
	a := &s.add
	if !a.open || s.api == nil {
		return 0
	}
	now := s.deps.Now()
	if query, instant := s.routeQuery(); a.routes.query != query {
		a.routes = finding{query: query, typedAt: now, chosen: -1, valuesFor: -1}
		if instant {
			a.routes.typedAt = now.Add(-lookAfter)
		}
		if query != "" {
			a.routes.state = lookWaiting
		}
	}
	if a.routes.state != lookWaiting {
		return 0
	}
	if wait := lookAfter - now.Sub(a.routes.typedAt); wait > 0 {
		return wait
	}
	s.findRoutes()
	return 0
}

// findRoutes asks the server for the routes of the query, in the background.
func (s *Screen) findRoutes() {
	r := &s.add.routes
	r.state = lookSearching
	query, gen, api := r.query, s.gen, s.api
	s.deps.Go(func() func() {
		res, err := api.FindRoutes(context.Background(), query, false)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			r := &s.add.routes
			if r.query != query || r.state != lookSearching {
				return // the address changed meanwhile
			}
			if err != nil {
				r.state = lookFailed
				return
			}
			r.state, r.res = lookDone, res
		}
	})
}

// routeView says what the route search shows. A link that the feed lookup could not use only shows routes that were
// found; a name always says what came of the search.
func (s *Screen) routeView() int {
	r := s.add.routes
	if r.query == "" {
		return routeNone
	}
	switch {
	case r.state == lookSearching:
		return routeSearching
	case r.state == lookDone && len(r.res.Routes) > 0:
		return routeList
	case !s.byName():
		return routeNone
	case r.state == lookFailed || r.state == lookDone && !r.res.Available:
		return routeNoCatalog
	case r.state == lookDone:
		return routeNoMatch
	}
	return routeNone
}

func (s *Screen) routeNote(view int) string {
	switch view {
	case routeSearching:
		return s.text.SearchingRoutes
	case routeList:
		return s.count(s.text.FoundRoutes, len(s.add.routes.res.Routes))
	case routeNoCatalog:
		return s.text.NoCatalog
	}
	return s.text.NoRoutes
}

// chosenRoute is the route the user chose from the list.
func (s *Screen) chosenRoute() (client.Route, bool) {
	r := s.add.routes
	if r.state != lookDone || r.chosen < 0 || r.chosen >= len(r.res.Routes) {
		return client.Route{}, false
	}
	return r.res.Routes[r.chosen], true
}

// syncRoute fills the parameters of a route newly chosen with what the link gave, and drops a preview that no longer
// fits the path.
func (s *Screen) syncRoute() {
	r := &s.add.routes
	rt, ok := s.chosenRoute()
	if !ok {
		r.values, r.valuesFor = nil, -1
		r.preview = previewing{}
		return
	}
	if r.valuesFor != r.chosen {
		r.values, r.valuesFor = make([]string, len(rt.Parameters)), r.chosen
		for i, p := range rt.Parameters {
			r.values[i] = rt.Values[p.Name]
		}
	}
	if r.preview.path != s.routePath() {
		r.preview = previewing{}
	}
}

// routeComplete reports whether every parameter the chosen route needs is filled.
func (s *Screen) routeComplete() bool {
	r := s.add.routes
	rt, ok := s.chosenRoute()
	if !ok || len(r.values) != len(rt.Parameters) {
		return false
	}
	for i, p := range rt.Parameters {
		if !p.Optional && strings.TrimSpace(r.values[i]) == "" {
			return false
		}
	}
	return true
}

// routeReady reports whether the routes listed leave Add something to subscribe to: a name needs a chosen route with
// its parameters filled, while a link is also added as typed when no route is chosen.
func (s *Screen) routeReady() bool {
	if _, ok := s.chosenRoute(); ok {
		return s.routeComplete()
	}
	return !(s.byName() && s.routeView() == routeList)
}

// routePath puts the filled parameters into the template of the chosen route; an optional parameter left empty drops
// its segment.
func (s *Screen) routePath() string {
	r := s.add.routes
	rt, ok := s.chosenRoute()
	if !ok {
		return ""
	}
	var out []string
	for _, seg := range strings.Split(rt.Path, "/") {
		if !strings.HasPrefix(seg, ":") {
			out = append(out, seg)
			continue
		}
		name := strings.TrimRight(seg[1:], "?*")
		value := ""
		for i, p := range rt.Parameters {
			if p.Name == name && i < len(r.values) {
				value = strings.TrimSpace(r.values[i])
			}
		}
		switch {
		case value == "" && strings.HasSuffix(seg, "?"), value == "" && strings.HasSuffix(seg, "*"):
		case strings.HasSuffix(seg, "*"):
			out = append(out, value)
		default:
			out = append(out, url.PathEscape(value))
		}
	}
	return strings.Join(out, "/")
}

// previewRoute asks for the first entries of the chosen route as filled in, without subscribing.
func (s *Screen) previewRoute() {
	r := &s.add.routes
	if !s.routeComplete() || r.preview.state == previewLoading {
		return
	}
	path, gen, api := s.routePath(), s.gen, s.api
	r.preview = previewing{state: previewLoading, path: path}
	s.deps.Go(func() func() {
		data, err := api.PreviewRoute(context.Background(), path)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			p := &s.add.routes.preview
			if p.path != path || p.state != previewLoading {
				return // the parameters changed meanwhile
			}
			switch {
			case err == nil:
				p.state, p.data = previewDone, data
			case code(err) == "route_not_found" || code(err) == "not_a_feed":
				p.state, p.err = previewFailed, s.text.PreviewNotFound
			default:
				p.state, p.err = previewFailed, s.problem(err)
			}
			s.announce = p.err
		}
	})
}

// routeName names a route by its namespace and its own name.
func (s *Screen) routeName(rt client.Route) string {
	space := rt.NamespaceName
	if space == "" {
		space = rt.Namespace
	}
	switch {
	case space == "":
		return rt.Name
	case rt.Name == "":
		return space
	}
	return space + s.text.Separator + rt.Name
}

// routeList lists the routes found, for the user to choose one, and under it the parameters of the chosen route.
func (s *Screen) routeList(c *ui.Context) {
	th := c.Theme()
	a := &s.add
	head := s.routeNote(routeList)
	ui.Column(c).FillWidth().Gap(theme.Px(th, 10)).Children(func() {
		ui.Column(c).FillWidth().Gap(theme.Px(th, 6)).Children(func() {
			help(c, head)
			ui.RadioGroup(c, func() {
				for i, rt := range a.routes.res.Routes {
					what := s.routeName(rt)
					on := a.routes.chosen == i
					row := ui.RadioBase(c, &a.routes.chosen, i).FillWidth().AlignItems(ui.Center).Gap(theme.Px(th, 10)).
						Padding(th.Space(2), theme.Px(th, 10)).Radius(th.Radius).Label(what + s.text.Separator + rt.Path).Disabled(a.busy)
					if on {
						row.Background(theme.Soft(th, th.Accent))
					} else {
						row.Border(1, th.Border)
					}
					row.Children(func() {
						ui.Icon(c, icons.Rss).FontSize(theme.Px(th, 16)).TextColor(th.Accent)
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, what).FontWeight(600).SingleLine()
							ui.Text(c, rt.Path).Font("monospace").FontSize(theme.Px(th, 12)).TextColor(th.TextMuted).SingleLine()
						})
						if on {
							ui.Icon(c, icons.Check).FontSize(theme.Px(th, 16)).TextColor(th.Success)
						}
					})
				}
			}).FillWidth().Gap(theme.Px(th, 6)).Label(head)
		})
		if rt, ok := s.chosenRoute(); ok {
			s.routeParams(c, rt)
		}
	})
}

// routeParams is the fields of the parameters of rt, its Preview button and the preview.
func (s *Screen) routeParams(c *ui.Context, rt client.Route) {
	th := c.Theme()
	a := &s.add
	r := &a.routes
	ui.Column(c).FillWidth().Gap(theme.Px(th, 10)).Children(func() {
		for i, p := range rt.Parameters {
			if i >= len(r.values) {
				break
			}
			ui.Column(c).FillWidth().Gap(th.Space(1)).Children(func() {
				label(c, p.Name)
				box(c, "param-"+p.Name, nil, false, th.Surface, func() {
					in := ui.TextInputBase(c, &r.values[i]).Grow(1).MinWidth(0).FocusRing(false).Label(p.Name).
						Placeholder(p.Description).Disabled(a.busy)
					if in.Submitted() {
						s.submit(false)
					}
				})
			})
		}
		busy := r.preview.state == previewLoading
		b := button(c, secondary, !s.routeComplete() || busy || a.busy).Key("preview")
		textButton(c, b, secondary, icons.Eye, s.text.Preview, busy)
		if b.Clicked() {
			s.previewRoute()
		}
		switch r.preview.state {
		case previewDone:
			s.previewCard(c)
		case previewFailed:
			errorLine(c, r.preview.err, "")
		}
	})
}

// previewCard lists the first entries of the previewed route, with how long ago each was published.
func (s *Screen) previewCard(c *ui.Context) {
	th := c.Theme()
	p := s.add.routes.preview
	head := strings.ReplaceAll(s.text.PreviewOf, "{name}", p.data.Title)
	ui.Column(c).FillWidth().Gap(theme.Px(th, 6)).Padding(th.Space(2), theme.Px(th, 10)).Radius(th.Radius).Border(1, th.Border).
		Role(ui.RoleStatus).Label(head).Children(func() {
		if len(p.data.Entries) == 0 {
			help(c, s.text.PreviewEmpty)
			return
		}
		help(c, head)
		now := s.deps.Now()
		for _, e := range p.data.Entries {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(th, 10)).Children(func() {
				ui.Text(c, e.Title).Grow(1).MinWidth(0).FontWeight(500).SingleLine()
				if e.PublishedAt != nil {
					ui.Text(c, locale.Capital(locale.Ago(s.lang, now, time.UnixMilli(*e.PublishedAt)))).FontSize(theme.Px(th, 12)).TextColor(th.TextMuted).NoWrap()
				}
			})
		}
	})
}
