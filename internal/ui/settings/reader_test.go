package settings

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/locale"
)

// fakeToken is an obvious stand-in; no real credential is used anywhere.
const fakeToken = "fake-token-0000"

// openReaderDialog shows the Import from Reader dialog on the Import and Export page.
func openReaderDialog(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, true)
	press(t, tt, s.text.OPML)
	press(t, tt, s.text.ReaderButton)
	if !s.reader.open || !tt.HasText(s.text.ReaderTitle) {
		t.Fatalf("the dialog is not open: %v", tt.Texts())
	}
	return s, f, tt
}

// connect fills the form and presses Connect, and runs the request.
func connect(t *testing.T, s *Screen, f *fake, tt *ui.Tester) {
	t.Helper()
	edit(tt, func() { s.reader.base, s.reader.cred = "https://reader.example.com/", fakeToken })
	press(t, tt, s.text.ReaderConnect)
	f.settle(tt)
}

func eachLanguage(t *testing.T, run func(t *testing.T, language string)) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) { run(t, language) })
	}
}

func TestReaderForm(t *testing.T) {
	eachLanguage(t, func(t *testing.T, language string) {
		s, f, tt := openReaderDialog(t, language)
		for _, text := range []string{s.text.ReaderService, s.text.ReaderAddress, s.text.ReaderToken, s.text.ReaderMemory} {
			if !tt.HasText(text) {
				t.Errorf("no %q in %v", text, tt.Texts())
			}
		}
		if tt.HasText(s.text.ReaderUser) {
			t.Error("Miniflux asks for a user name")
		}
		if s.reader.ready() {
			t.Fatal("an empty form is ready")
		}
		edit(tt, func() { s.reader.base = "reader.example.com" })
		if s.reader.ready() || !tt.HasText(s.text.ReaderErrAddress) {
			t.Errorf("a bad address is not shown: %v", tt.Texts())
		}
		edit(tt, func() { s.reader.base, s.reader.cred = "http://reader.example.com", "  " })
		if s.reader.ready() || tt.HasText(s.text.ReaderErrAddress) {
			t.Errorf("a blank credential is ready, or a good address is refused")
		}
		s.connectReader()
		if len(f.queue) != 0 || len(f.importReqs) != 0 {
			t.Fatal("Connect sent an incomplete form")
		}

		edit(tt, func() { s.reader.service = 1 })
		if !tt.HasText(s.text.ReaderUser) || !tt.HasText(s.text.ReaderPassword) {
			t.Errorf("FreshRSS shows no user name and password: %v", tt.Texts())
		}
		edit(tt, func() { s.reader.cred = fakeToken })
		if s.reader.ready() {
			t.Error("FreshRSS is ready without a user name")
		}
		edit(tt, func() { s.reader.user = "alice" })
		if !s.reader.ready() {
			t.Error("a complete FreshRSS form is not ready")
		}
	})
}

