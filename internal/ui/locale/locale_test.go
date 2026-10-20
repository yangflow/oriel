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
