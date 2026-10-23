package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// catalogTimeout and catalogMaxBytes bound one read of an instance's route catalog.
	catalogTimeout  = 15 * time.Second
	catalogMaxBytes = 32 << 20
	// catalogTTL is how long a parsed catalog is kept; catalogFailureTTL how long a failed read is remembered, so
	// typing does not ask an instance over and over.
	catalogTTL        = 24 * time.Hour
	catalogFailureTTL = time.Minute
	// maxRoutes is the most candidates a search returns; previewEntries the most entries a preview returns.
	maxRoutes      = 10
	previewEntries = 5
)

var (
	// ErrNoInstance reports a preview asked while no RSSHub instance is set.
	ErrNoInstance = errors.New("no RSSHub instance is set")
	// ErrRouteNotFound reports a route the instance answered 404 for.
	ErrRouteNotFound = errors.New("the instance has no such route")
	// ErrRouteUnavailable reports a route no instance could deliver.
	ErrRouteUnavailable = errors.New("the route could not be fetched")
	// ErrNotAFeed reports an answer that is not a feed.
	ErrNotAFeed = errors.New("the answer is not a feed")
)

// RouteParam is one parameter of a route template, in the order it appears.
type RouteParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional"`
}

// Route is one candidate route of an instance's catalog. Values holds the parameters read from a pasted link.
type Route struct {
	Path          string            `json:"path"`
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	NamespaceName string            `json:"namespaceName"`
	Example       string            `json:"example"`
	Parameters    []RouteParam      `json:"parameters"`
	Values        map[string]string `json:"values"`
}

// RouteMatches is the outcome of a search: Available is false when no instance's catalog could be read.
type RouteMatches struct {
	Available bool    `json:"available"`
	Instance  string  `json:"instance"`
	Total     int     `json:"total"`
	Routes    []Route `json:"routes"`
}

// PreviewEntry is one entry of a previewed route; PublishedAt is Unix milliseconds, nil when the entry has none.
type PreviewEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	PublishedAt *int64 `json:"publishedAt"`
}

// RoutePreview is the first entries of a route, fetched without subscribing.
type RoutePreview struct {
	Instance string         `json:"instance"`
	Title    string         `json:"title"`
	Entries  []PreviewEntry `json:"entries"`
}

// Routes finds the routes of the user's RSSHub instances and previews them. The catalog is read from the instance
// only and kept in memory. Instance addresses are typed by the user and may be local, so it uses a plain client,
// like the poller.
type Routes struct {
	client    *http.Client
	userAgent string
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]*catalogSlot
}

// catalogSlot is the cached read of one instance. Its lock is held during a read, so concurrent searches share it.
type catalogSlot struct {
	mu  sync.Mutex
	cat *catalog // nil after a failed read
	at  time.Time
	set bool
}

// NewRoutes returns a finder that identifies itself as Oriel/version.
func NewRoutes(version string) *Routes {
	return &Routes{
		client:    &http.Client{Timeout: catalogTimeout},
		userAgent: "Oriel/" + version,
		now:       time.Now,
		cache:     map[string]*catalogSlot{},
	}
}

// Find returns the routes matching q, a pasted link or a name, in the first instance whose catalog can be read.
// refresh drops the cached catalogs of the instances first.
func (r *Routes) Find(ctx context.Context, instances []string, q string, refresh bool) RouteMatches {
	out := RouteMatches{Routes: []Route{}}
	for _, inst := range instances {
		cat := r.catalogOf(ctx, inst, refresh)
		if cat == nil {
			continue
		}
		out.Available, out.Instance = true, inst
		all := cat.match(strings.TrimSpace(q))
		out.Total = len(all)
		out.Routes = all[:min(len(all), maxRoutes)]
		break
	}
	return out
}

