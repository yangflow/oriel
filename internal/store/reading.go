package store

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	// charsPerMinute is the reading speed for Han characters and kana, wordsPerMinute for every other script.
	charsPerMinute = 400
	wordsPerMinute = 220

	readingBackfillKey   = "reading_backfill_id"
	readingBackfillBatch = 200
	readingBackfillPause = 50 * time.Millisecond
)

// readingSeconds estimates the time an entry takes, see docs/product.md "阅读时间". The second result is false when
// there is nothing to estimate. searchText is the extracted text and must be empty unless the extraction succeeded.
func readingSeconds(kind string, durationSec int, searchText, contentHTML, summary string) (int, bool) {
	if durationSec > 0 {
		return durationSec, true
	}
	if kind == "video" || kind == "image" {
		return 0, false
	}
	var chars, words int
	for _, source := range []struct {
		text string
		html bool
	}{{searchText, false}, {contentHTML, true}, {summary, true}} {
		text := source.text
		if source.html {
			text = plainText(text)
		}
		if chars, words = countReading(text); chars+words > 0 {
			break
		}
	}
	if chars+words == 0 {
		return 0, false
	}
	seconds := math.Round(float64(chars)*60/charsPerMinute + float64(words)*60/wordsPerMinute)
	if seconds < 60 {
		return int(seconds), true
	}
	return int(math.Round(seconds/60)) * 60, true
}

// readingValue is readingSeconds as a value for a nullable column.
func readingValue(kind string, durationSec int, searchText, contentHTML, summary string) sql.NullInt64 {
	seconds, ok := readingSeconds(kind, durationSec, searchText, contentHTML, summary)
	return sql.NullInt64{Int64: int64(seconds), Valid: ok}
}

// countReading counts the Han characters and kana, and the words made of other letters or digits.
func countReading(text string) (chars, words int) {
	inWord := false
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
			chars++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inWord {
				words++
			}
			inWord = true
		case unicode.IsMark(r):
		default:
			inWord = false
		}
	}
	return chars, words
}

// plainText turns an HTML fragment into text, leaving out scripts and styles. Block elements separate words.
func plainText(fragment string) string {
	if !strings.ContainsAny(fragment, "<&") {
		return fragment
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Svg:
				return
			case atom.Img:
				b.WriteByte(' ')
				return
			case atom.Span, atom.A, atom.B, atom.I, atom.Em, atom.Strong, atom.Code, atom.Sup, atom.Sub, atom.Small, atom.Mark, atom.U:
			default:
				b.WriteByte(' ')
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode {
			b.WriteByte(' ')
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return b.String()
}

// BackfillReadingTime fills reading_sec of the entries that were stored before the column existed, in batches
// ordered by ID with a pause between them. The last ID done is kept in the settings, so a restart carries on.
func (s *Store) BackfillReadingTime(ctx context.Context) error {
	for {
		n, err := s.backfillReadingBatch(ctx, readingBackfillBatch)
		if err != nil || n < readingBackfillBatch {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readingBackfillPause):
		}
	}
}

// backfillReadingBatch handles the next size entries after the stored progress and returns how many it saw.
func (s *Store) backfillReadingBatch(ctx context.Context, size int) (int, error) {
	value, err := s.Setting(ctx, readingBackfillKey)
	if err != nil {
		return 0, err
	}
	last, _ := strconv.ParseInt(value, 10, 64)
	type update struct {
		id      int64
		seconds sql.NullInt64
	}
	var updates []update
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, duration_sec, extract_status, search_text, rss_content_html, rss_summary
		FROM entries WHERE id > ? ORDER BY id LIMIT ?`, last, size)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kind, status, searchText, content, summary string
		var duration sql.NullInt64
		if err := rows.Scan(&id, &kind, &duration, &status, &searchText, &content, &summary); err != nil {
			return 0, err
		}
		if status != "ok" {
			searchText = ""
		}
		updates = append(updates, update{id, readingValue(kind, int(duration.Int64), searchText, content, summary)})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	if len(updates) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, u := range updates {
		// An entry written or extracted since the migration already has its own value.
		if _, err := tx.ExecContext(ctx, "UPDATE entries SET reading_sec = ? WHERE id = ? AND reading_sec IS NULL", u.seconds, u.id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		readingBackfillKey, strconv.FormatInt(updates[len(updates)-1].id, 10)); err != nil {
		return 0, err
	}
	return len(updates), tx.Commit()
}
