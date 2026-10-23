package locale

import (
	"strings"
	"testing"
)

func TestFetchError(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want func(words) string
	}{
		{"status 403", func(w words) string { return w.ErrForbidden }},
		{"status 401", func(w words) string { return w.ErrForbidden }},
		{"status 404", func(w words) string { return w.ErrNotFound }},
		{"status 410: the feed was removed (Gone)", func(w words) string { return w.ErrNotFound }},
		{"status 429", func(w words) string { return w.ErrRateLimited }},
		{"status 503", func(w words) string { return w.ErrServer }},
		{"rsshub.example: status 500; backup.example: status 502", func(w words) string { return w.ErrServer }},
		{"Get \"https://x/feed\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)",
			func(w words) string { return w.ErrTimeout }},
		{"dial tcp 1.2.3.4:443: i/o timeout", func(w words) string { return w.ErrTimeout }},
		{"dial tcp: lookup x.example: no such host", func(w words) string { return w.ErrDNS }},
		{"tls: failed to verify certificate: x509: certificate has expired", func(w words) string { return w.ErrTLS }},
		{"dial tcp 1.2.3.4:443: connect: connection refused", func(w words) string { return w.ErrNetwork }},
		{"read tcp: connection reset by peer", func(w words) string { return w.ErrNetwork }},
		{"EOF", func(w words) string { return w.ErrNetwork }},
		{"cannot reach the proxy: connection refused", func(w words) string { return w.ErrNetwork }},
		{"not a feed: XML syntax error on line 1: unexpected EOF", func(w words) string { return w.ErrNotFeed }},
	} {
		for lang, w := range texts {
			got, ok := FetchError(lang, tc.raw)
			if want := tc.want(w); !ok || got != want || want == "" {
				t.Errorf("%s %q: %q, %v; want %q", lang, tc.raw, got, ok, want)
			}
		}
	}
}

func TestFetchErrorUnknown(t *testing.T) {
	for _, raw := range []string{
		"", "disk on fire", "status 418", "status 301", "refusing to connect to a private or reserved address: 10.0.0.1",
		"a: status 500; b: status 429", "a: status 500; b: disk on fire", "internal error while fetching",
	} {
		for lang := range texts {
			if got, ok := FetchError(lang, raw); ok || got != raw {
				t.Errorf("%s %q: %q, %v; want the text itself", lang, raw, got, ok)
			}
		}
	}
}

func TestFetchErrorPhrasesInEveryLanguage(t *testing.T) {
	for lang, w := range texts {
		for name, phrase := range map[string]string{"ErrTimeout": w.ErrTimeout, "ErrNetwork": w.ErrNetwork, "ErrDNS": w.ErrDNS,
			"ErrTLS": w.ErrTLS, "ErrForbidden": w.ErrForbidden, "ErrRateLimited": w.ErrRateLimited, "ErrNotFound": w.ErrNotFound,
			"ErrServer": w.ErrServer, "ErrNotFeed": w.ErrNotFeed} {
			if phrase == "" {
				t.Errorf("%s has no %s", lang, name)
			}
		}
		if !strings.Contains(w.ErrOriginal, "{raw}") {
			t.Errorf("%s: ErrOriginal %q has no {raw}", lang, w.ErrOriginal)
		}
	}
}

func TestWithOriginal(t *testing.T) {
	for _, tc := range []struct{ lang, text, raw, want string }{
		{"en", "The site refused access.", "status 403", "The site refused access.\nOriginal: status 403"},
		{"zh-Hans", "网站拒绝了访问。", "status 403", "网站拒绝了访问。\n原文：status 403"},
		{"en", "Last fetch failed: disk on fire", "disk on fire", "Last fetch failed: disk on fire"},
		{"en", "Fine.", "", "Fine."},
	} {
		if got := WithOriginal(tc.lang, tc.text, tc.raw); got != tc.want {
			t.Errorf("WithOriginal(%q, %q, %q) = %q, want %q", tc.lang, tc.text, tc.raw, got, tc.want)
		}
	}
}