func TestReaderImports(t *testing.T) {
	eachLanguage(t, func(t *testing.T, language string) {
		s, f, tt := openReaderDialog(t, language)
		f.startAns = client.ImportStatus{Phase: "fetching", Service: client.ServiceMiniflux}
		edit(tt, func() { s.reader.base, s.reader.cred = " https://reader.example.com/ ", " "+fakeToken+" " })
		press(t, tt, s.text.ReaderConnect)
		if s.reader.cred != "" {
			t.Fatal("the credential stays after Connect")
		}
		if !tt.HasText(s.text.ReaderStageSubscriptions) {
			t.Fatalf("no progress: %v", tt.Texts())
		}
		f.settle(tt)
		want := client.ImportRequest{Service: client.ServiceMiniflux, BaseURL: "https://reader.example.com/", Credential: fakeToken}
		if len(f.importReqs) != 1 || f.importReqs[0] != want {
			t.Fatalf("sent %+v", f.importReqs)
		}

		// Nothing is asked before its time; once it comes the progress and then the preview show.
		f.statusAns = client.ImportStatus{Phase: "fetching", Progress: client.ImportProgress{Stage: "starred", Done: 40, Total: 100}}
		tt.Frame()
		f.settle(tt)
		if f.polls != 0 {
			t.Fatalf("asked %d times too early", f.polls)
		}
		f.later += time.Second
		tt.Frame()
		f.settle(tt)
		if f.polls != 1 || !tt.HasText(s.text.ReaderStageStarred) ||
			!tt.HasText(s.fill(s.text.ReaderProgress, "{done}", "40", "{total}", "100")) {
			t.Fatalf("polls %d, texts %v", f.polls, tt.Texts())
		}
		f.statusAns = client.ImportStatus{Phase: "ready", Counts: client.ImportCounts{Sources: 12, Folders: 3, Starred: 1200, Read: 5}}
		f.later += time.Second
		tt.Frame()
		f.settle(tt)
		for _, text := range []string{s.text.ReaderSources, "12", s.text.ReaderStarred, "1,200", s.text.ReaderRead,
			s.text.ReaderFolders, s.text.ReaderFoldersNote, s.text.ReaderImport} {
			if !tt.HasText(text) {
				t.Errorf("no %q in %v", text, tt.Texts())
			}
		}
		if f.refreshed != 0 {
			t.Fatal("the preview refreshed the lists")
		}

		f.confirmAns = client.ImportStatus{Phase: "writing", Progress: client.ImportProgress{Stage: "writing", Done: 1, Total: 4}}
		// The page has an Import button of its own, so the dialog's is the one beside Cancel.
		pressBeside(t, tt, s.text.ReaderCancel)
		f.settle(tt)
		if !tt.HasText(s.text.ReaderStageWriting) {
			t.Fatalf("no writing: %v", tt.Texts())
		}
		f.statusAns = client.ImportStatus{Phase: "done", Result: &client.ImportResult{AddedSources: 2, ExistingSources: 1,
			StarredAdded: 3, StarredExisting: 1, ReadMarked: 4, UnmatchedRead: 2, FoldersSkipped: 3, SkippedTotal: 3,
			Skipped: []client.ImportSkip{
				{Kind: "source", Title: "Broken", Address: "ftp://x.example/feed", Error: "invalid_url"},
				{Kind: "entry", Address: "https://c.example/s/3", Error: "unknown_source"},
			}}}
		f.later += time.Second
		tt.Frame()
		f.settle(tt)
		for _, text := range []string{
			locale.Count(locale.Count(s.text.ReaderResultSources, "added", 2), "existing", 1),
			locale.Count(locale.Count(s.text.ReaderResultStarred, "added", 3), "existing", 1),
			locale.Count(s.text.ReaderResultRead, "n", 4),
			locale.Count(s.text.ReaderResultUnmatched, "n", 2),
			locale.Count(s.text.ReaderResultFolders, "n", 3),
			s.text.ReaderSkipped, "https://c.example/s/3", s.text.ReaderUnknownSource, s.text.InvalidURL,
			s.fill(s.text.ReaderMore, "{n}", "1"), s.text.ReaderDone,
		} {
			if !tt.HasText(text) {
				t.Errorf("no %q in %v", text, tt.Texts())
			}
		}
		if f.refreshed != 1 {
			t.Fatalf("refreshed %d", f.refreshed)
		}
		polls := f.polls
		f.later += time.Second
		tt.Frame()
		f.settle(tt)
		if f.refreshed != 1 || f.polls != polls {
			t.Fatalf("a finished import is asked again: refreshed %d, polls %d, was %d", f.refreshed, f.polls, polls)
		}
		press(t, tt, s.text.ReaderDone)
		f.settle(tt)
		if s.reader.open || f.cancels != 1 || tt.HasText(s.text.ReaderTitle) {
			t.Fatalf("open %t, cancels %d", s.reader.open, f.cancels)
		}
	})
}

