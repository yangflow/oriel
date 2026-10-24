package settings

import (
	"errors"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

func running(done, total int) client.OPMLImport {
	job := client.OPMLImport{Phase: client.OPMLRunning}
	job.Progress.Done, job.Progress.Total = done, total
	return job
}

// processed picks the announcements of the count out of everything announced.
func processed(all []string) []string {
	var heard []string
	for _, a := range all {
		if strings.HasPrefix(a, "Processed ") {
			heard = append(heard, a)
		}
	}
	return heard
}

func TestImportProgress(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.imported = client.Imported{Added: 8, Existing: 2}
	f.jobPoll = func(n int) (client.OPMLImport, error) {
		if n == 1 {
			return running(4, 10), nil
		}
		return client.OPMLImport{}, nil
	}
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	if f.jobPolls != 0 {
		t.Fatalf("asked %d times before its time", f.jobPolls)
	}
	f.after(tt, importEvery-1)
	if f.jobPolls != 0 {
		t.Fatalf("asked %d times too early", f.jobPolls)
	}
	f.after(tt, 1)
	if f.jobPolls != 1 || !tt.HasText("Processed 4 of 10") || !tt.HasText("Importing feeds.opml…") {
		t.Fatalf("%d polls, texts %v", f.jobPolls, tt.Texts())
	}
	if !s.busy || s.result.text != "" {
		t.Fatal("the import is not shown as under way")
	}
	f.after(tt, importEvery)
	if s.run != nil || s.busy || f.refreshed != 1 {
		t.Fatalf("run %v, busy %v, refreshed %d", s.run, s.busy, f.refreshed)
	}
	for _, text := range []string{"Imported feeds.opml: 8 added, 2 already subscribed.", s.text.ViewSources} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText("Processed 4 of 10") {
		t.Error("the progress stays after the import")
	}
	if err := tt.Click(s.text.ViewSources); err != nil {
		t.Fatal(err)
	}
	if f.viewed != 1 {
		t.Fatalf("viewed %d", f.viewed)
	}
}

func TestImportNothingToView(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.imported = client.Imported{Existing: 5}
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	f.finish(tt, s)
	if !tt.HasText("Imported feeds.opml: 0 added, 5 already subscribed.") || tt.HasText(s.text.ViewSources) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestImportStopped(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.jobPoll = func(int) (client.OPMLImport, error) {
		return client.OPMLImport{Phase: client.OPMLFailed, Error: "stopped",
			Result: &client.Imported{Added: 4, Existing: 1, Failed: []client.ImportFail{{Address: "x", Error: "invalid_url"}}}}, nil
	}
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	f.finish(tt, s)
	for _, text := range []string{"Stopped importing feeds.opml: 4 added before it stopped, 1 already subscribed.", s.text.ViewSources, "x"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if s.result.tone != partly || f.refreshed != 1 {
		t.Fatalf("tone %v, refreshed %d", s.result.tone, f.refreshed)
	}
}

func TestImportLost(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.jobPoll = func(int) (client.OPMLImport, error) { return client.OPMLImport{}, errors.New("offline") }
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	for i := 1; i <= 3; i++ {
		f.after(tt, importEvery)
		if s.run == nil || f.jobPolls != i {
			t.Fatalf("lost after %d failed answers", i)
		}
	}
	f.after(tt, importEvery)
	if s.run != nil || s.busy || f.jobPolls != 4 {
		t.Fatalf("run %v, busy %v, %d polls", s.run, s.busy, f.jobPolls)
	}
	if want := "Lost the connection while importing feeds.opml."; !tt.HasText(s.fill(s.text.Lost, "{file}", "feeds.opml")) || s.result.tone != partly {
		t.Fatalf("no %q in %v", want, tt.Texts())
	}
	if tt.HasText(s.text.ViewSources) || f.refreshed != 0 {
		t.Fatal("a lost import offers to view subscriptions")
	}
}

func TestImportLostResetsOnAnswer(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.jobPoll = func(n int) (client.OPMLImport, error) {
		switch {
		case n <= 3:
			return client.OPMLImport{}, errors.New("offline")
		case n == 4:
			return running(1, 2), nil
		case n <= 7:
			return client.OPMLImport{ID: "other", Phase: client.OPMLRunning}, nil
		}
		return client.OPMLImport{}, nil
	}
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	for range 7 {
		f.after(tt, importEvery)
	}
	if s.run == nil || s.run.lost != 3 {
		t.Fatalf("three failures and three answers of another job already lost the import: %+v", s.run)
	}
	f.after(tt, importEvery)
	if s.run != nil || !tt.HasText("Imported feeds.opml: 0 added, 0 already subscribed.") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestImportAnnouncesQuarters(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	done := []int{1, 9, 10, 12, 19, 20, 30, 31, 35}
	f.jobPoll = func(n int) (client.OPMLImport, error) {
		if n <= len(done) {
			return running(done[n-1], 40), nil
		}
		return client.OPMLImport{}, nil
	}
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	for range done {
		f.after(tt, importEvery)
	}
	heard := processed(tt.Announcements())
	want := []string{"Processed 10 of 40", "Processed 20 of 40", "Processed 30 of 40"}
	if len(heard) != len(want) {
		t.Fatalf("heard %q", heard)
	}
	for i := range want {
		if heard[i] != want[i] {
			t.Errorf("heard %q, want %q", heard, want)
		}
	}
}

func TestImportShortFileIsNotAnnounced(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.jobPoll = func(n int) (client.OPMLImport, error) {
		if n <= 4 {
			return running(n*4, 19), nil
		}
		return client.OPMLImport{}, nil
	}
	s.ImportFiles([]string{writeOPML(t, "feeds.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	for range 4 {
		f.after(tt, importEvery)
	}
	if heard := processed(tt.Announcements()); len(heard) != 0 {
		t.Fatalf("announced %q for a short file", heard)
	}
}

func TestImportSeveralResetsPerFile(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.imported = client.Imported{Added: 1}
	f.jobPoll = func(n int) (client.OPMLImport, error) {
		switch n {
		case 1:
			return running(30, 40), nil
		case 2:
			return client.OPMLImport{}, nil
		case 3:
			return running(2, 40), nil
		}
		return client.OPMLImport{}, nil
	}
	s.ImportFiles([]string{writeOPML(t, "a.opml", "<opml/>"), writeOPML(t, "b.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("Importing a.opml (file 1 of 2)…") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.after(tt, importEvery)
	if s.run.said != 3 {
		t.Fatalf("said %d", s.run.said)
	}
	f.after(tt, importEvery)
	// The first file is done: its progress is gone and the second starts from nothing.
	if s.run == nil || s.run.next != 1 || s.run.said != 0 || s.run.job.Progress.Done != 0 || len(f.sent) != 2 {
		t.Fatalf("run %+v, sent %d", s.run, len(f.sent))
	}
	if !tt.HasText("Importing b.opml (file 2 of 2)…") || tt.HasText("Processed 30 of 40") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.after(tt, importEvery)
	if !tt.HasText("Processed 2 of 40") || s.run.said != 0 {
		t.Fatalf("texts %v, said %d", tt.Texts(), s.run.said)
	}
	f.finish(tt, s)
	if !tt.HasText("Imported 2 files: 2 added, 0 already subscribed, 0 not added.") || !tt.HasText(s.text.ViewSources) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestImportSeveralBusyFile(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.importErr = &client.APIError{Status: 409, Code: "opml_import_busy"}
	s.ImportFiles([]string{writeOPML(t, "a.opml", "<opml/>"), writeOPML(t, "b.opml", "<opml/>")})
	f.finish(tt, s)
	if len(f.sent) != 2 || s.run != nil {
		t.Fatalf("sent %d, run %v", len(f.sent), s.run)
	}
	for _, text := range []string{"a.opml", "b.opml", s.text.BusyWhy} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
}

func TestImportBusy(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.importErr = &client.APIError{Status: 409, Code: "opml_import_busy"}
	s.ImportFiles([]string{writeOPML(t, "a.opml", "<opml/>")})
	f.finish(tt, s)
	if !tt.HasText(s.text.ImportBusy) || tt.HasText(s.text.ViewSources) || s.busy {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestImportSignedOutDuringJob(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.jobPoll = func(int) (client.OPMLImport, error) { return client.OPMLImport{}, client.ErrUnauthorized }
	s.ImportFiles([]string{writeOPML(t, "a.opml", "<opml/>")})
	tt.Frame()
	f.settle(tt)
	f.after(tt, importEvery)
	if s.api != nil || s.run != nil || s.busy || f.expired != 1 {
		t.Fatalf("api %v, run %v, busy %v, expired %d", s.api, s.run, s.busy, f.expired)
	}
}
