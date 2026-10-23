package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// MuteSetting is the setting that holds the global muted keywords, one per line. They hide entries of every source,
// together with each source's own. See docs/product.md "屏蔽词".
const MuteSetting = "mute_keywords"

// The limits of rules and of the global muted keywords.
const (
	MaxRules        = 50
	MaxRuleKeywords = 20
	MaxMuteKeywords = 200
	MaxLineRunes    = 100
)

// The errors of a rule that cannot be saved; the server turns each into its code.
var (
	ErrRuleSource      = errors.New("rule source does not exist")
	ErrRuleKind        = errors.New("rule kind is not a kind")
	ErrRuleKeywords    = errors.New("rule keywords are too many or too long")
	ErrRuleAuthor      = errors.New("rule author is too long")
	ErrRuleNoCondition = errors.New("rule has no condition")
	ErrRuleNoAction    = errors.New("rule has no action")
	ErrTooManyRules    = errors.New("too many rules")
	ErrMuteKeywords    = errors.New("muted keywords are too many or too long")
)

// Rule stars or marks as read the new entries that match all its conditions. See docs/product.md "规则".
type Rule struct {
	ID        int64
	SourceID  *int64 // nil is every source
	Kind      string // "" is any kind
	Keywords  string // one per line
	Author    string
	Star      bool
	MarkRead  bool
	Enabled   bool
	CreatedAt time.Time
}

// RulePatch holds the fields of a rule a request changes; nil leaves a field as it is. SetSource tells a source
// that is set to nil (every source) from one that is not in the request.
type RulePatch struct {
	SetSource bool
	SourceID  *int64
	Kind      *string
	Keywords  *string
	Author    *string
	Star      *bool
	MarkRead  *bool
	Enabled   *bool
}

