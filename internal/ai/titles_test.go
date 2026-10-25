package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// titleSource adds a source with title translation on and n entries titled "T0", "T1", ... in language lang, and
// returns the entry IDs in the order of the titles.
func (f *fixture) titleSource(n int, lang string) []int64 {
	f.t.Helper()
	ctx := context.Background()
	src, err := f.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/titles", Kind: "article", FetchInterval: time.Hour, TranslateTitles: true}, "", f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	items := make([]store.FeedEntry, n)
	for i := range items {
		items[i] = store.FeedEntry{GUID: "g" + strconv.Itoa(i), Title: "T" + strconv.Itoa(i), Language: lang}
	}
	feed := store.FeedSource{ID: src.ID, Kind: "article", TranslateTitles: true}
	if _, err := f.store.SaveFeed(ctx, feed, "", "", items, "zh-Hans", time.Time{}, f.now); err != nil {
		f.t.Fatal(err)
	}
	list, err := f.store.Entries(ctx, store.EntryFilter{SourceID: src.ID, Oldest: true}, n+1)
	if err != nil || len(list) != n {
		f.t.Fatalf("%d entries, %v", len(list), err)
	}
	ids := make([]int64, n)
	for _, e := range list {
		i, _ := strconv.Atoi(strings.TrimPrefix(e.Title, "T"))
		ids[i] = e.ID
	}
	return ids
}

// db opens the database file a second time, to read or change rows the store has no call for.
func (f *fixture) db() *sql.DB {
	f.t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(f.file)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { db.Close() })
	return db
}

func (f *fixture) translated(id int64) (status, text string, attempts int) {
	f.t.Helper()
	row := f.db().QueryRow("SELECT title_tr_status, translated_title, title_tr_attempts FROM entries WHERE id = ?", id)
	if err := row.Scan(&status, &text, &attempts); err != nil {
		f.t.Fatal(err)
	}
	return status, text, attempts
}

// translateAll answers a batch with "zh:<title>" for every title; bad lists the titles it garbles.
func translateAll(bad ...string) func(chatRequest) (int, string) {
	return func(req chatRequest) (int, string) {
		var titles []string
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &titles); err != nil {
			return http.StatusOK, "not json"
		}
		out := make([]string, len(titles))
		for i, title := range titles {
			for _, b := range bad {
				if title == b {
					return http.StatusOK, "not json"
				}
			}
			out[i] = "zh:" + title
		}
		reply, _ := json.Marshal(out)
		return http.StatusOK, string(reply)
	}
}

func TestTitlesGoOutInOneBatch(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = translateAll()
	ids := f.titleSource(3, "en")
	f.drain()
	calls := u.calls()
	if len(calls) != 1 {
		t.Fatalf("%d requests for 3 titles", len(calls))
	}
	if system := calls[0].Messages[0].Content; !strings.Contains(system, "简体中文") || strings.Contains(system, "{language}") {
		t.Fatalf("system prompt: %q", system)
	}
	var sent []string
	if err := json.Unmarshal([]byte(calls[0].Messages[1].Content), &sent); err != nil || len(sent) != 3 {
		t.Fatalf("user message %q: %v", calls[0].Messages[1].Content, err)
	}
	for i, id := range ids {
		if status, text, attempts := f.translated(id); status != "ok" || text != "zh:T"+strconv.Itoa(i) || attempts != 1 {
			t.Fatalf("title %d: %q %q %d", i, status, text, attempts)
		}
	}
	// Translated titles are not requested again.
	f.now = f.now.Add(retryDelay * 2)
	f.drain()
	if len(u.calls()) != 1 {
		t.Fatalf("%d requests after the titles were done", len(u.calls()))
	}
}

func TestTitlesAreBatchedByForty(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = translateAll()
	ids := f.titleSource(45, "en")
	f.drain()
	var sizes []int
	for _, c := range u.calls() {
		var sent []string
		if err := json.Unmarshal([]byte(c.Messages[1].Content), &sent); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(sent))
	}
	if len(sizes) != 2 || sizes[0] != titleBatch || sizes[1] != 5 {
		t.Fatalf("batch sizes %v", sizes)
	}
	for _, id := range ids {
		if status, _, _ := f.translated(id); status != "ok" {
			t.Fatalf("entry %d: %q", id, status)
		}
	}
}

func TestBadBatchIsSplitUntilTheBadTitleIsAlone(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = translateAll("T2")
	ids := f.titleSource(5, "en")
	f.drain()
	for i, id := range ids {
		status, text, attempts := f.translated(id)
		if i == 2 {
			if status != "failed" || text != "" || attempts != 1 {
				t.Fatalf("the bad title: %q %q %d", status, text, attempts)
			}
			continue
		}
		if status != "ok" || text != "zh:T"+strconv.Itoa(i) || attempts != 1 {
			t.Fatalf("title %d: %q %q %d", i, status, text, attempts)
		}
	}
	// 5 titles, then 2 + 3, then the 3 split into 1 + 2, and so on: more than one request, fewer than one per title twice.
	if n := len(u.calls()); n < 4 || n > 9 {
		t.Fatalf("%d requests", n)
	}
	// Later the failed title is tried again, up to the attempts, with the others left alone.
	before := len(u.calls())
	for range maxAttempts + 1 {
		f.now = f.now.Add(retryDelay + 1)
		f.drain()
	}
	if status, _, attempts := f.translated(ids[2]); status != "failed" || attempts != maxAttempts {
		t.Fatalf("after the retries: %q %d", status, attempts)
	}
	if n := len(u.calls()) - before; n != maxAttempts-1 {
		t.Fatalf("%d retries of the bad title, want %d", n, maxAttempts-1)
	}
}