// catalogOf returns the catalog of an instance, nil when it cannot be read.
func (r *Routes) catalogOf(ctx context.Context, inst string, refresh bool) *catalog {
	key := strings.ToLower(inst)
	r.mu.Lock()
	slot := r.cache[key]
	if slot == nil {
		slot = &catalogSlot{}
		r.cache[key] = slot
	}
	r.mu.Unlock()

	slot.mu.Lock()
	defer slot.mu.Unlock()
	now := r.now()
	if slot.set && !refresh {
		ttl := catalogTTL
		if slot.cat == nil {
			ttl = catalogFailureTTL
		}
		if now.Sub(slot.at) < ttl {
			return slot.cat
		}
	}
	cat, err := r.readCatalog(ctx, inst)
	if err != nil && ctx.Err() != nil {
		// A canceled search says nothing about the instance.
		return nil
	}
	slot.cat, slot.at, slot.set = cat, now, true
	return cat
}

// readCatalog reads and parses /api/namespace of an instance. A catalog without a single route is an error.
func (r *Routes) readCatalog(ctx context.Context, inst string) (*catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst+"/api/namespace", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("no catalog: " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > catalogMaxBytes {
		return nil, errTooLarge
	}
	return parseCatalog(body)
}

// Preview fetches path from the instances in order, with the failover of fetching: an instance that cannot be
// reached, or answers 5xx or 429, hands over to the next; any other status is the answer.
func (r *Routes) Preview(ctx context.Context, instances []string, path string) (RoutePreview, error) {
	if len(instances) == 0 {
		return RoutePreview{}, ErrNoInstance
	}
	for _, inst := range instances {
		prev, retry, err := r.previewFrom(ctx, inst, path)
		if err == nil {
			return prev, nil
		}
		if !retry {
			return RoutePreview{}, err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return RoutePreview{}, ErrRouteUnavailable
}

// previewFrom asks one instance; retry says the next instance should be tried.
func (r *Routes) previewFrom(ctx context.Context, inst, path string) (RoutePreview, bool, error) {
	address := inst + path
	base, err := url.Parse(address)
	if err != nil {
		return RoutePreview{}, false, ErrRouteUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return RoutePreview{}, false, ErrRouteUnavailable
	}
	req.Header.Set("User-Agent", r.userAgent)
	resp, err := r.client.Do(req)
	if err != nil {
		return RoutePreview{}, true, ErrRouteUnavailable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return RoutePreview{}, true, ErrRouteUnavailable
	case resp.StatusCode == http.StatusNotFound:
		return RoutePreview{}, false, ErrRouteNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return RoutePreview{}, false, ErrRouteUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return RoutePreview{}, true, ErrRouteUnavailable
	}
	if len(body) > maxBytes {
		return RoutePreview{}, false, ErrRouteUnavailable
	}
	feed, err := parse(body, resp.Header.Get("Content-Type"), base)
	if err != nil {
		return RoutePreview{}, false, ErrNotAFeed
	}
	out := RoutePreview{Instance: inst, Title: feed.title, Entries: []PreviewEntry{}}
	for _, e := range feed.entries[:min(len(feed.entries), previewEntries)] {
		item := PreviewEntry{Title: e.Title, URL: e.URL}
		if !e.PublishedAt.IsZero() {
			ms := e.PublishedAt.UnixMilli()
			item.PublishedAt = &ms
		}
		out.Entries = append(out.Entries, item)
	}
	return out, false, nil
}

// catalog is the parsed route catalog of one instance, in the order the instance gave it.
type catalog struct {
	namespaces []namespace
}

type namespace struct {
	key, name, host string
	routes          []catalogRoute
}

type catalogRoute struct {
	Route
	sources []radarSource
}

// radarSource is a host and path pattern of a route's radar, such as github.com/:user/:repo/issues.
type radarSource struct {
	host string
	path []string // nil for a host-only pattern
}

type rawNamespace struct {
	Name   string          `json:"name"`
	URL    string          `json:"url"`
	Routes json.RawMessage `json:"routes"`
}

type rawRoute struct {
	Path       json.RawMessage `json:"path"`
	Name       string          `json:"name"`
	Example    string          `json:"example"`
	Parameters json.RawMessage `json:"parameters"`
	Radar      json.RawMessage `json:"radar"`
}

type kv struct {
	key string
	val json.RawMessage
}

// objectEntries lists the members of a JSON object in document order.
func objectEntries(raw json.RawMessage) ([]kv, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	var list []kv
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		list = append(list, kv{key, val})
	}
	return list, nil
}

// parseCatalog reads /api/namespace tolerantly: unknown fields are ignored, and a namespace or route that cannot
// be read is skipped.
func parseCatalog(body []byte) (*catalog, error) {
	entries, err := objectEntries(body)
	if err != nil {
		return nil, err
	}
	cat := &catalog{}
	count := 0
	for _, e := range entries {
		var raw rawNamespace
		if json.Unmarshal(e.val, &raw) != nil || e.key == "" {
			continue
		}
		ns := namespace{key: e.key, name: raw.Name, host: hostOf(raw.URL)}
		routes, err := objectEntries(raw.Routes)
		if err != nil {
			continue
		}
		for _, re := range routes {
			var rr rawRoute
			if json.Unmarshal(re.val, &rr) != nil {
				continue
			}
			for _, path := range stringsOf(rr.Path, re.key) {
				if cr, ok := buildRoute(ns, rr, path); ok {
					ns.routes = append(ns.routes, cr)
					count++
				}
			}
		}
		cat.namespaces = append(cat.namespaces, ns)
	}
	if count == 0 {
		return nil, errors.New("the catalog has no routes")
	}
	return cat, nil
}

// stringsOf reads a JSON string or an array of strings; fallback is used when there is none.
func stringsOf(raw json.RawMessage, fallback string) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		many = slices.DeleteFunc(many, func(s string) bool { return s == "" })
		if len(many) > 0 {
			return many
		}
	}
	return []string{fallback}
}

