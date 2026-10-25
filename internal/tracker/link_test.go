package tracker

import "testing"

func TestStripLink(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"prefix", "https://a.example/p?utm_source=x&utm_medium=y&id=3", "https://a.example/p?id=3"},
		{"prefix without case", "https://a.example/p?UTM_Campaign=x&id=3", "https://a.example/p?id=3"},
		{"whole names", "https://a.example/p?fbclid=1&gclid=2&dclid=3&msclkid=4&mc_cid=5&mc_eid=6&_hsenc=7&_hsmi=8&mkt_tok=9&igshid=10&yclid=11&id=3",
			"https://a.example/p?id=3"},
		{"name is not a prefix match", "https://a.example/p?fbclid2=1&xutm_a=2", "https://a.example/p?fbclid2=1&xutm_a=2"},
		{"ref feed reader", "https://a.example/p?ref=rss&id=3", "https://a.example/p?id=3"},
		{"ref feedly", "https://a.example/p?id=3&ref=Feedly", "https://a.example/p?id=3"},
		{"ref feed", "https://a.example/p?ref=feed", "https://a.example/p"},
		{"ref other value stays", "https://a.example/p?ref=main&id=3", "https://a.example/p?ref=main&id=3"},
		{"ref without value stays", "https://a.example/p?ref", "https://a.example/p?ref"},
		{"only tracking drops the question mark", "https://a.example/p?utm_source=x&fbclid=1", "https://a.example/p"},
		{"only tracking with fragment", "https://a.example/p?utm_source=x#top", "https://a.example/p#top"},
		{"fragment stays", "https://a.example/p?utm_source=x&id=3#sec", "https://a.example/p?id=3#sec"},
		{"order and spelling stay", "http://a.example/p?z=%E4%B8%AD&utm_x=1&a=b+c&k=%2F", "http://a.example/p?z=%E4%B8%AD&a=b+c&k=%2F"},
		{"encoded name", "https://a.example/p?%75tm_source=x&id=3", "https://a.example/p?id=3"},
		{"no query", "https://a.example/p#utm_source=x", "https://a.example/p#utm_source=x"},
		{"question mark in fragment", "https://a.example/p#a?utm_source=x", "https://a.example/p#a?utm_source=x"},
		{"empty query", "https://a.example/p?", "https://a.example/p?"},
		{"untouched link is returned as is", "https://A.example/p/?a=1&&b", "https://A.example/p/?a=1&&b"},
		{"not http", "mailto:a@b.example?utm_source=x", "mailto:a@b.example?utm_source=x"},
		{"other scheme", "ftp://a.example/p?utm_source=x", "ftp://a.example/p?utm_source=x"},
		{"no host", "/p?utm_source=x", "/p?utm_source=x"},
		{"does not parse", "http://a b/p?utm_source=x", "http://a b/p?utm_source=x"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripLink(tc.in); got != tc.want {
				t.Errorf("StripLink(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParam(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"utm_source", "", true}, {"UTM_Medium", "x", true}, {"fbclid", "", true}, {"mkt_tok", "1", true},
		{"ref", "rss", true}, {"ref", "main", false}, {"ref", "", false}, {"id", "rss", false}, {"utm", "", false},
	} {
		if got := Param(tc.name, tc.value); got != tc.want {
			t.Errorf("Param(%q, %q) = %v, want %v", tc.name, tc.value, got, tc.want)
		}
	}
}
