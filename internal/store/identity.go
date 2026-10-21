package store

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
)

// linkKey is how two entry links of one source are compared: lowercase scheme and host, no fragment, no trailing
// slash. It is empty for a link that is empty or does not parse.
func linkKey(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Host == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String()
}

// feedURLKey is how two feed addresses are compared: lowercase scheme and host, default port dropped, no fragment,
// and a trailing slash on the path ignored, so an empty path equals "/". The stored address is never rewritten.
// An address that does not parse is compared as it is.
func feedURLKey(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Host = host
	if port != "" {
		u.Host += ":" + port
	}
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String()
}

// feedURLExists reports whether a source already has the same feed address as raw, compared with feedURLKey.
func (s *Store) feedURLExists(ctx context.Context, raw string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT url FROM sources WHERE url <> ''")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	want := feedURLKey(raw)
	for rows.Next() {
		var have string
		if err := rows.Scan(&have); err != nil {
			return false, err
		}
		if feedURLKey(have) == want {
			return true, nil
		}
	}
	return false, rows.Err()
}

// sourceLinks groups the entries of one source by link key, so an item with a new GUID can find the entry it
// really is. Entries without a usable link are left out.
func sourceLinks(ctx context.Context, tx *sql.Tx, sourceID int64) (map[string][]linkedEntry, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, guid, url FROM entries WHERE source_id = ? AND url <> ''", sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := map[string][]linkedEntry{}
	for rows.Next() {
		var e linkedEntry
		var link string
		if err := rows.Scan(&e.id, &e.guid, &link); err != nil {
			return nil, err
		}
		if key := linkKey(link); key != "" {
			links[key] = append(links[key], e)
		}
	}
	return links, rows.Err()
}

type linkedEntry struct {
	id   int64
	guid string
}