var routeParam = regexp.MustCompile(`:([A-Za-z0-9_]+)(\{[^}]*\})?([?*+])?`)

// buildRoute makes a catalog route of one path template.
func buildRoute(ns namespace, rr rawRoute, path string) (catalogRoute, bool) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path != "/"+ns.key && !strings.HasPrefix(path, "/"+ns.key+"/") {
		path = "/" + ns.key + path
	}
	descriptions := parameterDescriptions(rr.Parameters)
	params := []RouteParam{}
	for _, m := range routeParam.FindAllStringSubmatch(path, -1) {
		params = append(params, RouteParam{Name: m[1], Description: descriptions[m[1]], Optional: m[3] == "?" || m[3] == "*"})
	}
	// Strip the regular expressions of parameters from the template shown and subscribed.
	path = routeParam.ReplaceAllString(path, ":$1$3")
	name := rr.Name
	if name == "" {
		name = path
	}
	cr := catalogRoute{Route: Route{
		Path: path, Name: name, Namespace: ns.key, NamespaceName: ns.name, Example: rr.Example, Parameters: params,
		Values: map[string]string{},
	}}
	cr.sources = radarSources(rr.Radar)
	return cr, true
}

// parameterDescriptions reads the parameters object, whose values are descriptions or objects holding one.
func parameterDescriptions(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	entries, err := objectEntries(raw)
	if err != nil {
		return out
	}
	for _, e := range entries {
		var text string
		if json.Unmarshal(e.val, &text) == nil {
			out[e.key] = text
			continue
		}
		var obj struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(e.val, &obj) == nil {
			out[e.key] = obj.Description
		}
	}
	return out
}

// radarSources reads the sources of a route's radar, an array of {source: [...]} or a single such object.
func radarSources(raw json.RawMessage) []radarSource {
	type item struct {
		Source json.RawMessage `json:"source"`
	}
	var items []item
	if json.Unmarshal(raw, &items) != nil {
		var one item
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		items = []item{one}
	}
	var out []radarSource
	for _, it := range items {
		for _, s := range stringsOf(it.Source, "") {
			if src, ok := parseSource(s); ok {
				out = append(out, src)
			}
		}
	}
	return out
}

// parseSource splits a pattern such as github.com/:user/:repo into its host and path segments.
func parseSource(s string) (radarSource, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	host, rest, _ := strings.Cut(s, "/")
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	if host == "" {
		return radarSource{}, false
	}
	src := radarSource{host: host}
	if segs := splitPath(rest); len(segs) > 0 {
		src.path = segs
	}
	return src, true
}

