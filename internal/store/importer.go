package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
)

// SourceIDByFeedURL returns the id of the subscription whose feed address equals raw by feedURLKey, or ErrNotFound.
func (s *Store) SourceIDByFeedURL(ctx context.Context, raw string) (int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, url FROM sources WHERE url <> '' AND deleted_at IS NULL ORDER BY id")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	want := feedURLKey(raw)
	for rows.Next() {
		var id int64
		var have string
		if err := rows.Scan(&id, &have); err != nil {
			return 0, err
		}
		if feedURLKey(have) == want {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return 0, ErrNotFound
}

// ImportedEntry is an entry read from another reader. Read and Starred are its state there.
type ImportedEntry struct {
	Title       string
	URL         string
	Author      string
	PublishedAt time.Time
	Summary     string
	ContentHTML string
	Read        bool
	Starred     bool
}

// The outcomes of importing one entry.
const (
	ImportAdded     = "added"     // a starred entry that was not stored and now is
	ImportExisting  = "existing"  // the entry was stored already
	ImportUnmatched = "unmatched" // a read entry that is not stored; nothing was written
	ImportInvalid   = "invalid"   // the entry has no usable link; nothing was written
)

// ImportResult is what ImportEntries did with one entry. StarSet and ReadSet say the state was changed, which is
// never so when it already held.
type ImportResult struct {
	Outcome string
	StarSet bool
	ReadSet bool
}

// importGUID is the GUID an imported entry is stored under: its link as the feed parser normalises it when an item has
// no GUID of its own (lowercase host, no trailing slash), so the same item fetched later finds it.
func importGUID(link string) string {
	u, err := url.Parse(link)
	if err != nil || link == "" {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// ImportEntries applies the state of entries read from another reader to the entries of one source, in one
// transaction, and returns one result for each, in order.
// An entry matches a stored one by its GUID, which for an import is its normalised link, or else by link key when
// the source has exactly one entry with that link. A match gets the star and the read mark it has there, never losing
// one it has here. A starred entry without a match is stored, with its content, under the source; the source's own
// fetch can take it over later. A read entry without a match is left out: it is not stored just to be marked.
// New entries are dated by their publication, not later than now, and get no AI results.
// Running it again with the same entries changes nothing.
func (s *Store) ImportEntries(ctx context.Context, sourceID int64, entries []ImportedEntry, now time.Time) ([]ImportResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRowContext(ctx, "SELECT kind FROM sources WHERE id = ?", sourceID).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	extract := "skipped"
	if kind == "article" {
		extract = "pending"
	}
	var stored map[string][]linkedEntry
	results := make([]ImportResult, len(entries))
	for i, e := range entries {
		link := strings.TrimSpace(e.URL)
		guid, key := importGUID(link), linkKey(link)
		if guid == "" || key == "" {
			results[i].Outcome = ImportInvalid
			continue
		}
		var id int64
		var read, starred bool
		err := tx.QueryRowContext(ctx, "SELECT id, is_read, starred FROM entries WHERE source_id = ? AND guid = ?",
			sourceID, guid).Scan(&id, &read, &starred)
		if errors.Is(err, sql.ErrNoRows) {
			if stored == nil {
				if stored, err = sourceLinks(ctx, tx, sourceID); err != nil {
					return nil, err
				}
			}
			err = sql.ErrNoRows
			if same := stored[key]; len(same) == 1 {
				err = tx.QueryRowContext(ctx, "SELECT id, is_read, starred FROM entries WHERE id = ?", same[0].id).
					Scan(&id, &read, &starred)
			}
		}
		switch {
		case err == nil:
			results[i].Outcome = ImportExisting
			if e.Starred && !starred {
				if _, err := tx.ExecContext(ctx, "UPDATE entries SET starred = 1 WHERE id = ?", id); err != nil {
					return nil, err
				}
				results[i].StarSet = true
			}
			if e.Read && !read {
				if _, err := tx.ExecContext(ctx, "UPDATE entries SET is_read = 1, read_at = ? WHERE id = ?", now.UnixMilli(), id); err != nil {
					return nil, err
				}
				results[i].ReadSet = true
			}
		case !errors.Is(err, sql.ErrNoRows):
			return nil, err
		case !e.Starred:
			results[i].Outcome = ImportUnmatched
		default:
			created := now
			if !e.PublishedAt.IsZero() && e.PublishedAt.Before(now) {
				created = e.PublishedAt
			}
			var readAt any
			if e.Read {
				readAt = now.UnixMilli()
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO entries (source_id, guid, title, url, author, published_at, rss_summary, rss_content_html,
					extract_status, images, reading_sec, is_read, read_at, starred, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, 1, ?, ?)
				ON CONFLICT (source_id, guid) DO NOTHING`,
				sourceID, guid, e.Title, link, e.Author, nullTime(e.PublishedAt), e.Summary, e.ContentHTML, extract,
				readingValue(kind, 0, "", e.ContentHTML, e.Summary), e.Read, readAt, created.UnixMilli(), now.UnixMilli(),
			); err != nil {
				return nil, err
			}
			results[i] = ImportResult{Outcome: ImportAdded, StarSet: true, ReadSet: e.Read}
		}
	}
	return results, tx.Commit()
}
