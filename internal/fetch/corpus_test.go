package fetch

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// update rewrites the golden files from the current output: go test ./internal/fetch -run TestCorpus -update.
// Review the diff of testdata before committing it.
var update = flag.Bool("update", false, "rewrite the golden files of the feed corpus")

// corpusFeedBase is the address the corpus feeds are read from; relative links in them resolve against it.
const corpusFeedBase = "https://feeds.example.org/blog/feed.xml"

// corpusPageAddress is where a corpus page is read from, by file name. Pages not listed use corpusPageDefault.
// Discourse picks the post named by the address.
var corpusPageAddress = map[string]string{
	"discourse-topic.html": "https://forum.example.org/t/tomatoes-split-after-rain/77/3",
}

const corpusPageDefault = "https://pages.example.org/articles/post"

// goldenFeed is the reviewable form of a parsed feed: stable field names, UTC RFC 3339 times, and "" for no date.
type goldenFeed struct {
	Title   string        `json:"title"`
	SiteURL string        `json:"siteURL"`
	IconURL string        `json:"iconURL"`
	Entries []goldenEntry `json:"entries"`
}

type goldenEntry struct {
	GUID         string        `json:"guid"`
	Title        string        `json:"title"`
	URL          string        `json:"url"`
	Author       string        `json:"author"`
	Published    string        `json:"published"`
	Summary      string        `json:"summary"`
	ContentHTML  string        `json:"contentHTML"`
	ThumbnailURL string        `json:"thumbnailURL"`
	DurationSec  int           `json:"durationSec"`
	Platform     string        `json:"platform"`
	Language     string        `json:"language"`
	Images       []store.Image `json:"images"`
}

// goldenPage is the reviewable form of an extracted article page.
type goldenPage struct {
	Address string `json:"address"`
	HTML    string `json:"html"`
}

func newGoldenFeed(feed parsed) goldenFeed {
	out := goldenFeed{Title: feed.title, SiteURL: feed.siteURL, IconURL: feed.iconURL, Entries: []goldenEntry{}}
	for _, e := range feed.entries {
		published := ""
		if !e.PublishedAt.IsZero() {
			published = e.PublishedAt.UTC().Format(time.RFC3339)
		}
		images := e.Images
		if images == nil {
			images = []store.Image{}
		}
		out.Entries = append(out.Entries, goldenEntry{
			GUID: e.GUID, Title: e.Title, URL: e.URL, Author: e.Author, Published: published,
			Summary: e.Summary, ContentHTML: e.ContentHTML, ThumbnailURL: e.ThumbnailURL,
			DurationSec: e.DurationSec, Platform: e.Platform, Language: e.Language, Images: images,
		})
	}
	return out
}

// checkGolden compares got with the golden file, or rewrites the file under -update.
func checkGolden(t *testing.T, path string, got any) {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(got); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(want, b.Bytes()) {
		t.Errorf("%s differs from the output; run with -update and review the diff.\n got:\n%s\nwant:\n%s", path, b.Bytes(), want)
	}
}

// corpusFiles lists the samples of a directory: every file that is not a golden file.
func corpusFiles(t *testing.T, dir string) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join("testdata", dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range all {
		if !strings.HasSuffix(f, ".golden.json") {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		t.Fatalf("no samples in testdata/%s", dir)
	}
	return files
}

func goldenPath(sample string) string {
	return strings.TrimSuffix(sample, filepath.Ext(sample)) + ".golden.json"
}

func TestCorpusFeeds(t *testing.T) {
	base, _ := url.Parse(corpusFeedBase)
	for _, sample := range corpusFiles(t, "feeds") {
		t.Run(filepath.Base(sample), func(t *testing.T) {
			body, err := os.ReadFile(sample)
			if err != nil {
				t.Fatal(err)
			}
			feed, err := parse(body, "", base)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, goldenPath(sample), newGoldenFeed(feed))
		})
	}
}

func TestCorpusPages(t *testing.T) {
	for _, sample := range corpusFiles(t, "pages") {
		name := filepath.Base(sample)
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(sample)
			if err != nil {
				t.Fatal(err)
			}
			address := corpusPageDefault
			if a, ok := corpusPageAddress[name]; ok {
				address = a
			}
			page, _ := url.Parse(address)
			// The poller hands the response Content-Type to the decoder; a page declares its charset in a meta tag.
			decoded, err := charset.NewReader(bytes.NewReader(body), "text/html")
			if err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(decoded)
			if err != nil {
				t.Fatal(err)
			}
			content, err := articleHTML(doc, page)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, goldenPath(sample), goldenPage{Address: address, HTML: content})
		})
	}
}