func splitPath(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}

// hostOf returns the host of an address that may lack its scheme, without "www.".
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// isAddress says whether q is a link rather than a name: it has a web scheme, or a dot and no space.
func isAddress(q string) bool {
	lower := strings.ToLower(q)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	return strings.Contains(q, ".") && !strings.ContainsAny(q, " \t")
}

// match returns every route that fits q, best first.
func (c *catalog) match(q string) []Route {
	if q == "" {
		return nil
	}
	if isAddress(q) {
		return c.matchLink(q)
	}
	return c.matchName(strings.ToLower(q))
}

type ranked struct {
	tier  int
	route Route
}

func collect(list []ranked) []Route {
	slices.SortStableFunc(list, func(a, b ranked) int { return a.tier - b.tier })
	out := make([]Route, len(list))
	for i, r := range list {
		out[i] = r.route
	}
	return out
}

func (c *catalog) matchLink(q string) []Route {
	if !strings.Contains(q, "://") {
		q = "https://" + q
	}
	u, err := url.Parse(q)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := splitPath(u.Path)
	var list []ranked
	for _, ns := range c.namespaces {
		for _, cr := range ns.routes {
			tier, values := 99, map[string]string(nil)
			for _, src := range cr.sources {
				if !hostMatches(src.host, host) {
					continue
				}
				if src.path == nil {
					tier = min(tier, 1)
					continue
				}
				if captured, ok := pathMatches(src.path, segs); ok {
					if tier > 0 {
						values = captured
					}
					tier = 0
				} else {
					tier = min(tier, 1)
				}
			}
			if tier == 99 && len(cr.sources) == 0 && ns.host != "" && ns.host == host {
				tier = 2
			}
			if tier == 99 {
				continue
			}
			route := cr.Route
			route.Values = map[string]string{}
			for _, p := range route.Parameters {
				if v, ok := values[p.Name]; ok && tier == 0 {
					route.Values[p.Name] = v
				}
			}
			list = append(list, ranked{tier, route})
		}
	}
	return collect(list)
}

// hostMatches compares a pattern host, which may start with "*.", with the host of a link.
func hostMatches(pattern, host string) bool {
	if rest, ok := strings.CutPrefix(pattern, "*."); ok {
		return strings.HasSuffix(host, "."+rest)
	}
	return pattern == host
}

// pathMatches matches the path segments of a link against a radar path pattern: ":name" takes one segment, a
// trailing "?" makes it optional, "*" takes the rest. It returns the segments the names took.
func pathMatches(pattern, segs []string) (map[string]string, bool) {
	values := map[string]string{}
	i := 0
	for _, p := range pattern {
		switch {
		case p == "*":
			return values, true
		case strings.HasPrefix(p, ":"):
			name := strings.TrimPrefix(p, ":")
			optional := strings.HasSuffix(name, "?")
			name = strings.TrimRight(name, "?")
			if i >= len(segs) {
				if optional {
					continue
				}
				return nil, false
			}
			values[name] = segs[i]
			i++
		default:
			if i >= len(segs) || !strings.EqualFold(segs[i], p) {
				return nil, false
			}
			i++
		}
	}
	return values, i == len(segs)
}

func (c *catalog) matchName(q string) []Route {
	var list []ranked
	for _, ns := range c.namespaces {
		nsTier := 99
		for _, n := range []string{strings.ToLower(ns.key), strings.ToLower(ns.name)} {
			switch {
			case n == "":
			case n == q:
				nsTier = min(nsTier, 0)
			case strings.HasPrefix(n, q):
				nsTier = min(nsTier, 1)
			case strings.Contains(n, q):
				nsTier = min(nsTier, 2)
			}
		}
		for _, cr := range ns.routes {
			tier := nsTier
			switch {
			case tier < 99:
			case strings.Contains(strings.ToLower(cr.Name), q):
				tier = 3
			case strings.Contains(strings.ToLower(cr.Path), q):
				tier = 4
			default:
				continue
			}
			list = append(list, ranked{tier, cr.Route})
		}
	}
	return collect(list)
}
