package settings

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/locale"
)

// TestSingularCounts checks that the English texts with a count read right for none, one and many.
func TestSingularCounts(t *testing.T) {
	s, _, _ := setup(t, "en", true)
	for _, tc := range []struct {
		name, text string
		want       [3]string
	}{
		{"Days", s.text.Days, [3]string{"0 days", "1 day", "2 days"}},
		{"Exported", s.text.Exported, [3]string{"Exported 0 subscriptions to {file}.", "Exported 1 subscription to {file}.", "Exported 2 subscriptions to {file}."}},
		{"Minutes", s.text.Minutes, [3]string{"0 minutes ago", "1 minute ago", "2 minutes ago"}},
		{"Hours", s.text.Hours, [3]string{"0 hours ago", "1 hour ago", "2 hours ago"}},
		{"DaysAgo", s.text.DaysAgo, [3]string{"0 days ago", "1 day ago", "2 days ago"}},
		{"ImagesValue", s.text.ImagesValue, [3]string{"0 images, {size}", "1 image, {size}", "2 images, {size}"}},
		{"CopiesValue", s.text.CopiesValue, [3]string{"0 images, {size}", "1 image, {size}", "2 images, {size}"}},
		{"RecentKeep", s.text.RecentKeep, [3]string{"Each subscription keeps its last 0 attempts.", "Each subscription keeps its last 1 attempt.", "Each subscription keeps its last 2 attempts."}},
		{"ReaderResultRead", s.text.ReaderResultRead, [3]string{"0 entries marked as read", "1 entry marked as read", "2 entries marked as read"}},
		{"ReaderResultUnmatched", s.text.ReaderResultUnmatched, [3]string{"0 read entries are not in Oriel and were left out", "1 read entry is not in Oriel and was left out", "2 read entries are not in Oriel and were left out"}},
		{"ReaderResultFolders", s.text.ReaderResultFolders, [3]string{"0 folders and tags were not imported", "1 folder or tag was not imported", "2 folders and tags were not imported"}},
		{"MutedMany", s.text.MutedMany, [3]string{"Hides 0 entries right now", "Hides 1 entry right now", "Hides 2 entries right now"}},
	} {
		for n, want := range tc.want {
			if got := locale.Count(tc.text, "n", n); got != want {
				t.Errorf("%s with %d: %q, want %q", tc.name, n, got, want)
			}
		}
	}
	for n, want := range [3]string{"0 subscriptions added, 5 already subscribed", "1 subscription added, 5 already subscribed", "2 subscriptions added, 5 already subscribed"} {
		if got := s.sourcesResult(&client.ImportResult{AddedSources: n, ExistingSources: 5}); got != want {
			t.Errorf("ReaderResultSources with %d: %q, want %q", n, got, want)
		}
	}
	for n, want := range [3]string{"0 starred entries added, 1 already here", "1 starred entry added, 1 already here", "2 starred entries added, 1 already here"} {
		if got := locale.Count(locale.Count(s.text.ReaderResultStarred, "added", n), "existing", 1); got != want {
			t.Errorf("ReaderResultStarred with %d: %q, want %q", n, got, want)
		}
	}
	for _, tc := range []struct {
		sources, entries int
		want             string
	}{
		{0, 0, "Restored 0 subscriptions and 0 entries."},
		{1, 1, "Restored 1 subscription and 1 entry."},
		{2, 1, "Restored 2 subscriptions and 1 entry."},
		{1, 2, "Restored 1 subscription and 2 entries."},
	} {
		if got := locale.Count(locale.Count(s.text.RestoreDone, "sources", tc.sources), "entries", tc.entries); got != tc.want {
			t.Errorf("RestoreDone with %d and %d: %q, want %q", tc.sources, tc.entries, got, tc.want)
		}
	}
}

// TestCountFormsOnlyInEnglish checks that a plural form in a text belongs to a count the text also carries, and that
// Chinese, which has no plural, has none.
func TestCountFormsOnlyInEnglish(t *testing.T) {
	var raw map[string]map[string]string
	if err := json.Unmarshal(catalog, &raw); err != nil {
		t.Fatal(err)
	}
	form := regexp.MustCompile(`\{(\w+):([^|}]*)\|([^}]*)\}`)
	for language, texts := range raw {
		for key, text := range texts {
			forms := form.FindAllStringSubmatch(text, -1)
			if language != "en" && len(forms) > 0 {
				t.Errorf("%s.%s has a plural form", language, key)
			}
			for _, f := range forms {
				if !strings.Contains(text, "{"+f[1]+"}") {
					t.Errorf("%s.%s has a plural form of {%s} without the count", language, key, f[1])
				}
			}
			if language == "en" && strings.Count(text, "{")-strings.Count(text, "}") != 0 {
				t.Errorf("%s.%s has unbalanced braces", language, key)
			}
		}
	}
}
