package search

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) *Day { return &Day{y, m, d} }

func TestParse(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Query
	}{
		{"", Query{}},
		{"   \t ", Query{}},
		{"sqlite", Query{Include: []string{"sqlite"}}},
		{"SQLite  trigram", Query{Include: []string{"SQLite", "trigram"}}},
		{`"exact phrase" more`, Query{Include: []string{"exact phrase", "more"}}},
		{`-ads -"big sale"`, Query{Exclude: []string{"ads", "big sale"}}},
		{"state-of-the-art - x", Query{Include: []string{"state-of-the-art", "-", "x"}}},
		{"--x", Query{Exclude: []string{"-x"}}},
		{`\-x`, Query{Include: []string{"-x"}}},
		{`a\"b \\ c\d`, Query{Include: []string{`a"b`, `\`, `c\d`}}},
		{`"say \"hi\" \\ \n"`, Query{Include: []string{`say "hi" \ \n`}}},
		{`ab"cd`, Query{Include: []string{`ab"cd`}}},
		{`"open ended`, Query{Include: []string{"open ended"}}},
		{`"" "  " x`, Query{Include: []string{"x"}}},
		{`"from:go"`, Query{Include: []string{"from:go"}}},
		{"from:go", Query{From: []string{"go"}}},
		{`FROM:"The Go Blog" From:rust`, Query{From: []string{"The Go Blog", "rust"}}},
		{`from:"" x`, Query{Include: []string{"from:", "x"}}},
		{"from:", Query{Include: []string{"from:"}}},
		{"-from:go", Query{Exclude: []string{"from:go"}}},
		{"kind:video Kind:IMAGE", Query{Kinds: []string{"video", "image"}}},
		{"kind:podcast kind:", Query{Include: []string{"kind:podcast", "kind:"}}},
		{"after:2026-01-01 before:2026-02-01", Query{After: day(2026, 1, 1), Before: day(2026, 2, 1)}},
		{"after:2026-01-01 after:2026-03-05 after:2025-12-31", Query{After: day(2026, 3, 5)}},
		{"before:2026-05-01 before:2026-02-01", Query{Before: day(2026, 2, 1)}},
		{"after:2026-13-01 before:yesterday after:2026-02-30 after:2026-1-1", Query{Include: []string{"after:2026-13-01", "before:yesterday", "after:2026-02-30", "after:2026-1-1"}}},
		{"foo:bar https://example.com/a C++ 12:30", Query{Include: []string{"foo:bar", "https://example.com/a", "C++", "12:30"}}},
		{"from:go kind:article sqlite -rust", Query{From: []string{"go"}, Kinds: []string{"article"}, Include: []string{"sqlite"}, Exclude: []string{"rust"}}},
		{"深度学习 模型 -广告", Query{Include: []string{"深度学习", "模型"}, Exclude: []string{"广告"}}},
		{"from:订阅 检索", Query{From: []string{"订阅"}, Include: []string{"检索"}}},
	} {
		got := Parse(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseTokenCap(t *testing.T) {
	words := make([]string, 20)
	for i := range words {
		words[i] = "w" + string(rune('a'+i))
	}
	got := Parse(strings.Join(words, " "))
	if !reflect.DeepEqual(got.Include, words[:MaxTokens]) {
		t.Errorf("Include = %v, want the first %d words", got.Include, MaxTokens)
	}
	// Empty quotes are not tokens, so they do not use the cap up.
	got = Parse(strings.Repeat(`"" `, 30) + "x")
	if !reflect.DeepEqual(got.Include, []string{"x"}) {
		t.Errorf("Include = %v", got.Include)
	}
}

func TestNoneAndShort(t *testing.T) {
	for _, c := range []struct {
		in          string
		none, short bool
	}{
		{"", true, false},
		{`""`, true, false},
		{"abc", false, false},
		{"go", false, true},
		{"检索", false, true},
		{"深度学", false, false},
		{"-ab", false, true},
		{`"a b"`, false, false},
		{`"ab"`, false, true},
		{"abc xy", false, true},
		{"from:go kind:video after:2026-01-01", false, false},
		{"-广告", false, true},
	} {
		q := Parse(c.in)
		if q.None() != c.none || q.Short() != c.short {
			t.Errorf("%q: None %v Short %v, want %v %v", c.in, q.None(), q.Short(), c.none, c.short)
		}
	}
}

func TestDayStart(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("no time zone database")
	}
	d := Day{2026, 1, 1}
	if got := d.Start(shanghai).UTC(); !got.Equal(time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC)) {
		t.Errorf("Start = %v", got)
	}
	if got := d.Start(time.UTC); !got.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Start = %v", got)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{`from:"a b" -"c`, `\`, `"`, `-`, `from:"`, "after:2026-01-01"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		q := Parse(s)
		if len(q.Include)+len(q.Exclude)+len(q.From)+len(q.Kinds) > MaxTokens*2 {
			t.Errorf("too many terms for %q", s)
		}
	})
}
