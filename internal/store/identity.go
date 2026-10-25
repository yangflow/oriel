package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"sort"
	"strings"

	"modernc.org/sqlite"

	"github.com/yangflow/oriel/internal/tracker"
)

// linkKey is how two entry links of one source are compared: lowercase scheme and host, no fragment, no trailing
// slash, no tracking parameters (tracker.StripLink). It is empty for a link that is empty or does not parse.
func linkKey(link string) string {
	u, err := url.Parse(tracker.StripLink(strings.TrimSpace(link)))
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

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("group_key", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		link, _ := args[0].(string)
		guid, _ := args[1].(string)
		return groupKey(link, guid), nil
	})
}

// groupKey is how entries of any sources are compared to show one article once (docs/product.md "同一篇文章只显示一次").
// It is the link, or the GUID when there is no link and the GUID is an http or https address, with the scheme ignored,
// the host lowercase without "www." and the default port, no fragment, no trailing slash on the path, tracking
// parameters dropped and the other parameters sorted. It is empty when there is nothing to compare: no usable link, or
// only a host (a homepage), which many feeds give to every item.
func groupKey(link, guid string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		guid = strings.TrimSpace(guid)
		if lower := strings.ToLower(guid); strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			link = guid
		}
	}
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
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
	path := strings.TrimRight(u.Path, "/")
	query := url.Values{}
	for name, values := range u.Query() {
		for _, value := range values {
			if !tracker.Param(name, value) {
				query[name] = append(query[name], value)
			}
		}
	}
	if path == "" && len(query) == 0 {
		return ""
	}
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(host)
	b.WriteString(path)
	for i, name := range names {
		if i == 0 {
			b.WriteByte('?')
		} else {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(name))
		values := query[name]
		sort.Strings(values)
		for j, value := range values {
			if j > 0 {
				b.WriteByte('&')
				b.WriteString(url.QueryEscape(name))
			}
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(value))
		}
	}
	return b.String()
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
