package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestSourcesUnreadCountsMatchSingleSource checks that the grouped counts of Sources equal the counts of Source for a
// varied set: muted by source and by global keywords, starred, paced, paused, deleted, read and empty sources.
func TestSourcesUnreadCountsMatchSingleSource(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	const sources = 8
	for i := 1; i <= sources; i++ {
		keywords, paused, pace, deleted := "", 0, 0, "NULL"
		switch i {
		case 2, 3:
			keywords = "sponsored"
		case 4:
			paused = 1
		case 5:
			pace = 3600
		case 6:
			deleted = "1"
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO sources (url, title, kind, mute_keywords, paused, pace_sec, deleted_at, next_fetch_at, created_at, updated_at)
			VALUES (?, ?, 'article', ?, ?, ?, %s, 0, 0, 0)`, deleted), fmt.Sprintf("https://example.com/%d.xml", i), fmt.Sprintf("Source %d", i), keywords, paused, pace); err != nil {
			t.Fatal(err)
		}
	}
	// Source 7 gets no entries at all; the others get a mix.
	for i := 1; i <= sources-2; i++ {
		for j := 0; j < 30; j++ {
			title := fmt.Sprintf("Entry %d", j)
			if j%4 == 0 {
				title += " Sponsored"
			}
			if j%5 == 0 {
				title += " global"
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO entries (source_id, guid, title, rss_summary, is_read, starred, created_at, updated_at)
				VALUES (?, ?, ?, 'summary', ?, ?, ?, ?)`, i, fmt.Sprint(j), title, j%3 == 0, j%7 == 0, int64(j), int64(j)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, global := range []string{"global", ""} {
		if err := s.SetSetting(ctx, MuteSetting, global); err != nil {
			t.Fatal(err)
		}
		list, err := s.Sources(ctx)
		if err != nil || len(list) != sources-1 {
			t.Fatalf("global %q: Sources = %d sources, %v, want %d", global, len(list), err, sources-1)
		}
		total := 0
		for _, src := range list {
			one, err := s.Source(ctx, src.ID)
			if err != nil {
				t.Fatal(err)
			}
			if one.UnreadCount != src.UnreadCount {
				t.Errorf("global %q, source %d: list count %d, single count %d", global, src.ID, src.UnreadCount, one.UnreadCount)
			}
			total += src.UnreadCount
		}
		if list[0].UnreadCount == 0 || total == 0 {
			t.Errorf("counts are all zero: %+v", list)
		}
		for _, src := range list {
			if src.ID == 7 && src.UnreadCount != 0 {
				t.Errorf("source without entries counts %d, want 0", src.UnreadCount)
			}
		}
	}
}

// TestSourcesUnreadCountsPlan checks that the grouped count reads the partial index of unread entries.
func TestSourcesUnreadCountsPlan(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT s.id, coalesce(u.n, 0) FROM sources s LEFT JOIN "+unreadByID+" u ON u.source_id = s.id WHERE s.deleted_at IS NULL")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "entries_source_unread") || strings.Contains(text, "TEMP B-TREE FOR GROUP BY") {
		t.Errorf("want entries_source_unread and no sort for the grouping, got\n%s", text)
	}
}
