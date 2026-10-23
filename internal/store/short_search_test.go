package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestShortSearchWalksTimelineIndex checks that a short search reads the newest entries by the timeline index and
// stops at the limit, instead of sorting every entry that matches or going through the full-text index.
func TestShortSearchWalksTimelineIndex(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	for _, oldest := range []bool{false, true} {
		query, args := EntryFilter{Query: "苹果", Oldest: oldest}.timelineQuery()
		plan := queryPlan(t, s, query, append(args, 200)...)
		if !strings.Contains(plan, "SCAN e USING INDEX entries_timeline") || strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "entries_fts") {
			t.Errorf("oldest %v: want a walk of entries_timeline with no sort, got\n%s", oldest, plan)
		}
	}
}
