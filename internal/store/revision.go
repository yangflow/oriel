package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/yangflow/oriel/internal/tracker"
)

// Revision is the version of an entry from before its latest substantive update, next to the version it has now. Both
// are reduced to a title and paragraphs of text, so a client can compare them. See docs/product.md "条目的修订".
type Revision struct {
	// FetchedAt is when the earlier version was fetched, ReplacedAt when the update that replaced it was.
	FetchedAt, ReplacedAt time.Time
	Title                 string
	Paragraphs            []string
	CurrentTitle          string
	CurrentParagraphs     []string
}

// ErrNoRevision is returned for an entry that has no earlier version stored.
var ErrNoRevision = errors.New("store: no revision")

// blockElements end the paragraph being collected.
var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Li: true, atom.Ul: true, atom.Ol: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Blockquote: true,
	atom.Pre: true, atom.Br: true, atom.Hr: true, atom.Tr: true, atom.Table: true, atom.Figure: true,
	atom.Figcaption: true, atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true,
	atom.Aside: true, atom.Details: true, atom.Summary: true,
}

// paragraphs splits an HTML fragment into the paragraphs of its text: block elements and line breaks end one, white
// space collapses, empty ones go, and scripts, styles and tracking pictures do not count.
func paragraphs(fragment string) []string {
	if strings.TrimSpace(fragment) == "" {
		return nil
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return nil
	}
	var out []string
	var current strings.Builder
	flush := func() {
		if text := strings.Join(strings.Fields(current.String()), " "); text != "" {
			out = append(out, text)
		}
		current.Reset()
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			current.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Svg:
				return
			case atom.Img:
				src := ""
				for _, a := range n.Attr {
					if a.Key == "src" {
						src = a.Val
					}
				}
				if tracker.Counts(n, src) {
					return
				}
				current.WriteByte(' ')
				return
			}
			if blockElements[n.DataAtom] {
				flush()
				defer flush()
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	flush()
	return out
}

// bodyParagraphs is the paragraphs of the feed's content, or of its summary when the content has none.
func bodyParagraphs(contentHTML, summary string) []string {
	if p := paragraphs(contentHTML); len(p) > 0 {
		return p
	}
	return paragraphs(summary)
}

func normalTitle(title string) string { return strings.Join(strings.Fields(title), " ") }

// substantiveChange reports whether the title or the text differs once white space and tracking pictures are left out.
func substantiveChange(oldTitle, oldContent, oldSummary, newTitle, newContent, newSummary string) bool {
	if normalTitle(oldTitle) != normalTitle(newTitle) {
		return true
	}
	a, b := bodyParagraphs(oldContent, oldSummary), bodyParagraphs(newContent, newSummary)
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// revisionReader reads the stored version of an entry and keeps it as its revision when the new one differs.
type revisionReader struct {
	read, write *sql.Stmt
}

func newRevisionReader(ctx context.Context, tx *sql.Tx) (*revisionReader, error) {
	read, err := tx.PrepareContext(ctx, "SELECT title, rss_summary, rss_content_html, updated_at FROM entries WHERE source_id = ? AND guid = ?")
	if err != nil {
		return nil, err
	}
	write, err := tx.PrepareContext(ctx, `
		INSERT INTO entry_revisions (entry_id, title, summary, content_html, fetched_at, replaced_at)
		SELECT id, ?, ?, ?, ?, ? FROM entries WHERE source_id = ? AND guid = ?
		ON CONFLICT (entry_id) DO UPDATE SET title = excluded.title, summary = excluded.summary,
			content_html = excluded.content_html, fetched_at = excluded.fetched_at, replaced_at = excluded.replaced_at`)
	if err != nil {
		read.Close()
		return nil, err
	}
	return &revisionReader{read: read, write: write}, nil
}

func (r *revisionReader) close() {
	r.read.Close()
	r.write.Close()
}

// keep stores the entry's current version as its revision when the item differs from it in substance. Call it before
// the item is written over the entry; an entry that does not exist yet has nothing to keep.
func (r *revisionReader) keep(ctx context.Context, sourceID int64, e FeedEntry, now time.Time) error {
	var title, summary, content string
	var updated int64
	err := r.read.QueryRowContext(ctx, sourceID, e.GUID).Scan(&title, &summary, &content, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !substantiveChange(title, content, summary, e.Title, e.ContentHTML, e.Summary) {
		return nil
	}
	_, err = r.write.ExecContext(ctx, title, summary, content, updated, now.UnixMilli(), sourceID, e.GUID)
	return err
}

// Revision returns the earlier version of an entry beside its current one; ErrNoRevision when it was never updated in
// substance, ErrNotFound when the entry does not exist.
func (s *Store) Revision(ctx context.Context, id int64) (Revision, error) {
	var rev Revision
	var content, summary string
	var fetched, replaced sql.NullInt64
	var oldTitle sql.NullString
	var oldSummaryN, oldContentN sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT e.title, e.rss_summary, e.rss_content_html, r.title, r.summary, r.content_html, r.fetched_at, r.replaced_at
		FROM entries e JOIN sources s ON s.id = e.source_id LEFT JOIN entry_revisions r ON r.entry_id = e.id
		WHERE e.id = ? AND s.deleted_at IS NULL`, id).
		Scan(&rev.CurrentTitle, &summary, &content, &oldTitle, &oldSummaryN, &oldContentN, &fetched, &replaced)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	if err != nil {
		return Revision{}, err
	}
	if !fetched.Valid {
		return Revision{}, ErrNoRevision
	}
	rev.FetchedAt, rev.ReplacedAt = time.UnixMilli(fetched.Int64), time.UnixMilli(replaced.Int64)
	rev.Title = oldTitle.String
	rev.Paragraphs = bodyParagraphs(oldContentN.String, oldSummaryN.String)
	rev.CurrentParagraphs = bodyParagraphs(content, summary)
	return rev, nil
}
