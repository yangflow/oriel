package reader

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/locale"
)

// TestSingularCounts checks that the English texts with a count read right for none, one and many.
func TestSingularCounts(t *testing.T) {
	s, _, _ := setup(t, "en", client.Article{})
	for n, want := range []string{"0 unchanged paragraphs", "1 unchanged paragraph", "2 unchanged paragraphs"} {
		if got := locale.Count(s.text.Unchanged, "n", n); got != want {
			t.Errorf("Unchanged with %d: %q, want %q", n, got, want)
		}
	}
	for _, tc := range []struct {
		cols, rows int
		want       string
	}{
		{1, 1, "Table, 1 column, 1 row"},
		{1, 3, "Table, 1 column, 3 rows"},
		{5, 2, "Table, 5 columns, 2 rows"},
	} {
		if got := tableName(s.text.TableName, tc.cols, tc.rows); got != tc.want {
			t.Errorf("TableName with %d and %d: %q, want %q", tc.cols, tc.rows, got, tc.want)
		}
	}
}
