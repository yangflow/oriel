package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// MuteSetting is the setting that holds the global muted keywords, one per line. They hide entries of every source,
// together with each source's own. See docs/product.md "屏蔽词".
const MuteSetting = "mute_keywords"

// The limits of rules and of the global muted keywords.
const (
	MaxRules        = 50
	MaxRuleKeywords = 20
	MaxRuleSources  = 50
	MaxMuteKeywords = 200
	MaxLineRunes    = 100
)

// The errors of a rule that cannot be saved; the server turns each into its code.
var (
	ErrRuleSource      = errors.New("rule source does not exist")
	ErrRuleSources     = errors.New("rule has too many sources")
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
	SourceIDs []int64 // none is every source
	Kind      string  // "" is any kind
	Keywords  string  // one per line; an entry has to contain one of them
	Exclude   string  // one per line; an entry must contain none of them
	Author    string
	Star      bool
	MarkRead  bool
	Enabled   bool
	CreatedAt time.Time
}

// RulePatch holds the fields of a rule a request changes; nil leaves a field as it is. A SourceIDs that points to an
// empty list sets the rule to every source.
type RulePatch struct {
	SourceIDs *[]int64
	Kind      *string
	Keywords  *string
	Exclude   *string
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

const ruleColumns = "id, kind, keywords, exclude_keywords, author, star, mark_read, enabled, created_at"

func scanRule(row interface{ Scan(...any) error }) (Rule, error) {
	var r Rule
	var created int64
	if err := row.Scan(&r.ID, &r.Kind, &r.Keywords, &r.Exclude, &r.Author, &r.Star, &r.MarkRead, &r.Enabled, &created); err != nil {
		return Rule{}, err
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return rules, loadRuleSources(ctx, s.db, rules)
}

// loadRuleSources fills in the sources of the rules, in the order of their IDs.
func loadRuleSources(ctx context.Context, q querier, rules []Rule) error {
	rows, err := q.QueryContext(ctx, "SELECT rule_id, source_id FROM rule_sources ORDER BY source_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	at := make(map[int64]int, len(rules))
	for i, r := range rules {
		at[r.ID] = i
	}
	for rows.Next() {
		var rule, source int64
		if err := rows.Scan(&rule, &source); err != nil {
			return err
		}
		if i, ok := at[rule]; ok {
			rules[i].SourceIDs = append(rules[i].SourceIDs, source)
		}
	}
	return rows.Err()
}

// rowQuerier is what a transaction and the database share for reading one row.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Rule returns one rule, or ErrNotFound.
func (s *Store) Rule(ctx context.Context, id int64) (Rule, error) {
	return ruleByID(ctx, s.db, id)
}

func ruleByID(ctx context.Context, q rowQuerier, id int64) (Rule, error) {
	r, err := scanRule(q.QueryRowContext(ctx, "SELECT "+ruleColumns+" FROM rules WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrNotFound
	} else if err != nil {
		return Rule{}, err
	}
	rows, err := q.QueryContext(ctx, "SELECT source_id FROM rule_sources WHERE rule_id = ? ORDER BY source_id", id)
	if err != nil {
		return Rule{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var source int64
		if err := rows.Scan(&source); err != nil {
			return Rule{}, err
		}
		r.SourceIDs = append(r.SourceIDs, source)
	}
	return r, rows.Err()
}

// checkConditions validates the conditions of a rule and returns it with its text fields cleaned. A source inside its
// undo window still counts: it comes back, or its rules go with it when it is purged.
func checkConditions(ctx context.Context, q rowQuerier, r Rule) (Rule, error) {
	slices.Sort(r.SourceIDs)
	r.SourceIDs = slices.Compact(r.SourceIDs)
	if len(r.SourceIDs) > MaxRuleSources {
		return Rule{}, ErrRuleSources
	}
	for _, id := range r.SourceIDs {
		var one int
		err := q.QueryRowContext(ctx, "SELECT 1 FROM sources WHERE id = ?", id).Scan(&one)
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
	r.Exclude = CleanLines(r.Exclude)
	if !LinesWithin(r.Exclude, MaxRuleKeywords) {
		return Rule{}, ErrRuleKeywords
	}
	r.Author = strings.TrimSpace(r.Author)
	if utf8.RuneCountInString(r.Author) > MaxLineRunes {
		return Rule{}, ErrRuleAuthor
	}
	// Words to leave out are not a condition: with only those the rule would match nearly every entry.
	if len(r.SourceIDs) == 0 && r.Kind == "" && r.Keywords == "" && r.Author == "" {
		return Rule{}, ErrRuleNoCondition
	}
	return r, nil
}

// checkRule validates a whole rule: its conditions and that it has an action.
func checkRule(ctx context.Context, q rowQuerier, r Rule) (Rule, error) {
	r, err := checkConditions(ctx, q, r)
	if err != nil {
		return Rule{}, err
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
		INSERT INTO rules (id, kind, keywords, exclude_keywords, author, star, mark_read, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, r.Kind, r.Keywords, r.Exclude, r.Author, r.Star, r.MarkRead, r.Enabled, now.UnixMilli())
	if err != nil {
		return Rule{}, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return Rule{}, err
	}
	if err := setRuleSources(ctx, tx, newID, r.SourceIDs); err != nil {
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
	if p.SourceIDs != nil {
		r.SourceIDs = *p.SourceIDs
	}
	if p.Kind != nil {
		r.Kind = *p.Kind
	}
	if p.Keywords != nil {
		r.Keywords = *p.Keywords
	}
	if p.Exclude != nil {
		r.Exclude = *p.Exclude
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
		UPDATE rules SET kind = ?, keywords = ?, exclude_keywords = ?, author = ?, star = ?, mark_read = ?, enabled = ?
		WHERE id = ?`, r.Kind, r.Keywords, r.Exclude, r.Author, r.Star, r.MarkRead, r.Enabled, id); err != nil {
		return Rule{}, err
	}
	if err := setRuleSources(ctx, tx, id, r.SourceIDs); err != nil {
		return Rule{}, err
	}
	updated, err := ruleByID(ctx, tx, id)
	if err != nil {
		return Rule{}, err
	}
	return updated, tx.Commit()
}

// setRuleSources makes the sources of a rule exactly ids, which are checked and have no duplicates.
func setRuleSources(ctx context.Context, tx *sql.Tx, rule int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM rule_sources WHERE rule_id = ?", rule); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT INTO rule_sources (rule_id, source_id) VALUES (?, ?)", rule, id); err != nil {
			return err
		}
	}
	return nil
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

// sourceRules loads the enabled rules that can apply to the entries of one source. Their SourceIDs are not filled in:
// the query has already chosen by source.
func sourceRules(ctx context.Context, q querier, sourceID int64, kind string) ([]Rule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+ruleColumns+` FROM rules
		WHERE enabled = 1 AND (kind = '' OR kind = ?)
		AND (NOT EXISTS (SELECT 1 FROM rule_sources WHERE rule_id = rules.id)
			OR EXISTS (SELECT 1 FROM rule_sources WHERE rule_id = rules.id AND source_id = ?))`, kind, sourceID)
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
	for _, r := range rules {
		if r.Keywords != "" && !muted(r.Keywords, e.Title, e.Summary) {
			continue
		}
		if r.Exclude != "" && muted(r.Exclude, e.Title, e.Summary) {
			continue
		}
		if r.Author != "" && !containsFold(e.Author, r.Author) {
			continue
		}
		star, read = star || r.Star, read || r.MarkRead
	}
	return star, read
}

// RuleMatchCount says how many entries of the live subscriptions the conditions of r match now, starred or read or
// not: the same match as for a new entry, but only counted. The rule need not be saved, so the dialog can show the
// effect of an edit. Without a condition it is 0; the actions of r are not looked at.
func (s *Store) RuleMatchCount(ctx context.Context, r Rule) (int, error) {
	r, err := checkConditions(ctx, s.db, r)
	if errors.Is(err, ErrRuleNoCondition) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	query := `SELECT count(*) FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE s.deleted_at IS NULL AND (?1 = '' OR muted(?1, e.title, e.rss_summary))
		AND NOT muted(?2, e.title, e.rss_summary) AND (?3 = '' OR contains_fold(e.author, ?3))`
	args := []any{r.Keywords, r.Exclude, r.Author}
	if r.Kind != "" {
		query += " AND s.kind = ?"
		args = append(args, r.Kind)
	}
	if len(r.SourceIDs) > 0 {
		query += " AND e.source_id IN (?" + strings.Repeat(",?", len(r.SourceIDs)-1) + ")"
		for _, id := range r.SourceIDs {
			args = append(args, id)
		}
	}
	var n int
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("contains_fold", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		text, _ := args[0].(string)
		part, _ := args[1].(string)
		return containsFold(text, part), nil
	})
}

// containsFold reports whether text contains part, ignoring case.
func containsFold(text, part string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(part))
}
