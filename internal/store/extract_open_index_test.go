package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// queryPlan returns the EXPLAIN QUERY PLAN lines of a production query.
func queryPlan(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
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
	return strings.Join(plan, "\n")
}

func TestPendingExtractionsUsesOpenIndex(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	plan := queryPlan(t, s, pendingExtractionsQuery, 1000, 10)
	if !strings.Contains(plan, "SEARCH e USING INDEX entries_extract_open") || strings.Contains(plan, "SCAN") {
		t.Errorf("want a search of entries_extract_open and no scan of entries, got\n%s", plan)
	}
}
