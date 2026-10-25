package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

func entryByGUID(t *testing.T, s *Store, sourceID int64, guid string) Entry {
	t.Helper()
	var id int64
	if err := s.db.QueryRowContext(context.Background(), "SELECT id FROM entries WHERE source_id = ? AND guid = ?", sourceID, guid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	e, err := s.Entry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func guidOf(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var guid string
	if err := s.db.QueryRowContext(context.Background(), "SELECT guid FROM entries WHERE id = ?", id).Scan(&guid); err != nil {
		t.Fatal(err)
	}
	return guid
}

func saveItems(t *testing.T, s *Store, src Source, items ...FeedEntry) {
	t.Helper()
	if _, err := s.SaveFeed(context.Background(), FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestRulesActOnNewEntriesOnly(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	b := newLogSource(t, s, "https://b.example/feed")
	saveItems(t, s, a, FeedEntry{GUID: "old", Title: "Go old"})

	star, err := s.CreateRule(ctx, Rule{Keywords: "go\nrust", Star: true, Enabled: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{a.ID}, Author: "ALICE", MarkRead: true, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRule(ctx, Rule{Kind: "video", Keywords: "clip", MarkRead: true, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	off, err := s.CreateRule(ctx, Rule{Keywords: "never", Star: true}, time.Now())
	if err != nil || off.Enabled {
		t.Fatalf("%+v %v", off, err)
	}

	saveItems(t, s, a,
		FeedEntry{GUID: "old", Title: "Go old"},
		FeedEntry{GUID: "go", Title: "Go 2", Author: "Bob"},
		FeedEntry{GUID: "both", Summary: "about RUST", Author: "alice smith"},
		FeedEntry{GUID: "plain", Title: "Plain", Author: "Bob"},
		FeedEntry{GUID: "never", Title: "never"},
		FeedEntry{GUID: "clip", Title: "clip"},
	)
	saveItems(t, s, b, FeedEntry{GUID: "go", Title: "Go in b", Author: "Alice"})

	for _, c := range []struct {
		src          int64
		guid         string
		star, isRead bool
	}{
		{a.ID, "old", false, false},
		{a.ID, "go", true, false},
		{a.ID, "both", true, true},
		{a.ID, "plain", false, false},
		{a.ID, "never", false, false},
		{a.ID, "clip", false, false}, // the kind of the source is article
		{b.ID, "go", true, false},    // the author rule belongs to source a
	} {
		e := entryByGUID(t, s, c.src, c.guid)
		if e.Starred != c.star || e.IsRead != c.isRead {
			t.Errorf("%d/%s: starred %v read %v", c.src, c.guid, e.Starred, e.IsRead)
		}
	}
	if e := entryByGUID(t, s, a.ID, "both"); e.ReadAt.IsZero() {
		t.Error("a rule that marks read leaves no read time")
	}

	// A disabled rule that is switched on acts on the next fetch, and deleting a rule leaves what it did.
	yes := true
	if _, err := s.UpdateRule(ctx, off.ID, RulePatch{Enabled: &yes}); err != nil {
		t.Fatal(err)
	}
	saveItems(t, s, a, FeedEntry{GUID: "never2", Title: "never again"})
	if !entryByGUID(t, s, a.ID, "never2").Starred {
		t.Error("an enabled rule did not run")
	}
	if err := s.DeleteRule(ctx, star.ID); err != nil {
		t.Fatal(err)
	}
	if !entryByGUID(t, s, a.ID, "go").Starred {
		t.Error("deleting a rule undid its work")
	}
}

func TestRuleStarQueuesTheLocalCopy(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{a.ID}, Keywords: "keep", Star: true, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	saveItems(t, s, a, FeedEntry{GUID: "1", Title: "keep this", URL: "https://a.example/1"}, FeedEntry{GUID: "2", Title: "skip"})
	if n := count(t, s, "SELECT count(*) FROM entries WHERE starred = 1"); n != 1 {
		t.Fatalf("%d starred", n)
	}
}

func TestRulesGoWithTheirSource(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{a.ID}, Star: true, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	if rules, err := s.Rules(ctx); err != nil || len(rules) != 0 {
		t.Fatalf("%+v %v", rules, err)
	}
}

func TestRuleValidation(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	missing := int64(9999)
	long := make([]rune, 101)
	for i := range long {
		long[i] = '字'
	}
	var many string
	for i := 0; i < 21; i++ {
		many += "k" + strconv.Itoa(i) + "\n"
	}
	for _, c := range []struct {
		name string
		rule Rule
		want error
	}{
		{"missing source", Rule{SourceIDs: []int64{missing}, Star: true}, ErrRuleSource},
		{"kind", Rule{Kind: "podcast", Keywords: "x", Star: true}, ErrRuleKind},
		{"too many keywords", Rule{Keywords: many, Star: true}, ErrRuleKeywords},
		{"long keyword", Rule{Keywords: string(long), Star: true}, ErrRuleKeywords},
		{"long author", Rule{Author: string(long), Star: true}, ErrRuleAuthor},
		{"no condition", Rule{Keywords: " \n\t\n", Author: "  ", Star: true}, ErrRuleNoCondition},
		{"no action", Rule{SourceIDs: []int64{a.ID}}, ErrRuleNoAction},
	} {
		if _, err := s.CreateRule(ctx, c.rule, time.Now()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}

	// A rule with 20 lines of 100 characters is fine, and blank lines do not count.
	ok, err := s.CreateRule(ctx, Rule{Keywords: many[:len(many)-len("k19\nk20\n")] + "\n\n  spaced  ", Star: true}, time.Now())
	if err != nil || ok.Keywords[len(ok.Keywords)-6:] != "spaced" {
		t.Fatalf("%+v %v", ok, err)
	}

	// The merged result of a patch is checked again.
	none := false
	if _, err := s.UpdateRule(ctx, ok.ID, RulePatch{Star: &none}); !errors.Is(err, ErrRuleNoAction) {
		t.Errorf("patch to no action: %v", err)
	}
	empty := ""
	if _, err := s.UpdateRule(ctx, ok.ID, RulePatch{Keywords: &empty}); !errors.Is(err, ErrRuleNoCondition) {
		t.Errorf("patch to no condition: %v", err)
	}
	// A source can be set and cleared again.
	one := []int64{a.ID}
	set, err := s.UpdateRule(ctx, ok.ID, RulePatch{SourceIDs: &one})
	if err != nil || !slices.Equal(set.SourceIDs, one) {
		t.Fatalf("%+v %v", set, err)
	}
	every := []int64{}
	cleared, err := s.UpdateRule(ctx, ok.ID, RulePatch{SourceIDs: &every})
	if err != nil || len(cleared.SourceIDs) != 0 {
		t.Fatalf("%+v %v", cleared, err)
	}
	if _, err := s.UpdateRule(ctx, 9999, RulePatch{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("patch of a missing rule: %v", err)
	}
	if err := s.DeleteRule(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete of a missing rule: %v", err)
	}
}

func TestRuleOfSeveralSourcesAndExcludedWords(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	b := newLogSource(t, s, "https://b.example/feed")
	c := newLogSource(t, s, "https://c.example/feed")
	rule, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{b.ID, a.ID, a.ID}, Keywords: "weekly", Exclude: " ads \n\nSPAM", Star: true, Enabled: true}, time.Now())
	if err != nil || !slices.Equal(rule.SourceIDs, []int64{a.ID, b.ID}) || rule.Exclude != "ads\nSPAM" {
		t.Fatalf("%+v %v", rule, err)
	}
	// The same rule is read back by the list.
	if rules, err := s.Rules(ctx); err != nil || len(rules) != 1 || !slices.Equal(rules[0].SourceIDs, rule.SourceIDs) {
		t.Fatalf("%+v %v", rules, err)
	}
	items := []FeedEntry{
		{GUID: "plain", Title: "Weekly 1"},
		{GUID: "ads", Title: "Weekly sponsored by ADS"},
		{GUID: "spam", Summary: "weekly spam"},
	}
	saveItems(t, s, a, items...)
	saveItems(t, s, b, items[:1]...)
	saveItems(t, s, c, items[:1]...)
	for _, want := range []struct {
		src     int64
		guid    string
		starred bool
	}{
		{a.ID, "plain", true}, {a.ID, "ads", false}, {a.ID, "spam", false}, {b.ID, "plain", true}, {c.ID, "plain", false},
	} {
		if e := entryByGUID(t, s, want.src, want.guid); e.Starred != want.starred {
			t.Errorf("%d %s: starred %v", want.src, want.guid, e.Starred)
		}
	}

	// Words to leave out are no condition; too many sources are refused.
	if _, err := s.CreateRule(ctx, Rule{Exclude: "ads", Star: true}, time.Now()); !errors.Is(err, ErrRuleNoCondition) {
		t.Errorf("exclude alone: %v", err)
	}
	if _, err := s.CreateRule(ctx, Rule{Exclude: many21(), Keywords: "x", Star: true}, time.Now()); !errors.Is(err, ErrRuleKeywords) {
		t.Errorf("too many excluded words: %v", err)
	}
	var ids []int64
	for i := 0; i <= MaxRuleSources; i++ {
		ids = append(ids, int64(1000+i))
	}
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: ids, Star: true}, time.Now()); !errors.Is(err, ErrRuleSources) {
		t.Errorf("too many sources: %v", err)
	}

	// The count looks at the entries there are now: starred, read and the like do not matter.
	n, err := s.RuleMatchCount(ctx, Rule{SourceIDs: []int64{a.ID, c.ID}, Keywords: "weekly", Exclude: "ads"})
	if err != nil || n != 3 {
		t.Fatalf("count %d %v", n, err)
	}
	if n, err := s.RuleMatchCount(ctx, Rule{Keywords: "weekly"}); err != nil || n != 5 {
		t.Fatalf("every source: %d %v", n, err)
	}
	if n, err := s.RuleMatchCount(ctx, Rule{Exclude: "ads"}); err != nil || n != 0 {
		t.Fatalf("exclude alone: %d %v", n, err)
	}
	if _, err := s.RuleMatchCount(ctx, Rule{Kind: "podcast"}); !errors.Is(err, ErrRuleKind) {
		t.Fatalf("kind: %v", err)
	}
	if err := s.DeleteSource(ctx, c.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RuleMatchCount(ctx, Rule{Keywords: "weekly"}); err != nil || n != 4 {
		t.Fatalf("a deleted source is not counted: %d %v", n, err)
	}
}

func many21() string {
	var many string
	for i := 0; i < 21; i++ {
		many += "k" + strconv.Itoa(i) + "\n"
	}
	return many
}

func TestRuleKeepsItsOtherSourcesWhenOneIsPurged(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	b := newLogSource(t, s, "https://b.example/feed")
	both, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{a.ID, b.ID}, Star: true, Enabled: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{a.ID}, Star: true, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	rules, err := s.Rules(ctx)
	if err != nil || len(rules) != 1 || rules[0].ID != both.ID || !slices.Equal(rules[0].SourceIDs, []int64{b.ID}) {
		t.Fatalf("%+v %v", rules, err)
	}
}

func TestRuleLimit(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	for i := 0; i < MaxRules; i++ {
		if _, err := s.CreateRule(ctx, Rule{Keywords: "k" + strconv.Itoa(i), Star: true, Enabled: true}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateRule(ctx, Rule{Keywords: "one more", Star: true}, time.Now()); !errors.Is(err, ErrTooManyRules) {
		t.Fatalf("the 51st rule: %v", err)
	}
}

func TestGlobalMuteHidesLikeASourceList(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	b := newLogSource(t, s, "https://b.example/feed")
	saveItems(t, s, a,
		FeedEntry{GUID: "1", Title: "Sponsored post"},
		FeedEntry{GUID: "2", Title: "Rust news"},
		FeedEntry{GUID: "3", Title: "Kept", Summary: "nothing"},
		FeedEntry{GUID: "4", Title: "Sponsored but starred"},
	)
	saveItems(t, s, b, FeedEntry{GUID: "5", Title: "Also sponsored"}, FeedEntry{GUID: "6", Title: "Fine"})
	yes := true
	if _, err := s.UpdateEntry(ctx, entryByGUID(t, s, a.ID, "4").ID, EntryPatch{Starred: &yes}, time.Now()); err != nil {
		t.Fatal(err)
	}

	titles := func() map[string]bool {
		list, err := s.Entries(ctx, EntryFilter{}, 50)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, e := range list {
			got[guidOf(t, s, e.ID)] = true
		}
		return got
	}
	if got := titles(); len(got) != 6 {
		t.Fatalf("before: %v", got)
	}

	if n, err := s.MutedCountAll(ctx, "sponsored"); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if err := s.SetSetting(ctx, MuteSetting, "sponsored"); err != nil {
		t.Fatal(err)
	}
	got := titles()
	if got["1"] || got["5"] || !got["4"] || !got["2"] || len(got) != 4 {
		t.Fatalf("global list: %v", got)
	}

	// The source's own list adds to the global one; the count says what the global list hides in total.
	if _, err := s.db.ExecContext(ctx, "UPDATE sources SET mute_keywords = 'rust' WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	got = titles()
	if got["1"] || got["2"] || got["5"] || len(got) != 3 {
		t.Fatalf("both lists: %v", got)
	}
	if n, err := s.MutedCountAll(ctx, "rust\nsponsored"); err != nil || n != 3 {
		t.Fatalf("count with both lists = %d, %v", n, err)
	}

	// The unread count follows the same rule.
	list, err := s.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	unread := map[int64]int{}
	for _, src := range list {
		unread[src.ID] = src.UnreadCount
	}
	if unread[a.ID] != 2 || unread[b.ID] != 1 {
		t.Fatalf("unread counts: %v", unread)
	}

	// Clearing the list shows everything again.
	if err := s.SetSetting(ctx, MuteSetting, ""); err != nil {
		t.Fatal(err)
	}
	if got := titles(); len(got) != 5 {
		t.Fatalf("after clearing: %v", got)
	}
}

func TestRuleOfASourceInItsUndoWindow(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src := newLogSource(t, s, "https://a.example/feed")
	now := time.Now()
	rule, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{src.ID}, Star: true, Enabled: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(ctx, src.ID, now); err != nil {
		t.Fatal(err)
	}
	off := false
	if got, err := s.UpdateRule(ctx, rule.ID, RulePatch{Enabled: &off}); err != nil || got.Enabled {
		t.Fatalf("toggle in the window: %+v, %v", got, err)
	}
	// Undoing the deletion of the rule needs the same.
	if err := s.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRule(ctx, Rule{ID: rule.ID, SourceIDs: []int64{src.ID}, Star: true, Enabled: true}, now); err != nil {
		t.Fatalf("re-create in the window: %v", err)
	}
	// Purged for good, the source is invalid again and its rule is gone.
	if _, _, err := s.PurgeDeletedSources(ctx, now.Add(DeleteUndoWindow+time.Second)); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Rules(ctx); len(list) != 0 {
		t.Fatalf("rules after the purge: %+v", list)
	}
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: []int64{src.ID}, Star: true}, now); !errors.Is(err, ErrRuleSource) {
		t.Fatalf("rule of a purged source: %v", err)
	}
}

func TestRuleLimitHoldsUnderConcurrentCreates(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	var wg sync.WaitGroup
	var mu sync.Mutex
	made, refused := 0, 0
	for i := 0; i < MaxRules+10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateRule(ctx, Rule{Keywords: "k" + strconv.Itoa(i), Star: true, Enabled: true}, time.Now())
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				made++
			case errors.Is(err, ErrTooManyRules):
				refused++
			default:
				t.Errorf("create %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if list, _ := s.Rules(ctx); len(list) != MaxRules || made != MaxRules || refused != 10 {
		t.Fatalf("rules = %d, made %d, refused %d", len(list), made, refused)
	}
}

func TestConcurrentRulePatchesKeepBoth(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	rule, err := s.CreateRule(ctx, Rule{Keywords: "go", Star: true, Enabled: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	read, off, words := true, false, "rust"
	var wg sync.WaitGroup
	for _, p := range []RulePatch{{MarkRead: &read}, {Enabled: &off}, {Keywords: &words}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.UpdateRule(ctx, rule.ID, p); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := s.Rule(ctx, rule.ID)
	if err != nil || !got.MarkRead || got.Enabled || got.Keywords != "rust" || !got.Star {
		t.Fatalf("rule = %+v, %v", got, err)
	}
}

func TestRestoredRuleKeepsItsPlace(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	base := time.Now()
	var made []Rule
	for i, k := range []string{"a", "b", "c"} {
		r, err := s.CreateRule(ctx, Rule{Keywords: k, Star: true, Enabled: true}, base.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, r)
	}
	if err := s.DeleteRule(ctx, made[1].ID); err != nil {
		t.Fatal(err)
	}
	back, err := s.CreateRule(ctx, made[1], made[1].CreatedAt)
	if err != nil || back.ID != made[1].ID {
		t.Fatalf("restored = %+v, %v", back, err)
	}
	list, _ := s.Rules(ctx)
	if len(list) != 3 || list[0].Keywords != "a" || list[1].Keywords != "b" || list[2].Keywords != "c" {
		t.Fatalf("order after the restore: %+v", list)
	}
	// An ID that is taken gets a new one, and the rule still sorts by its old time.
	again, err := s.CreateRule(ctx, Rule{ID: made[0].ID, Keywords: "d", Star: true}, made[0].CreatedAt.Add(time.Millisecond))
	if err != nil || again.ID == made[0].ID {
		t.Fatalf("taken ID: %+v, %v", again, err)
	}
}

func TestMigrationMovesTheSourceOfARuleToTheJoinTable(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s := open(t, file)
	a := newLogSource(t, s, "https://a.example/feed")
	s.Close()

	// Put the database back to version 28: the rules table with its one source_id.
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"DROP TRIGGER rules_lose_last_source", "DROP TABLE rule_sources", "DROP TABLE rules",
		`CREATE TABLE rules (id INTEGER PRIMARY KEY, source_id INTEGER REFERENCES sources (id) ON DELETE CASCADE,
			kind TEXT NOT NULL DEFAULT '', keywords TEXT NOT NULL DEFAULT '', author TEXT NOT NULL DEFAULT '',
			star INTEGER NOT NULL DEFAULT 0, mark_read INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL)`,
		"CREATE INDEX rules_source ON rules (source_id)",
		"INSERT INTO rules (id, source_id, keywords, star, created_at) VALUES (7, " + strconv.FormatInt(a.ID, 10) + ", 'go', 1, 5)",
		"INSERT INTO rules (id, kind, author, mark_read, enabled, created_at) VALUES (8, 'video', 'bob', 1, 0, 6)",
		"ALTER TABLE sources DROP COLUMN open_target",
		"DROP TABLE entry_revisions",
		"ALTER TABLE sources DROP COLUMN hide_from_all",
		"DROP INDEX entries_remind",
		"ALTER TABLE entries DROP COLUMN remind_at",
		"DROP INDEX entries_title_tr",
		"DROP INDEX entries_translated_title",
		"ALTER TABLE entries DROP COLUMN translated_title",
		"ALTER TABLE entries DROP COLUMN title_tr_status",
		"ALTER TABLE entries DROP COLUMN title_tr_attempts",
		"ALTER TABLE entries DROP COLUMN title_tr_at",
		"DROP TRIGGER entries_unfold",
		"DROP INDEX entries_folded",
		"ALTER TABLE entries DROP COLUMN folded",
		"ALTER TABLE sources DROP COLUMN daily_cap",
		"ALTER TABLE sources DROP COLUMN noisy_dismissed",
		"ALTER TABLE sources DROP COLUMN translate_titles",
		"PRAGMA user_version = 28",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rules, err := s.Rules(ctx)
	if err != nil || len(rules) != 2 {
		t.Fatalf("%+v %v", rules, err)
	}
	if r := rules[0]; r.ID != 7 || !slices.Equal(r.SourceIDs, []int64{a.ID}) || r.Keywords != "go" || !r.Star || !r.Enabled || r.CreatedAt.UnixMilli() != 5 {
		t.Errorf("%+v", r)
	}
	if r := rules[1]; r.ID != 8 || len(r.SourceIDs) != 0 || r.Kind != "video" || r.Author != "bob" || !r.MarkRead || r.Enabled {
		t.Errorf("%+v", r)
	}
}
