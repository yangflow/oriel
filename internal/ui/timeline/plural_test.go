package timeline

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// TestSingularCounts checks that the English texts with a count read right for none, one and many.
func TestSingularCounts(t *testing.T) {
	s, _, _ := setup(t, "en")
	for _, tc := range []struct {
		name, text string
		want       [3]string
	}{
		{"AlsoChip", s.text.AlsoChip, [3]string{"0 more sources", "1 more source", "2 more sources"}},
		{"AlsoName", s.text.AlsoName, [3]string{"Also in 0 other sources, show them", "Also in 1 other source, show them", "Also in 2 other sources, show them"}},
		{"MarkedAbove", s.text.MarkedAbove, [3]string{"Marked 0 entries above as read", "Marked 1 entry above as read", "Marked 2 entries above as read"}},
		{"MarkedVisible", s.text.MarkedVisible, [3]string{"Marked 0 visible entries as read", "Marked 1 visible entry as read", "Marked 2 visible entries as read"}},
		{"Marked", s.text.Marked, [3]string{"Marked 0 entries in “{scope}” as read", "Marked 1 entry in “{scope}” as read", "Marked 2 entries in “{scope}” as read"}},
		{"MarkedOlder", s.text.MarkedOlder, [3]string{"Marked 0 entries in “{scope}” older than {age} as read", "Marked 1 entry in “{scope}” older than {age} as read", "Marked 2 entries in “{scope}” older than {age} as read"}},
		{"Pictures", s.text.Pictures, [3]string{"0 pictures", "1 picture", "2 pictures"}},
		{"MorePictures", s.text.MorePictures, [3]string{"0 more pictures", "1 more picture", "2 more pictures"}},
		{"RefreshingAnnounce", s.text.RefreshingAnnounce, [3]string{"Refreshing 0 subscriptions", "Refreshing 1 subscription", "Refreshing 2 subscriptions"}},
		{"Refreshed", s.text.Refreshed, [3]string{"Checked 0 subscriptions", "Checked 1 subscription", "Checked 2 subscriptions"}},
		{"RefreshedFailed", s.text.RefreshedFailed, [3]string{"Checked 0 subscriptions, {failed} failed", "Checked 1 subscription, {failed} failed", "Checked 2 subscriptions, {failed} failed"}},
		{"RefreshedPart", s.text.RefreshedPart, [3]string{"Checked {done} of 0 subscriptions, the rest will follow", "Checked {done} of 1 subscription, the rest will follow", "Checked {done} of 2 subscriptions, the rest will follow"}},
		{"FailedTimes", s.text.FailedTimes, [3]string{"Failed 0 times in a row: {error}", "Failed 1 time in a row: {error}", "Failed 2 times in a row: {error}"}},
		{"FailingShow", s.text.FailingShow, [3]string{"Show only the 0 failing subscriptions", "Show only the 1 failing subscription", "Show only the 2 failing subscriptions"}},
		{"Days", s.text.Days, [3]string{"0 days", "1 day", "2 days"}},
		{"TimeUpTo", s.text.TimeUpTo, [3]string{"Up to 0 minutes", "Up to 1 minute", "Up to 2 minutes"}},
		{"TimeNameUpTo", s.text.TimeNameUpTo, [3]string{"Reading time: up to 0 minutes", "Reading time: up to 1 minute", "Reading time: up to 2 minutes"}},
		{"NothingFits", s.text.NothingFits, [3]string{"Nothing fits in 0 minutes", "Nothing fits in 1 minute", "Nothing fits in 2 minutes"}},
	} {
		for n, want := range tc.want {
			if got := s.format(tc.text, n); got != want {
				t.Errorf("%s with %d: %q, want %q", tc.name, n, got, want)
			}
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
