package fetch

import (
	"strings"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"simplified", "今天我们发布了新的版本，这个版本带来了更快的构建速度和更小的程序体积，欢迎大家下载试用。", "zh-Hans"},
		{"traditional", "今天我們發佈了新的版本，這個版本帶來了更快的構建速度和更小的程式體積，歡迎大家下載試用。", "zh-Hant"},
		{"english", "Today the team is happy to release a new version with faster builds and smaller binaries.", "en"},
		{"german", "Heute veröffentlichen wir eine neue Version mit schnelleren Builds und kleineren Programmen für alle.", "de"},
		{"japanese", "今日は新しいバージョンをリリースしました。ビルドが速くなり、プログラムも小さくなりました。", "ja"},
		{"mixed keeps the main language", "这次发布的 Go 1.27 带来了更快的构建速度和更小的二进制文件，我们建议所有用户升级到这个版本。", "zh-Hans"},
		{"too short", "Hello world", ""},
		{"short chinese", "你好，世界", ""},
		{"empty", "", ""},
		{"digits and symbols", "1234567890 !!! 1234567890 ??? 1234567890 ...", ""},
	}
	for _, c := range cases {
		if got := detectLanguage(c.text); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestChineseScriptTieIsSimplified(t *testing.T) {
	if got := chineseScript("这們"); got != "zh-Hans" {
		t.Fatalf("tie: %q", got)
	}
	if got := chineseScript("没有区别"); got != "zh-Hans" {
		t.Fatalf("no marker: %q", got)
	}
}

func TestDetectLanguageReadsOnlyASample(t *testing.T) {
	// The tail is far beyond the sample, so it cannot change the answer.
	text := strings.Repeat("Today the team is happy to release a new version of the language, with faster builds and smaller binaries for everyone who wants to try it. ", 30) + strings.Repeat("这是一段很长的中文内容。", 2000)
	if got := detectLanguage(text); got != "en" {
		t.Fatalf("got %q", got)
	}
}

func TestEntryLanguageUsesTitleAndText(t *testing.T) {
	if got := entryLanguage("Go 1.27 is released", "Today the Go team is happy to release the new version."); got != "en" {
		t.Fatalf("got %q", got)
	}
	if got := entryLanguage("Hi", ""); got != "" {
		t.Fatalf("short entry: %q", got)
	}
}

func TestParseSetsLanguage(t *testing.T) {
	feed := mustParse(t, `<?xml version="1.0"?><rss version="2.0"><channel><title>T</title>
<item><guid>a</guid><title>Release notes</title><description>Today the team is happy to release a new version with faster builds.</description></item>
<item><guid>b</guid><title>Hi</title><description>Short</description></item>
</channel></rss>`, "https://blog.example.org/feed.xml")
	if feed.entries[0].Language != "en" || feed.entries[1].Language != "" {
		t.Fatalf("languages: %q %q", feed.entries[0].Language, feed.entries[1].Language)
	}
}
