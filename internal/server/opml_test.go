package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/fetch"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const importFile = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Elsewhere</title></head>
  <body>
    <outline text="Blog" title="Blog" type="rss" xmlUrl=" https://example.org/feed "/>
    <outline text="Tech">
      <outline text="Deep">
        <outline text="Talks" xmlUrl="https://example.org/talks" orielKind="video"/>
      </outline>
      <outline text="Again" xmlUrl="https://Example.org:443/feed/"/>
      <outline text="Old" xmlUrl="https://example.org/old"/>
      <outline text="Gopher" xmlUrl="ftp://example.org/feed"/>
      <outline text="Odd" xmlUrl="https://example.org/odd" orielKind="podcast"/>
    </outline>
    <outline text="/github/issue/yangflow/oriel" orielRsshubPath="/github/issue/yangflow/oriel" orielKind="social"
      xmlUrl="https://rsshub.example.org/github/issue/yangflow/oriel"/>
  </body>
</opml>`

// subscriptions lists every source as "kind address title", sorted.
func subscriptions(t *testing.T, s *Server, header map[string]string) []string {
	t.Helper()
	var got []string
	for _, item := range decode(t, do(s, "GET", "/api/sources", "", header))["sources"].([]any) {
		src := item.(map[string]any)
		got = append(got, fmt.Sprintf("%s %s%s %s", src["kind"], src["url"], src["rsshubPath"], src["title"]))
	}
	slices.Sort(got)
	return got
}

func TestOPMLImport(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, r := range []struct{ method, target string }{{"POST", "/api/opml/import"}, {"GET", "/api/opml/export"}} {
		if w := do(s, r.method, r.target, importFile, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", r.target, w.Code)
		}
	}
	do(s, "POST", "/api/sources", `{"url":"https://example.org/old","kind":"article"}`, bearer(token))

	w := do(s, "POST", "/api/opml/import", importFile, bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["added"] != 3.0 || body["existing"] != 2.0 {
		t.Fatalf("import: %d %v", w.Code, body)
	}
	if got := fmt.Sprint(body["failed"]); got != "[map[address:ftp://example.org/feed error:invalid_url title:Gopher] "+
		"map[address:https://example.org/odd error:invalid_kind title:Odd]]" {
		t.Fatalf("failed: %s", got)
	}
	if got := subscriptions(t, s, bearer(token)); fmt.Sprint(got) != "[article https://example.org/feed Blog article https://example.org/old  "+
		"social /github/issue/yangflow/oriel  video https://example.org/talks Talks]" {
		t.Fatalf("sources: %q", got)
	}

	for body, want := range map[string]struct {
		status int
		code   string
	}{
		`<rss version="2.0"><channel></channel></rss>`: {http.StatusBadRequest, "invalid_opml"},
		`not xml`: {http.StatusBadRequest, "invalid_opml"},
		"<opml><body>" + strings.Repeat(" ", maxOPML) + "</body></opml>": {http.StatusRequestEntityTooLarge, "opml_too_large"},
	} {
		w := do(s, "POST", "/api/opml/import", body, bearer(token))
		if w.Code != want.status || decode(t, w)["error"] != want.code {
			t.Errorf("%.30s: %d %s", body, w.Code, w.Body)
		}
	}
}

func TestOPMLRoundTrip(t *testing.T) {
	s, _ := newServer(t, "")
	do(s, "POST", "/api/opml/import", importFile, nil)
	s.store.SetSetting(context.Background(), fetch.RSSHubSetting, "http://localhost:1200/\nhttp://backup.example")
	w := do(s, "GET", "/api/opml/export", "", nil)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/x-opml") ||
		!strings.Contains(w.Body.String(), `xmlUrl="http://localhost:1200/github/issue/yangflow/oriel"`) {
		t.Fatalf("export: %d %v %s", w.Code, w.Header(), w.Body)
	}

	again, _ := newServer(t, "")
	if body := decode(t, do(again, "POST", "/api/opml/import", w.Body.String(), nil)); body["added"] != 4.0 || len(body["failed"].([]any)) != 0 {
		t.Fatalf("reimport: %v", body)
	}
	if got, want := subscriptions(t, again, nil), subscriptions(t, s, nil); !slices.Equal(got, want) {
		t.Fatalf("round trip:\n got %q\nwant %q", got, want)
	}
}

func TestOPMLImportSpreadsFirstFetches(t *testing.T) {
	s, c := newServer(t, "")
	var file strings.Builder
	file.WriteString(`<opml version="2.0"><body>`)
	for i := range 300 {
		fmt.Fprintf(&file, `<outline text="s%d" xmlUrl="https://example.org/%d"/>`, i, i)
	}
	file.WriteString(`</body></opml>`)
	if w := do(s, "POST", "/api/opml/import", file.String(), nil); w.Code != http.StatusOK {
		t.Fatalf("import: %d", w.Code)
	}
	sources, err := s.store.Sources(context.Background())
	if err != nil || len(sources) != 300 {
		t.Fatalf("%d sources, %v", len(sources), err)
	}
	// A file of 300 spreads over the ten minute maximum: not all due at once, none later than the window.
	due, latest := 0, time.Duration(0)
	for _, src := range sources {
		delay := src.NextFetchAt.Sub(c.now())
		if delay < 0 || delay >= importSpreadMax {
			t.Fatalf("first fetch in %s", delay)
		}
		if delay == 0 {
			due++
		}
		latest = max(latest, delay)
	}
	if due > 10 || latest < importSpreadMax/2 {
		t.Errorf("%d due at once, the latest in %s", due, latest)
	}

	// A source added by hand is fetched at once, and a file of one waits only a moment.
	one := `<opml version="2.0"><body><outline text="x" xmlUrl="https://example.org/single"/></body></opml>`
	do(s, "POST", "/api/opml/import", one, nil)
	do(s, "POST", "/api/sources", `{"url":"https://example.org/manual","kind":"article"}`, nil)
	sources, _ = s.store.Sources(context.Background())
	for _, src := range sources {
		if strings.HasSuffix(src.URL, "/manual") && !src.NextFetchAt.Equal(c.now()) {
			t.Errorf("a source added by hand is due in %s", src.NextFetchAt.Sub(c.now()))
		}
		if strings.HasSuffix(src.URL, "/single") && src.NextFetchAt.Sub(c.now()) >= importSpreadEach {
			t.Errorf("a one-source file waits %s", src.NextFetchAt.Sub(c.now()))
		}
	}
}

func TestOPMLImportOtherEncodings(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().String(`<?xml version="1.0" encoding="GBK"?>
<opml version="2.0"><body><outline text="博客" type="rss" xmlUrl="https://example.org/gbk"/></body></opml>`)
	if err != nil {
		t.Fatal(err)
	}
	latin, err := charmap.ISO8859_1.NewEncoder().String(`<?xml version="1.0" encoding="ISO-8859-1"?>
