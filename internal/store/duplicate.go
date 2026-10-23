package store

import (
	"context"
	"net/url"
	"sort"
	"strings"
)

// ExistsError says which subscription a new one would repeat. It matches ErrExists.
type ExistsError struct{ ID int64 }

func (e *ExistsError) Error() string { return ErrExists.Error() }

func (e *ExistsError) Is(target error) bool { return target == ErrExists }

// feedURLKey is how two native feed addresses are compared: scheme ignored, host lowercase without "www." and the
// default port, no fragment, no trailing slash on the path, tracking parameters dropped and the other parameters
// sorted, so an empty path equals "/". The stored address is never rewritten. An address that does not parse is
// compared as it is.
func feedURLKey(raw string) string {
	raw = strings.TrimSpace(raw)
	host, path, query, ok := splitAddress(raw)
	if !ok {
		return raw
	}
	return host + path + query
}

// splitAddress cuts an http or https address into the host (with a port that is not the default), the path without
// a trailing slash, and the query as "?a=1&b=2" or "" when none is left after the tracking parameters go.
func splitAddress(raw string) (host, path, query string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", "", false
	}
	host, port := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	params := u.Query()
	names := make([]string, 0, len(params))
	for name, values := range params {
		if isTrackingParam(name) {
			continue
		}
		sort.Strings(values)
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		for _, value := range params[name] {
			if b.Len() == 0 {
				b.WriteByte('?')
			} else {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(name) + "=" + url.QueryEscape(value))
		}
	}
	return host, strings.TrimRight(u.Path, "/"), b.String(), true
}

func isTrackingParam(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "utm_") || trackingParams[lower]
}

// sourceKeys lists the identities of a subscription; two subscriptions are the same feed when they share one.
//   - "u:" is the native address, by feedURLKey.
//   - "r:" is an RSSHub route: the path and query of a RSSHub source, or of a native address that lies on one of the
//     RSSHub instances in roots, so a feed typed in as "https://rsshub.example/github/issue/a/b" equals the route
//     "/github/issue/a/b".
//   - "y:" is the upstream of the common routes that have a plain feed of their own (YouTube channels and playlists),
//     so the route and the feed address are one subscription.
func sourceKeys(address, route string, roots []rootAddress) []string {
	var keys []string
	if address != "" {
		host, path, query, ok := splitAddress(strings.TrimSpace(address))
		if !ok {
			return []string{"u:" + strings.TrimSpace(address)}
		}
		keys = append(keys, "u:"+host+path+query)
		for _, root := range roots {
			if host == root.host && (path == root.path || strings.HasPrefix(path, root.path+"/")) {
				keys = append(keys, routeKeys(strings.TrimPrefix(path, root.path)+query)...)
			}
		}
		if key := feedUpstream(host, path, query); key != "" {
			keys = append(keys, key)
		}
	}
	if route = strings.TrimSpace(route); route != "" {
		keys = append(keys, routeKeys(route)...)
	}
	return keys
}

// routeKeys is the "r:" key of a RSSHub route, and its upstream key when it has one. A route is a path with an
// optional query; the empty route (the instance's home page) has none.
func routeKeys(route string) []string {
	_, path, query, ok := splitAddress("https://x" + route)
	if !ok || path == "" {
		return nil
	}
	keys := []string{"r:" + path + query}
	if query != "" {
		return keys
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) == 3 && segments[0] == "youtube" && segments[2] != "" {
		switch segments[1] {
		case "channel":
			keys = append(keys, "y:channel_id="+segments[2])
		case "playlist":
			keys = append(keys, "y:playlist_id="+segments[2])
		}
	}
	return keys
}

// feedUpstream is the "y:" key of a YouTube feed address, or "".
func feedUpstream(host, path, query string) string {
	if host != "youtube.com" || path != "/feeds/videos.xml" {
		return ""
	}
	for _, name := range []string{"channel_id", "playlist_id"} {
		if value, ok := strings.CutPrefix(query, "?"+name+"="); ok && value != "" && !strings.Contains(value, "&") {
			return "y:" + name + "=" + value
		}
	}
	return ""
}

// rootAddress is the host and path of a RSSHub instance, in the form splitAddress gives.
type rootAddress struct{ host, path string }

// rsshubRoots lists the RSSHub instances a native address may lie on: those in the settings and those that served a
// source last, so that an address copied from an instance since dropped from the settings still matches.
func (s *Store) rsshubRoots(ctx context.Context) ([]rootAddress, error) {
	raw, err := s.Setting(ctx, "rsshub_base_url")
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT rsshub_instance FROM sources WHERE rsshub_instance <> '' AND deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bases := strings.Split(raw, "\n")
	for rows.Next() {
		var base string
		if err := rows.Scan(&base); err != nil {
			return nil, err
		}
		bases = append(bases, base)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var roots []rootAddress
	seen := map[rootAddress]bool{}
	for _, base := range bases {
		if host, path, _, ok := splitAddress(strings.TrimSpace(base)); ok && !seen[rootAddress{host, path}] {
			seen[rootAddress{host, path}] = true
			roots = append(roots, rootAddress{host, path})
		}
	}
	return roots, nil
}

// FindDuplicate returns the id of the live subscription that src repeats, by sourceKeys, or ok false when there is
// none. When several match, it is the oldest.
func (s *Store) FindDuplicate(ctx context.Context, src NewSource) (id int64, ok bool, err error) {
	roots, err := s.rsshubRoots(ctx)
	if err != nil {
		return 0, false, err
	}
	want := sourceKeys(src.URL, src.RSSHubPath, roots)
	if len(want) == 0 {
		return 0, false, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, url, rsshub_path FROM sources WHERE deleted_at IS NULL ORDER BY id")
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var have int64
		var address, route string
		if err := rows.Scan(&have, &address, &route); err != nil {
			return 0, false, err
		}
		if sharesKey(want, sourceKeys(address, route, roots)) {
			return have, true, nil
		}
	}
	return 0, false, rows.Err()
}

func sharesKey(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
