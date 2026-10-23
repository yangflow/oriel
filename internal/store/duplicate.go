package store

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/idna"
)

// ExistsError says which subscription a new one would repeat. It matches ErrExists.
type ExistsError struct{ ID int64 }

func (e *ExistsError) Error() string { return ErrExists.Error() }

func (e *ExistsError) Is(target error) bool { return target == ErrExists }

// feedURLKey is how two native feed addresses are compared: scheme ignored, host lowercase (an internationalised name
// in its ASCII form) without "www." and the default port, no fragment, the user name and password kept, no trailing slash on the path, tracking parameters dropped and the other parameters
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
	host, port := normalHost(u.Hostname()), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	if u.User != nil {
		host = u.User.String() + "@" + host // another login can be another feed; the key is never shown or logged
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
	return host, strings.TrimRight(u.EscapedPath(), "/"), b.String(), true
}

// normalHost lowercases a host name, turns an internationalised one into its ASCII form and drops a leading "www.".
func normalHost(host string) string {
	host = strings.ToLower(host)
	if !strings.Contains(host, ":") {
		if ascii, err := idna.Lookup.ToASCII(host); err == nil {
			host = ascii
		}
	}
	return strings.TrimPrefix(host, "www.")
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

// DuplicateIndex answers "is this subscription already there" from the identities of all subscriptions, read once.
// An import builds one and keeps it up to date with Add, so N feeds cost one read and not N.
type DuplicateIndex struct {
	roots   []rootAddress
	live    map[string]int64   // identity -> the oldest live subscription with it
	deleted map[string][]int64 // identity -> deleted subscriptions still in their undo window
	gone    map[int64]bool     // deleted subscriptions purged through this index
}

// exactKey is the identity of the very same address and route, which the unique index refuses.
func exactKey(address, route string) string { return "x:" + address + "\x00" + route }

// DuplicateIndex reads the identities of every subscription.
func (s *Store) DuplicateIndex(ctx context.Context) (*DuplicateIndex, error) {
	roots, err := s.rsshubRoots(ctx)
	if err != nil {
		return nil, err
	}
	idx := &DuplicateIndex{roots: roots, live: map[string]int64{}, deleted: map[string][]int64{}, gone: map[int64]bool{}}
	rows, err := s.db.QueryContext(ctx, "SELECT id, url, rsshub_path, deleted_at IS NOT NULL FROM sources ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var address, route string
		var isDeleted bool
		if err := rows.Scan(&id, &address, &route, &isDeleted); err != nil {
			return nil, err
		}
		idx.add(id, address, route, isDeleted)
	}
	return idx, rows.Err()
}

func (idx *DuplicateIndex) add(id int64, address, route string, isDeleted bool) {
	for _, key := range append(sourceKeys(address, route, idx.roots), exactKey(address, route)) {
		if !isDeleted {
			if _, ok := idx.live[key]; !ok {
				idx.live[key] = id
			}
		} else {
			idx.deleted[key] = append(idx.deleted[key], id)
		}
	}
}

// Add records a subscription that was just created.
func (idx *DuplicateIndex) Add(id int64, src NewSource) { idx.add(id, src.URL, src.RSSHubPath, false) }

// Find returns the oldest live subscription that src repeats, by sourceKeys.
func (idx *DuplicateIndex) Find(src NewSource) (id int64, ok bool) {
	for _, key := range sourceKeys(src.URL, src.RSSHubPath, idx.roots) {
		if have, found := idx.live[key]; found && (!ok || have < id) {
			id, ok = have, true
		}
	}
	return id, ok
}

// deletedLike lists the deleted subscriptions that src would clash with and marks them purged.
func (idx *DuplicateIndex) deletedLike(src NewSource) []int64 {
	var ids []int64
	for _, key := range append(sourceKeys(src.URL, src.RSSHubPath, idx.roots), exactKey(src.URL, src.RSSHubPath)) {
		for _, id := range idx.deleted[key] {
			if !idx.gone[id] {
				idx.gone[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// FindDuplicate returns the id of the live subscription that src repeats, by sourceKeys, or ok false when there is
// none. When several match, it is the oldest.
func (s *Store) FindDuplicate(ctx context.Context, src NewSource) (id int64, ok bool, err error) {
	idx, err := s.DuplicateIndex(ctx)
	if err != nil {
		return 0, false, err
	}
	id, ok = idx.Find(src)
	return id, ok, nil
}
