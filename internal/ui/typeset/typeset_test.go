package typeset

import (
	"strings"
	"testing"
)

// spaced renders pieces for comparison: "|" after a piece with a gap and "~" after one that tightens.
func spaced(runs []Run) string {
	out := Layout(runs)
	var b strings.Builder
	for k, run := range runs {
		if out == nil || out[k] == nil {
			b.WriteString(run.Text)
			continue
		}
		for _, p := range out[k] {
			b.WriteString(p.Text)
			switch p.Em {
			case Gap:
				b.WriteString("|")
			case Compress:
				b.WriteString("~")
			}
		}
	}
	return b.String()
}

func TestRules(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"使用Go语言写的RSS阅读器", "使用|Go|语言写的|RSS|阅读器"},
		{"支持2种主题", "支持|2|种主题"},
		{"café里", "café|里"},
		{"使用 Go 语言", "使用 Go 语言"},
		{"价格50%，约$5", "价格|50%，约$5"},
		{"中文#标签", "中文#标签"},
		{"（见「设置」）。", "（见「设置」~）~。"},
		{"」。", "」~。"},
		{"，「", "，~「"},
		{"（「", "（~「"},
		{"「」", "「」"},
		{"」）。", "」~）~。"},
		{"。」", "。~」"},
		{"“引号”，‘引’", "“引号”，‘引’"},
		{"中·文——结束……", "中·文——结束……"},
		{"English text 123, only.", "English text 123, only."},
		{"只有English 123", "只有|English 123"},
		{"", ""},
	} {
		if got := spaced([]Run{{Text: tc.in}}); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEnglishNeedsNothing(t *testing.T) {
	if Layout([]Run{{Text: "A good feed reader, 2 of them."}}) != nil || Text("Hello, world") != nil {
		t.Error("plain English got pieces")
	}
}

func TestRunsPairAcrossMarkupButNotCode(t *testing.T) {
	cases := []struct {
		name string
		runs []Run
		want string
	}{
		{"link", []Run{{Text: "见"}, {Text: "Go"}, {Text: "语言"}}, "见|Go|语言"},
		{"empty run between", []Run{{Text: "见"}, {Text: ""}, {Text: "Go"}}, "见|Go"},
		{"punctuation across markup", []Run{{Text: "「好」"}, {Text: "。"}}, "「好」~。"},
		{"code breaks the gap", []Run{{Text: "用"}, {Text: "go", Code: true}, {Text: "写"}}, "用go写"},
		{"code breaks the pair", []Run{{Text: "」"}, {Text: "x", Code: true}, {Text: "。"}}, "」x。"},
		{"code is not spaced", []Run{{Text: "用Go，。", Code: true}}, "用Go，。"},
		{"space in the next run", []Run{{Text: "用"}, {Text: " Go"}}, "用 Go"},
	}
	for _, tc := range cases {
		if got := spaced(tc.runs); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestJoinedPiecesAreTheText(t *testing.T) {
	runs := []Run{{Text: "使用Go语言写的RSS阅读器，"}, {Text: "「好」。", Code: false}, {Text: "x := 1", Code: true}, {Text: "2种𠀀a"}, {Text: ""}}
	out := Layout(runs)
	if out == nil {
		t.Fatal("no pieces")
	}
	for k, run := range runs {
		var joined strings.Builder
		for _, p := range out[k] {
			joined.WriteString(p.Text)
			if p.Text == "" {
				t.Errorf("run %d: empty piece", k)
			}
		}
		if joined.String() != run.Text {
			t.Errorf("run %d: %q joined, want %q", k, joined.String(), run.Text)
		}
	}
	if got := spaced(runs); !strings.Contains(got, "𠀀|a") {
		t.Errorf("supplementary plane Han not spaced: %q", got)
	}
}