func TestReaderFailsAndRetries(t *testing.T) {
	eachLanguage(t, func(t *testing.T, language string) {
		s, f, tt := openReaderDialog(t, language)
		edit(tt, func() { s.reader.service, s.reader.user = 1, "alice" })
		f.startErr = &client.APIError{Code: "unreachable"}
		connect(t, s, f, tt)
		if !tt.HasText(s.text.ReaderErrUnreachable) || !tt.HasText(s.text.ReaderTryAgain) {
			t.Fatalf("no failure: %v", tt.Texts())
		}
		if f.importReqs[0].Username != "alice" || f.importReqs[0].Service != client.ServiceFreshRSS {
			t.Fatalf("sent %+v", f.importReqs[0])
		}
		press(t, tt, s.text.ReaderTryAgain)
		if !s.reader.open || s.reader.step() != stepForm || s.reader.cred != "" || s.reader.base != "https://reader.example.com/" ||
			s.reader.user != "alice" || s.reader.service != 1 {
			t.Fatalf("the form came back as %+v", s.reader)
		}

		// A job that fails while it reads shows the code the server gives, and Close clears it.
		f.startErr = nil
		f.startAns = client.ImportStatus{Phase: "fetching"}
		edit(tt, func() { s.reader.cred = fakeToken })
		press(t, tt, s.text.ReaderConnect)
		f.settle(tt)
		f.statusAns = client.ImportStatus{Phase: "failed", Error: "unauthorized"}
		f.later += time.Second
		tt.Frame()
		f.settle(tt)
		if !tt.HasText(s.text.ReaderErrUnauthorized) {
			t.Fatalf("no failure: %v", tt.Texts())
		}
		press(t, tt, s.text.ReaderClose)
		f.settle(tt)
		if s.reader.open || f.cancels != 1 {
			t.Fatalf("open %t, cancels %d", s.reader.open, f.cancels)
		}
	})
}

func TestReaderCancels(t *testing.T) {
	eachLanguage(t, func(t *testing.T, language string) {
		// Before anything is sent there is nothing to cancel.
		s, f, tt := openReaderDialog(t, language)
		press(t, tt, s.text.ReaderCancel)
		f.settle(tt)
		if s.reader.open || f.cancels != 0 || len(f.importReqs) != 0 {
			t.Fatalf("open %t, cancels %d, sent %d", s.reader.open, f.cancels, len(f.importReqs))
		}

		// Cancelling a preview drops it and sends nothing more.
		s, f, tt = openReaderDialog(t, language)
		f.startAns = client.ImportStatus{Phase: "ready", Counts: client.ImportCounts{Sources: 1}}
		connect(t, s, f, tt)
		if !tt.HasText(s.text.ReaderImport) {
			t.Fatalf("no preview: %v", tt.Texts())
		}
		press(t, tt, s.text.ReaderCancel)
		f.settle(tt)
		if s.reader.open || f.cancels != 1 || f.refreshed != 0 {
			t.Fatalf("open %t, cancels %d, refreshed %d", s.reader.open, f.cancels, f.refreshed)
		}

		// Leaving while the request is out drops the answer and cancels the job it made.
		s, f, tt = openReaderDialog(t, language)
		f.startAns = client.ImportStatus{Phase: "fetching"}
		edit(tt, func() { s.reader.base, s.reader.cred = "https://reader.example.com", fakeToken })
		press(t, tt, s.text.ReaderConnect)
		s.closeReader()
		f.settle(tt)
		if s.reader.open || s.reader.started || f.cancels != 1 {
			t.Fatalf("open %t, started %t, cancels %d", s.reader.open, s.reader.started, f.cancels)
		}
	})
}

func TestReaderExpired(t *testing.T) {
	s, f, tt := openReaderDialog(t, "en")
	f.startErr = client.ErrUnauthorized
	connect(t, s, f, tt)
	if f.expired != 1 || s.reader.open {
		t.Fatalf("expired %d, open %t", f.expired, s.reader.open)
	}
	if s.reader.cred != "" || strings.Contains(strings.Join(tt.Texts(), "\n"), fakeToken) {
		t.Fatal("the credential outlived the session")
	}
}

// pressBeside clicks the button to the right of the one labeled text, in the middle of its row.
func pressBeside(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no %q in %v", text, tt.Texts())
	}
	tt.ClickAt(r.X+r.W+60, r.Y+r.H/2)
}
