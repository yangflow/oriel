package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkSources measures the sources list with 500 subscriptions and 100000 entries, a fifth of them unread,
// 50 subscriptions with mute keywords and a few starred entries. Run it with
// go test -run '^$' -bench BenchmarkSources ./internal/store/
func BenchmarkSources(b *testing.B) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(b.TempDir(), "oriel.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })

	const sources, perSource = 500, 200
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 1; i <= sources; i++ {
		keywords := ""
		if i%10 == 0 {
			keywords = "sponsored\nadvert\nCoupon"
		}
		if _, err := tx.Exec(`INSERT INTO sources (url, title, kind, mute_keywords, next_fetch_at, created_at, updated_at)
			VALUES (?, ?, 'article', ?, 0, 0, 0)`, fmt.Sprintf("https://example.com/%d.xml", i), fmt.Sprintf("Source %d", i), keywords); err != nil {
			b.Fatal(err)
		}
	}
	for i := 1; i <= sources; i++ {
		for j := 0; j < perSource; j++ {
			title := fmt.Sprintf("Entry %d of %d", j, i)
			if j%25 == 0 {
				title += " sponsored"
			}
			if _, err := tx.Exec(`INSERT INTO entries (source_id, guid, title, rss_summary, is_read, starred, created_at, updated_at)
				VALUES (?, ?, ?, 'a short summary', ?, ?, ?, ?)`,
				i, fmt.Sprint(j), title, j%5 != 0, j%97 == 0, int64(j), int64(j)); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		list, err := s.Sources(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(list) != sources {
			b.Fatal(len(list))
		}
	}
}
