package locale

import (
	"encoding/json"
	"testing"
	"time"
)

var noon = time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC)

func TestAgo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		at     time.Time
		en, zh string
	}{
		{"ahead", noon.Add(time.Hour), "just now", "刚刚"},
		{"now", noon, "just now", "刚刚"},
		{"59 seconds", noon.Add(-59 * time.Second), "just now", "刚刚"},
		{"one minute", noon.Add(-time.Minute), "1 min ago", "1 分钟前"},
		{"59 minutes", noon.Add(-59 * time.Minute), "59 min ago", "59 分钟前"},
		{"one hour", noon.Add(-time.Hour), "1 h ago", "1 小时前"},
		{"midnight", noon.Add(-11*time.Hour - 59*time.Minute), "11 h ago", "11 小时前"},
		{"yesterday", noon.Add(-24 * time.Hour), "yesterday", "昨天"},
		{"last evening", time.Date(2025, 10, 21, 23, 59, 0, 0, time.UTC), "yesterday", "昨天"},
		{"two days", noon.Add(-48 * time.Hour), "2 days ago", "2 天前"},
		{"six days", noon.Add(-6 * 24 * time.Hour), "6 days ago", "6 天前"},
		{"a week", noon.Add(-7 * 24 * time.Hour), "Oct 15", "10月15日"},
		{"this year", time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC), "Jan 2", "1月2日"},
		{"another year", time.Date(2024, 12, 31, 9, 0, 0, 0, time.UTC), "Dec 31, 2024", "2024年12月31日"},
	} {
		if got := Ago("en", noon, tc.at); got != tc.en {
			t.Errorf("%s: en %q, want %q", tc.name, got, tc.en)
		}
		if got := Ago("zh-Hans", noon, tc.at); got != tc.zh {
			t.Errorf("%s: zh-Hans %q, want %q", tc.name, got, tc.zh)
		}
	}
}

// One day is "yesterday" and two are plural, so "1 day ago" never shows; the plural form still has to be right.
func TestAgoSingular(t *testing.T) {
	if got := Count(wordsOf("en").Days, "n", 1); got != "1 day ago" {
		t.Errorf("one day: %q", got)
	}
}

func TestAgoInTheZoneOfNow(t *testing.T) {
	zone := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2025, 10, 22, 1, 0, 0, 0, zone)
	at := time.Date(2025, 10, 21, 16, 30, 0, 0, time.UTC) // 00:30 on the 22nd there
	if got := Ago("en", now, at); got != "30 min ago" {
		t.Errorf("%q", got)
	}
	at = time.Date(2025, 10, 21, 10, 0, 0, 0, time.UTC) // 18:00 on the 21st there
	if got := Ago("en", now, at); got != "yesterday" {
		t.Errorf("%q", got)
	}
}

func TestClock(t *testing.T) {
	for _, tc := range []struct {
		lang   string
		cycle  int
		hour   int
		minute int
		want   string
	}{
		{"en", 12, 15, 4, "3:04 PM"}, {"en", 12, 0, 5, "12:05 AM"}, {"en", 12, 12, 0, "12:00 PM"}, {"en", 12, 9, 30, "9:30 AM"},
		{"en", 24, 15, 4, "15:04"}, {"en", 24, 9, 30, "09:30"},
		{"en", 0, 15, 4, "3:04 PM"}, {"zh-Hans", 0, 15, 4, "15:04"},
		{"zh-Hans", 12, 15, 4, "下午3:04"}, {"zh-Hans", 12, 9, 30, "上午9:30"}, {"zh-Hans", 24, 9, 30, "09:30"},
		{"en", 7, 15, 4, "3:04 PM"},
	} {
		at := time.Date(2025, 3, 4, tc.hour, tc.minute, 0, 0, time.UTC)
		if got := Clock(tc.lang, tc.cycle, at); got != tc.want {
			t.Errorf("%s/%d %02d:%02d: %q, want %q", tc.lang, tc.cycle, tc.hour, tc.minute, got, tc.want)
		}
	}
}

func TestDateTime(t *testing.T) {
	now := time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC)
	this := time.Date(2025, 10, 18, 11, 55, 0, 0, time.UTC)
	other := time.Date(2024, 3, 4, 9, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		lang  string
		cycle int
		at    time.Time
		want  string
	}{
		{"en", 12, this, "Oct 18, 11:55 AM"}, {"en", 24, this, "Oct 18, 11:55"}, {"en", 12, other, "Mar 4, 2024, 9:30 AM"},
		{"zh-Hans", 24, this, "10月18日 11:55"}, {"zh-Hans", 24, other, "2024年3月4日 09:30"}, {"zh-Hans", 12, this, "10月18日 上午11:55"},
	} {
		if got := DateTime(tc.lang, tc.cycle, now, tc.at); got != tc.want {
			t.Errorf("%s/%d: %q, want %q", tc.lang, tc.cycle, got, tc.want)
		}
	}
}

func TestCapital(t *testing.T) {
	for in, want := range map[string]string{"yesterday": "Yesterday", "5 min ago": "5 min ago", "昨天": "昨天", "": "", "éa": "Éa"} {
		if got := Capital(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// TestCatalog checks that both languages carry every key.
func TestCatalog(t *testing.T) {
	var raw map[string]map[string]string
	if err := json.Unmarshal(catalog, &raw); err != nil {
		t.Fatal(err)
	}
	for _, lang := range Languages {
		if len(raw[lang]) != len(raw["en"]) {
			t.Errorf("%s has %d keys, en %d", lang, len(raw[lang]), len(raw["en"]))
		}
		for k, v := range raw[lang] {
			if v == "" {
				t.Errorf("%s.%s is empty", lang, k)
			}
		}
	}
}