func TestReplyOfTheWrongLengthIsABadBatch(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = func(req chatRequest) (int, string) {
		var titles []string
		json.Unmarshal([]byte(req.Messages[1].Content), &titles)
		if len(titles) > 1 {
			return http.StatusOK, `["only one"]`
		}
		return http.StatusOK, "```json\n[\"zh:" + titles[0] + "\"]\n```"
	}
	ids := f.titleSource(3, "en")
	f.drain()
	for i, id := range ids {
		if status, text, _ := f.translated(id); status != "ok" || text != "zh:T"+strconv.Itoa(i) {
			t.Fatalf("title %d: %q %q", i, status, text)
		}
	}
}

func TestTitlesInTheTargetLanguageAreNotSent(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = translateAll()
	ids := f.titleSource(2, "en")
	// The language became known after the titles were queued.
	if _, err := f.db().Exec("UPDATE entries SET language = 'zh-Hans' WHERE id = ?", ids[0]); err != nil {
		t.Fatal(err)
	}
	f.drain()
	calls := u.calls()
	if len(calls) != 1 {
		t.Fatalf("%d requests", len(calls))
	}
	var sent []string
	json.Unmarshal([]byte(calls[0].Messages[1].Content), &sent)
	if len(sent) != 1 || sent[0] != "T1" {
		t.Fatalf("sent %v", sent)
	}
	if status, text, _ := f.translated(ids[0]); status != "" || text != "" {
		t.Fatalf("the title in the target language: %q %q", status, text)
	}
}

func TestTitlesWaitWhileTheEndpointFailsAndKeepTheirAttempts(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = func(chatRequest) (int, string) { return http.StatusTooManyRequests, "" }
	ids := f.titleSource(3, "en")
	f.drain()
	if n := len(u.calls()); n != 1 {
		t.Fatalf("%d requests: an endpoint failure must not split the batch", n)
	}
	f.drain()
	if n := len(u.calls()); n != 1 {
		t.Fatalf("%d requests while the endpoint waits", n)
	}
	for _, id := range ids {
		if status, _, attempts := f.translated(id); status != "pending" || attempts != 0 {
			t.Fatalf("%q %d", status, attempts)
		}
	}
	u.answer = translateAll()
	f.now = f.now.Add(maxBackoff)
	f.drain()
	for _, id := range ids {
		if status, _, _ := f.translated(id); status != "ok" {
			t.Fatalf("not translated after the outage: %q", status)
		}
	}
}

func TestDailyCapStopsTitles(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	f.set(AutoDailySetting, "1")
	u.answer = translateAll("T3")
	ids := f.titleSource(4, "en")
	// The batch uses the one call and fails; the cap stops the halves.
	f.drain()
	f.drain()
	if n := len(u.calls()); n != 1 {
		t.Fatalf("%d requests with a cap of 1", n)
	}
	for _, id := range ids {
		if status, _, attempts := f.translated(id); status != "failed" || attempts != maxAttempts {
			t.Fatalf("%q %d", status, attempts)
		}
	}
	// Stopped titles are not tried again the same day.
	f.now = f.now.Add(retryDelay * 2)
	f.drain()
	if n := len(u.calls()); n != 1 {
		t.Fatalf("%d requests after the stop", n)
	}
}

func TestNoTitlesAreSentWithoutAI(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, "")
	ids := f.titleSource(2, "en")
	f.drain()
	if n := len(u.calls()); n != 0 {
		t.Fatalf("%d requests", n)
	}
	// The titles wait; they are not failed.
	for _, id := range ids {
		if status, text, _ := f.translated(id); status != "pending" || text != "" {
			t.Fatalf("%q %q", status, text)
		}
	}
}

func TestParseTitles(t *testing.T) {
	for _, tc := range []struct {
		reply string
		n     int
		want  []string
		ok    bool
	}{
		{`["a","b"]`, 2, []string{"a", "b"}, true},
		{" \n[\" a \"]\n", 1, []string{"a"}, true},
		{"```json\n[\"a\"]\n```", 1, []string{"a"}, true},
		{"```\n[\"a\"]\n```", 1, []string{"a"}, true},
		{`["a"]`, 2, nil, false},
		{`["a",""]`, 2, nil, false},
		{`["a",1]`, 2, nil, false},
		{`{"a":1}`, 1, nil, false},
		{"Sure! Here you go", 1, nil, false},
	} {
		got, err := parseTitles(tc.reply, tc.n)
		if (err == nil) != tc.ok || (tc.ok && strings.Join(got, "|") != strings.Join(tc.want, "|")) {
			t.Errorf("parseTitles(%q, %d) = %v, %v", tc.reply, tc.n, got, err)
		}
	}
}

func TestLongTitlesAreCutBeforeSending(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = translateAll()
	ids := f.titleSource(1, "en")
	long := strings.Repeat("长", maxTitleRunes+50)
	if _, err := f.db().Exec("UPDATE entries SET title = ? WHERE id = ?", long, ids[0]); err != nil {
		t.Fatal(err)
	}
	f.drain()
	var sent []string
	json.Unmarshal([]byte(u.calls()[0].Messages[1].Content), &sent)
	if len(sent) != 1 || len([]rune(sent[0])) > maxTitleRunes {
		t.Fatalf("sent %d characters", len([]rune(sent[0])))
	}
}
