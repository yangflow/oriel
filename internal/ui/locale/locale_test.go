package locale

import "testing"

func TestLanguage(t *testing.T) {
	for locale, want := range map[string]string{
		"en-US": "en", "zh-CN": "zh-Hans", "zh_TW": "zh-Hans", "zh-Hant-HK": "zh-Hans", "ZH": "zh-Hans",
		"fr-CA": "en", "ja-JP": "en", "xx": "en", "": "en",
	} {
		if got := Language(locale); got != want {
			t.Errorf("%q: %s, want %s", locale, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct{ chosen, system, want string }{
		{"", "en-US", "en"}, {"", "zh_TW", "zh-Hans"}, {"en", "zh-CN", "en"}, {"zh-Hans", "en-US", "zh-Hans"},
		{"fr", "zh-CN", "zh-Hans"}, {"fr", "de", "en"},
	} {
		if got := Resolve(tc.chosen, tc.system); got != tc.want {
			t.Errorf("%q on %q: %s, want %s", tc.chosen, tc.system, got, tc.want)
		}
	}
}

func TestCount(t *testing.T) {
	for _, tc := range []struct {
		text, name string
		n          int
		want       string
	}{
		{"{n} {n:entry|entries}", "n", 0, "0 entries"},
		{"{n} {n:entry|entries}", "n", 1, "1 entry"},
		{"{n} {n:entry|entries}", "n", 2, "2 entries"},
		{"{n:entry is|entries are} here", "n", 1, "entry is here"},
		{"{n} {n:day|days}", "n", -1, "-1 days"},
		{"Restored {s} {s:subscription|subscriptions}, {e} {e:entry|entries}", "s", 1, "Restored 1 subscription, {e} {e:entry|entries}"},
		{"{n:one|other", "n", 1, "{n:one|other"},
		{"{n:one", "n", 1, "{n:one"},
		{"{n:only}", "n", 1, "only"},
		{"{n:only}", "n", 2, ""},
		{"{n} 条条目", "n", 1, "1 条条目"},
		{"no count", "n", 1, "no count"},
	} {
		if got := Count(tc.text, tc.name, tc.n); got != tc.want {
			t.Errorf("Count(%q, %q, %d) = %q, want %q", tc.text, tc.name, tc.n, got, tc.want)
		}
	}
}