// CleanLines trims every line and drops the empty ones.
func CleanLines(text string) string {
	var lines []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// LinesWithin reports whether the text, cleaned, has at most n lines of at most MaxLineRunes characters.
func LinesWithin(text string, n int) bool {
	count := 0
	for line := range strings.Lines(CleanLines(text)) {
		if count++; count > n || utf8.RuneCountInString(strings.TrimRight(line, "\n")) > MaxLineRunes {
			return false
		}
	}
	return true
}

const ruleColumns = "id, source_id, kind, keywords, author, star, mark_read, enabled, created_at"

func scanRule(row interface{ Scan(...any) error }) (Rule, error) {
	var r Rule
	var source sql.NullInt64
	var created int64
	if err := row.Scan(&r.ID, &source, &r.Kind, &r.Keywords, &r.Author, &r.Star, &r.MarkRead, &r.Enabled, &created); err != nil {
		return Rule{}, err
	}
	if source.Valid {
		r.SourceID = &source.Int64
	}
	r.CreatedAt = time.UnixMilli(created)
	return r, nil
}

// Rules lists every rule, oldest first.
func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+ruleColumns+" FROM rules ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// rowQuerier is what a transaction and the database share for reading one row.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Rule returns one rule, or ErrNotFound.
func (s *Store) Rule(ctx context.Context, id int64) (Rule, error) {
	return ruleByID(ctx, s.db, id)
}

func ruleByID(ctx context.Context, q rowQuerier, id int64) (Rule, error) {
	r, err := scanRule(q.QueryRowContext(ctx, "SELECT "+ruleColumns+" FROM rules WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrNotFound
	}
	return r, err
}

// checkRule validates a whole rule and returns it with its text fields cleaned. A source inside its undo window
// still counts: it comes back, or its rules go with it when it is purged.
func checkRule(ctx context.Context, q rowQuerier, r Rule) (Rule, error) {
	if r.SourceID != nil {
		var one int
		err := q.QueryRowContext(ctx, "SELECT 1 FROM sources WHERE id = ?", *r.SourceID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return Rule{}, ErrRuleSource
		} else if err != nil {
			return Rule{}, err
		}
	}
	switch r.Kind {
	case "", "article", "video", "social", "image":
	default:
		return Rule{}, ErrRuleKind
	}
	r.Keywords = CleanLines(r.Keywords)
	if !LinesWithin(r.Keywords, MaxRuleKeywords) {
		return Rule{}, ErrRuleKeywords
	}
	r.Author = strings.TrimSpace(r.Author)
	if utf8.RuneCountInString(r.Author) > MaxLineRunes {
		return Rule{}, ErrRuleAuthor
	}
	if r.SourceID == nil && r.Kind == "" && r.Keywords == "" && r.Author == "" {
		return Rule{}, ErrRuleNoCondition
	}
	if !r.Star && !r.MarkRead {
		return Rule{}, ErrRuleNoAction
	}
	return r, nil
}

// CreateRule validates and adds a rule, and returns it with its ID. A rule with a nonzero ID is a restored one: it
// keeps that ID when it is free, and now is its original creation time, so it takes its old place in the list.
func (s *Store) CreateRule(ctx context.Context, r Rule, now time.Time) (Rule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Rule{}, err
	}
	defer tx.Rollback()
	if r, err = checkRule(ctx, tx, r); err != nil {
		return Rule{}, err
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM rules").Scan(&n); err != nil {
		return Rule{}, err
	}
	if n >= MaxRules {
		return Rule{}, ErrTooManyRules
	}
	var id any // NULL lets SQLite pick one
	if r.ID != 0 {
		var taken int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM rules WHERE id = ?", r.ID).Scan(&taken)
		if errors.Is(err, sql.ErrNoRows) {
			id = r.ID
		} else if err != nil {
			return Rule{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO rules (id, source_id, kind, keywords, author, star, mark_read, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, r.SourceID, r.Kind, r.Keywords, r.Author, r.Star, r.MarkRead, r.Enabled, now.UnixMilli())
	if err != nil {
		return Rule{}, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return Rule{}, err
	}
	created, err := ruleByID(ctx, tx, newID)
	if err != nil {
		return Rule{}, err
	}
	return created, tx.Commit()
}

// UpdateRule changes the fields of a rule the patch holds; the result as a whole must still be valid.
func (s *Store) UpdateRule(ctx context.Context, id int64, p RulePatch) (Rule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Rule{}, err
	}
	defer tx.Rollback()
	r, err := ruleByID(ctx, tx, id)
	if err != nil {
		return Rule{}, err
	}
	if p.SetSource {
		r.SourceID = p.SourceID
	}
	if p.Kind != nil {
		r.Kind = *p.Kind
	}
	if p.Keywords != nil {
		r.Keywords = *p.Keywords
	}
	if p.Author != nil {
		r.Author = *p.Author
	}
	if p.Star != nil {
		r.Star = *p.Star
	}
	if p.MarkRead != nil {
		r.MarkRead = *p.MarkRead
	}
	if p.Enabled != nil {
		r.Enabled = *p.Enabled
	}
	if r, err = checkRule(ctx, tx, r); err != nil {
		return Rule{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE rules SET source_id = ?, kind = ?, keywords = ?, author = ?, star = ?, mark_read = ?, enabled = ?
		WHERE id = ?`, r.SourceID, r.Kind, r.Keywords, r.Author, r.Star, r.MarkRead, r.Enabled, id); err != nil {
		return Rule{}, err
	}
	updated, err := ruleByID(ctx, tx, id)
	if err != nil {
		return Rule{}, err
	}
	return updated, tx.Commit()
}

// DeleteRule removes a rule, or returns ErrNotFound. Entries it acted on keep what it did.
func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM rules WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// querier is what a transaction and the database share.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// sourceRules loads the enabled rules that can apply to the entries of one source.
func sourceRules(ctx context.Context, q querier, sourceID int64, kind string) ([]Rule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+ruleColumns+` FROM rules
		WHERE enabled = 1 AND (source_id IS NULL OR source_id = ?) AND (kind = '' OR kind = ?)`, sourceID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// ruleActions is what the rules do to a new entry: the union of the actions of every rule it matches.
func ruleActions(rules []Rule, e FeedEntry) (star, read bool) {
	author := strings.ToLower(e.Author)
	for _, r := range rules {
		if r.Keywords != "" && !muted(r.Keywords, e.Title, e.Summary) {
			continue
		}
		if r.Author != "" && !strings.Contains(author, strings.ToLower(r.Author)) {
			continue
		}
		star, read = star || r.Star, read || r.MarkRead
	}
	return star, read
}