<opml version="2.0"><body><outline text="Café" type="rss" xmlUrl="https://example.org/latin"/></body></opml>`)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, file := range []string{gbk, latin} {
		w := do(s, "POST", "/api/opml/import", file, bearer(token))
		if body := decode(t, w); w.Code != http.StatusOK || body["added"] != 1.0 {
			t.Fatalf("import: %d %v", w.Code, body)
		}
	}
	if got := fmt.Sprint(subscriptions(t, s, bearer(token))); got != "[article https://example.org/gbk 博客 article https://example.org/latin Café]" {
		t.Errorf("sources: %s", got)
	}
}

// opmlFile builds an OPML file of n subscriptions, one of them, when bad is true, with an address no one can add.
func opmlFile(n int, bad bool) string {
	var file strings.Builder
	file.WriteString(`<opml version="2.0"><body><outline text="Group">`)
	for i := range n {
		address := fmt.Sprintf("https://example.org/%d", i)
		if bad && i == 1 {
			address = "ftp://example.org/feed"
		}
		fmt.Fprintf(&file, `<outline text="s%d" xmlUrl="%s"/>`, i, address)
	}
	file.WriteString(`</outline></body></opml>`)
	return file.String()
}

// opmlStatusOf reads the status of the import job.
func opmlStatusOf(t *testing.T, s *Server, header map[string]string) map[string]any {
	t.Helper()
	w := do(s, "GET", "/api/opml/import", "", header)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	return decode(t, w)
}

// waitOPML waits until the job has left the running phase.
func waitOPML(t *testing.T, s *Server, header map[string]string) map[string]any {
	t.Helper()
	for range 500 {
		if st := opmlStatusOf(t, s, header); st["phase"] != "running" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the job did not end")
	return nil
}

func TestOPMLJobReportsProgress(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	header := bearer(token)
	if w := do(s, "POST", "/api/opml/import/start", importFile, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("start without session: %d", w.Code)
	}
	if w := do(s, "GET", "/api/opml/import", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("status without session: %d", w.Code)
	}
	if st := opmlStatusOf(t, s, header); st["phase"] != "idle" || st["id"] != nil {
		t.Fatalf("before any job: %v", st)
	}

	// The job holds after its second subscription, so the status can be read in the middle.
	reached, release := make(chan struct{}), make(chan struct{})
	s.opml.step = func(done int) {
		if done == 2 {
			close(reached)
			<-release
		}
	}
	w := do(s, "POST", "/api/opml/import/start", opmlFile(5, true), header)
	started := decode(t, w)
	id, _ := started["id"].(string)
	if w.Code != http.StatusAccepted || id == "" || started["phase"] != "running" || fmt.Sprint(started["progress"]) != "map[done:0 total:5]" {
		t.Fatalf("start: %d %v", w.Code, started)
	}
	<-reached
	if st := opmlStatusOf(t, s, header); st["id"] != id || st["phase"] != "running" || fmt.Sprint(st["progress"]) != "map[done:2 total:5]" || st["result"] != nil {
		t.Fatalf("in the middle: %v", st)
	}

	// While it runs, both routes turn another import away, and the file is still checked first.
	for _, target := range []string{"/api/opml/import", "/api/opml/import/start"} {
		w := do(s, "POST", target, importFile, header)
		if w.Code != http.StatusConflict || decode(t, w)["error"] != "opml_import_busy" {
			t.Errorf("%s while running: %d %s", target, w.Code, w.Body)
		}
	}
	if w := do(s, "POST", "/api/opml/import/start", "not xml", header); w.Code != http.StatusBadRequest {
		t.Errorf("a bad file while running: %d", w.Code)
	}
	close(release)

	st := waitOPML(t, s, header)
	if st["id"] != id || st["phase"] != "done" || fmt.Sprint(st["progress"]) != "map[done:5 total:5]" || st["error"] != nil ||
		fmt.Sprint(st["result"]) != "map[added:4 existing:0 failed:[map[address:ftp://example.org/feed error:invalid_url title:s1]]]" {
		t.Fatalf("done: %v", st)
	}
	// The result stays until the next job, and a second job gets another id, with every subscription already there.
	if again := opmlStatusOf(t, s, header); fmt.Sprint(again) != fmt.Sprint(st) {
		t.Fatalf("kept: %v", again)
	}
	s.opml.step = nil
	next := decode(t, do(s, "POST", "/api/opml/import/start", opmlFile(5, true), header))
	if next["id"] == id {
		t.Fatalf("the same id twice: %v", next)
	}
	if st := waitOPML(t, s, header); st["id"] != next["id"] || fmt.Sprint(st["result"]) != "map[added:0 existing:4 failed:[map[address:ftp://example.org/feed error:invalid_url title:s1]]]" {
		t.Fatalf("second job: %v", st)
	}
	// The synchronous route works again once the job is over.
	if w := do(s, "POST", "/api/opml/import", importFile, header); w.Code != http.StatusOK {
		t.Fatalf("sync after the job: %d %s", w.Code, w.Body)
	}
}

func TestOPMLJobWithoutSubscriptionsIsDoneAtOnce(t *testing.T) {
	s, _ := newServer(t, "")
	do(s, "POST", "/api/opml/import/start", `<opml version="2.0"><body><outline text="Empty"/></body></opml>`, nil)
	if st := waitOPML(t, s, nil); st["phase"] != "done" || fmt.Sprint(st["progress"]) != "map[done:0 total:0]" {
		t.Fatalf("empty file: %v", st)
	}
}

func TestOPMLSyncImportHoldsTheLock(t *testing.T) {
	s, _ := newServer(t, "")
	if !s.opml.begin() {
		t.Fatal("the lock was taken")
	}
	for _, target := range []string{"/api/opml/import", "/api/opml/import/start"} {
		w := do(s, "POST", target, importFile, nil)
		if w.Code != http.StatusConflict || decode(t, w)["error"] != "opml_import_busy" {
			t.Errorf("%s during a synchronous import: %d %s", target, w.Code, w.Body)
		}
	}
	s.opml.endSync()
	if w := do(s, "POST", "/api/opml/import/start", importFile, nil); w.Code != http.StatusAccepted {
		t.Fatalf("after it: %d %s", w.Code, w.Body)
	}
}

func TestOPMLJobRejectsBadFilesBeforeStarting(t *testing.T) {
	s, _ := newServer(t, "")
	for body, want := range map[string]struct {
		status int
		code   string
	}{
		`<rss version="2.0"><channel></channel></rss>`: {http.StatusBadRequest, "invalid_opml"},
		`not xml`: {http.StatusBadRequest, "invalid_opml"},
		"<opml><body>" + strings.Repeat(" ", maxOPML) + "</body></opml>": {http.StatusRequestEntityTooLarge, "opml_too_large"},
	} {
		w := do(s, "POST", "/api/opml/import/start", body, nil)
		if w.Code != want.status || decode(t, w)["error"] != want.code {
			t.Errorf("%.30s: %d %s", body, w.Code, w.Body)
		}
		if st := opmlStatusOf(t, s, nil); st["phase"] != "idle" {
			t.Errorf("%.30s started a job: %v", body, st)
		}
	}
}

func TestOPMLJobFailsWithTheCountsSoFar(t *testing.T) {
	s, _ := newServer(t, "")
	// The server shuts down after the second subscription: the third cannot be written.
	s.opml.step = func(done int) {
		if done == 2 {
			s.opml.cancel()
		}
	}
	do(s, "POST", "/api/opml/import/start", opmlFile(5, false), nil)
	st := waitOPML(t, s, nil)
	if st["phase"] != "failed" || st["error"] != "import_failed" || fmt.Sprint(st["result"]) != "map[added:2 existing:0 failed:[]]" {
		t.Fatalf("failed: %v", st)
	}
	if got := subscriptions(t, s, nil); len(got) != 2 {
		t.Fatalf("the added ones stay: %q", got)
	}
}

func TestOPMLJobPanicFailsTheJob(t *testing.T) {
	s, _ := newServer(t, "")
	s.opml.step = func(int) { panic("boom") }
	do(s, "POST", "/api/opml/import/start", opmlFile(3, false), nil)
	if st := waitOPML(t, s, nil); st["phase"] != "failed" || st["error"] != "import_failed" {
		t.Fatalf("panic: %v", st)
	}
	if w := do(s, "POST", "/api/opml/import/start", importFile, nil); w.Code != http.StatusAccepted {
		t.Fatalf("after a panic: %d", w.Code)
	}
}

func TestOPMLJobDoesNotStartAfterClose(t *testing.T) {
	s, _ := newServer(t, "")
	s.Close()
	w := do(s, "POST", "/api/opml/import/start", importFile, nil)
	if w.Code != http.StatusServiceUnavailable || decode(t, w)["error"] != "server_closing" {
		t.Fatalf("start after Close: %d %s", w.Code, w.Body)
	}
	if st := opmlStatusOf(t, s, nil); st["phase"] != "idle" {
		t.Fatalf("a job started: %v", st)
	}
}
