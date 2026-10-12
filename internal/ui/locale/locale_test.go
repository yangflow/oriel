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
