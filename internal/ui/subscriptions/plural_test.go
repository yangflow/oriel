package subscriptions

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/ui/locale"
)

// TestSingularCounts checks that the English texts with a count read right for none, one and many.
func TestSingularCounts(t *testing.T) {
	s, _, _ := setup(t, "en")
	for _, tc := range []struct {
		name, text string
		want       [3]string
	}{
		{"FoundMany", s.text.FoundMany, [3]string{"Found 0 feeds. Choose one.", "Found 1 feed. Choose one.", "Found 2 feeds. Choose one."}},
		{"FoundRoutes", s.text.FoundRoutes, [3]string{"Found 0 routes. Choose one.", "Found 1 route. Choose one.", "Found 2 routes. Choose one."}},
		{"FailedTimes", s.text.FailedTimes, [3]string{"Failed 0 times in a row: {error}", "Failed 1 time in a row: {error}", "Failed 2 times in a row: {error}"}},
		{"HealthSilent", s.text.HealthSilent, [3]string{"No new entries for 0 days", "No new entries for 1 day", "No new entries for 2 days"}},
		{"HealthDead", s.text.HealthDead, [3]string{"Failing for 0 days", "Failing for 1 day", "Failing for 2 days"}},
		{"HealthFailing", s.text.HealthFailing, [3]string{"Failed 0 times in a row", "Failed 1 time in a row", "Failed 2 times in a row"}},
		{"HealthRare", s.text.HealthRare, [3]string{"Read {read} of 0 new entries in the last {days} {days:day|days}", "Read {read} of 1 new entry in the last {days} {days:day|days}", "Read {read} of 2 new entries in the last {days} {days:day|days}"}},
	} {
		for n, want := range tc.want {
			if got := s.count(tc.text, n); got != want {
				t.Errorf("%s with %d: %q, want %q", tc.name, n, got, want)
			}
		}
	}
	for n, want := range [3]string{"Last 0 days: {recent} new, {read} read, {starred} starred", "Last 1 day: {recent} new, {read} read, {starred} starred", "Last 2 days: {recent} new, {read} read, {starred} starred"} {
		if got := locale.Count(s.text.HealthNumbers, "days", n); got != want {
			t.Errorf("HealthNumbers with %d: %q, want %q", n, got, want)
		}
	}
	if got := locale.Count(s.count(s.text.HealthRare, 1), "days", 1); got != "Read {read} of 1 new entry in the last 1 day" {
		t.Errorf("HealthRare: %q", got)
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
