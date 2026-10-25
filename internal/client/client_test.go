package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEntriesSendsZoneOnlyForSearch(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		w.Write([]byte(`{"entries":[],"nextCursor":null}`))
	}))
	defer srv.Close()
	local := zoneName
	defer func() { zoneName = local }()
	session := Session{Server: srv.URL, Token: "t1"}
	for _, c := range []struct {
		zone   *time.Location
		search string
		want   string
	}{
		{time.FixedZone("Europe/Paris", 3600), "after:2026-01-01", "q=after%3A2026-01-01&tz=Europe%2FParis"},
		{time.FixedZone("Europe/Paris", 3600), "", ""},
		{time.FixedZone("Local", 0), "go", "q=go"},
		{time.FixedZone("", 0), "go", "q=go"},
	} {
		zoneName = c.zone.String
		if _, err := session.Entries(context.Background(), Query{Search: c.search}); err != nil {
			t.Fatal(err)
		}
		if last := got[len(got)-1]; last != c.want {
			t.Errorf("zone %q, search %q: %q, want %q", c.zone, c.search, last, c.want)
		}
	}
}

func TestAddress(t *testing.T) {
	for input, want := range map[string]string{
		"home.lan":                   "http://home.lan:7470",
		" 192.168.1.20 ":             "http://192.168.1.20:7470",
		"home.lan:8080":              "http://home.lan:8080",
		"https://oriel.example.com/": "https://oriel.example.com",
		"http://home.lan/oriel/?x=1": "http://home.lan/oriel",
		"[::1]":                      "http://[::1]:7470",
	} {
		got, err := Address(input)
		if err != nil || got != want {
			t.Errorf("Address(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "   ", "ftp://home.lan", "http://"} {
		if _, err := Address(input); !errors.Is(err, ErrUnreachable) {
			t.Errorf("Address(%q) = %v; want ErrUnreachable", input, err)
		}
	}
}

// fake answers like `oriel serve` with the password "secret", which hands out the token "t1".
func fake(t *testing.T, health string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(health))
	})
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		if string(buf[:n]) != `{"password":"secret"}` {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"wrong_password"}`))
			return
		}
		w.Write([]byte(`{"token":"t1"}`))
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Write([]byte(`{}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestProbe(t *testing.T) {
	ctx := context.Background()
	s := fake(t, `{"app":"oriel","version":"0.8.0","auth":true,"database":"ok"}`)
	server, err := Probe(ctx, s.URL+"/")
	if err != nil || server.URL != s.URL || !server.Auth {
		t.Fatalf("Probe = %+v, %v", server, err)
	}
	if _, err := Probe(ctx, fake(t, `<html>router</html>`).URL); !errors.Is(err, ErrNotOriel) {
		t.Errorf("not JSON: %v", err)
	}
	if _, err := Probe(ctx, fake(t, `{"app":"other"}`).URL); !errors.Is(err, ErrNotOriel) {
		t.Errorf("other app: %v", err)
	}
	if _, err := Probe(ctx, fake(t, `{"app":"oriel","database":"unwritable"}`).URL); !errors.Is(err, ErrUnwritable) {
		t.Errorf("unwritable: %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if _, err := Probe(ctx, closed.URL); !errors.Is(err, ErrUnreachable) {
		t.Errorf("closed: %v", err)
	}
}

func TestLoginAndCheck(t *testing.T) {
	ctx := context.Background()
	s := fake(t, `{"app":"oriel","auth":true,"database":"ok"}`)
	server := Server{URL: s.URL, Auth: true}
	if _, err := Login(ctx, server, ""); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("empty password: %v", err)
	}
	if _, err := Login(ctx, server, "nope"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}
	session, err := Login(ctx, server, "secret")
	if err != nil || session != (Session{Server: s.URL, Token: "t1"}) {
		t.Fatalf("Login = %+v, %v", session, err)
	}
	if err := Check(ctx, session); err != nil {
		t.Errorf("Check: %v", err)
	}
	if err := Check(ctx, Session{Server: s.URL, Token: "old"}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expired: %v", err)
	}

	open, err := Login(ctx, Server{URL: s.URL}, "ignored")
	if err != nil || open != (Session{Server: s.URL}) {
		t.Errorf("without password = %+v, %v", open, err)
	}
}

func TestSessionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote.json")
	if _, ok, err := LoadSession(path); ok || err != nil {
		t.Fatalf("missing file: ok %v, %v", ok, err)
	}
	want := Session{Server: "http://home.lan:7470", Token: "t1"}
	if err := SaveSession(path, want); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, %v", info.Mode().Perm(), err)
		}
	}
	got, ok, err := LoadSession(path)
	if !ok || err != nil || got != want {
		t.Errorf("LoadSession = %+v, %v, %v", got, ok, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left %d files behind", len(entries))
	}
	if err := ForgetSession(path); err != nil {
		t.Fatal(err)
	}
	if err := ForgetSession(path); err != nil {
		t.Errorf("forget twice: %v", err)
	}
	os.WriteFile(path, []byte("{"), 0o600)
	if _, ok, err := LoadSession(path); ok || err == nil {
		t.Errorf("broken file: ok %v, %v", ok, err)
	}
}

func TestTimelineAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		switch r.URL.Path {
		case "/api/entries":
			if r.URL.Query().Get("cursor") == "bad" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_cursor"}`))
				return
			}
			w.Write([]byte(`{"entries":[{"id":7,"kind":"video","title":"T","publishedAt":null,"durationSec":95,"readingSec":600,
				"images":[{"url":"https://i/1.jpg","width":4,"height":3}],"createdAt":1700000000000,"group":{"by":"link","entries":[{"id":8,"sourceId":2,"sourceTitle":"Other","title":"T2","url":"https://x/t","isRead":true,"starred":false,"createdAt":1700000001000}]}}],"nextCursor":null}`))
		case "/api/sources":
			w.Write([]byte(`{"sources":[{"id":1,"title":"Feed","kind":"article","unreadCount":3,"lastError":"boom","lastErrorKind":"blocked","fetchIntervalSec":900,"fetchIntervalAuto":true,"suggestedKind":"video"}],"unreadTotal":2}`))
		case "/api/sources/health":
			w.Write([]byte(`{"windowDays":30,"sources":[{"id":2,"title":"Old","state":"dead","cause":"silent","days":143,"recent":0},
				{"id":3,"title":"Busy","state":"rarelyRead","recent":120,"recentRead":3,"recentStarred":1}]}`))
		case "/api/entries/7":
			w.Write([]byte(`{"id":7,"starred":true,"readProgress":0.5,"extractStatus":"ok","contentHtml":"<p>x</p>","extractedHtml":"<p>y</p>"}`))
		case "/api/entries/7/extract":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":7,"extractStatus":"pending","contentHtml":"<p>x</p>"}`))
		case "/api/entries/read":
			w.Write([]byte(`{"marked":2,"ids":[7,8],"readAt":1700000000500}`))
		case "/api/entries/unread":
			w.Write([]byte(`{"marked":2,"ids":[7,8]}`))
		case "/api/image":
			w.Write([]byte("PNG"))
		case "/api/discover":
			if r.URL.Query().Get("url") == "https://down.example/" {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(`{"error":"page_unavailable"}`))
				return
			}
			w.Write([]byte(`{"candidates":[{"url":"https://blog.example/rss.xml","rsshubPath":"","title":"Blog","kind":"","origin":"alternate"},
				{"url":"","rsshubPath":"/bilibili/user/video/1","title":"","kind":"video","origin":"rule","site":"bilibili","label":"videos","subject":"1"}]}`))
		case "/api/routes":
			w.Write([]byte(`{"available":true,"instance":"http://rsshub.local","total":12,"routes":[{"path":"/github/issue/:user/:repo",
				"name":"Issues","namespace":"github","namespaceName":"GitHub","example":"","unknown":1,
				"parameters":[{"name":"user","description":"User","optional":false},{"name":"repo","description":"","optional":true}],
				"values":{"user":"rsshub"}}]}`))
		case "/api/routes/preview":
			if r.URL.Query().Get("path") == "/gone" {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(`{"error":"route_not_found"}`))
				return
			}
			w.Write([]byte(`{"instance":"http://rsshub.local","title":"Issues","entries":[{"title":"One","url":"https://e/1","publishedAt":1700000000000},
				{"title":"Two","url":"https://e/2","publishedAt":null}]}`))
		case "/api/sources/5/restore":
			w.Write([]byte(`{"id":5,"title":"Back","kind":"video","lastFetchedAt":null}`))
		case "/api/sources/5", "/api/sources/5/refresh":
			if r.Method == http.MethodPatch {
				w.Write([]byte(`{"id":5,"title":"New","kind":"video","paused":true,"keepDays":90,"lastFetchedAt":null}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/sources/5/muted-count":
			w.Write([]byte(`{"count":4}`))
		case "/api/sources/refresh":
			w.Write([]byte(`{"queued":3,"at":1700000000000}`))
		case "/api/sources/6/refresh":
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"source_paused"}`))
		case "/api/settings":
			w.Write([]byte(`{"rsshubBaseUrl":"https://rsshub.app","retentionDays":30,"groupSameLink":true}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "t1"}

	local := zoneName
	zoneName = func() string { return "Asia/Shanghai" }
	defer func() { zoneName = local }()
	page, err := session.Entries(ctx, Query{Kind: "video", Unread: true, Search: "go fmt", MaxMinutes: 10, Oldest: true, Limit: 50, Cursor: "1.2"})
	if err != nil || len(page.Entries) != 1 || page.Next != "" {
		t.Fatalf("Entries = %+v, %v", page, err)
	}
	if e := page.Entries[0]; e.DurationSec != 95 || e.ReadingSec != 600 || e.PublishedAt != 0 || e.Images[0].Width != 4 || e.CreatedAt != 1700000000000 {
		t.Errorf("entry = %+v", e)
	} else if g := e.Group; g == nil || g.By != "link" || len(g.Entries) != 1 || g.Entries[0].ID != 8 || g.Entries[0].SourceTitle != "Other" || !g.Entries[0].IsRead {
		t.Errorf("group = %+v", g)
	}
	var apiErr *APIError
	if _, err := session.Entries(ctx, Query{Cursor: "bad"}); !errors.As(err, &apiErr) || apiErr.Code != "invalid_cursor" {
		t.Errorf("bad cursor: %v", err)
	}
	if article, err := session.RetryExtraction(ctx, 7); err != nil || article.ID != 7 || article.ExtractStatus != "pending" {
		t.Errorf("RetryExtraction = %+v, %v", article, err)
	}
	list, err := session.Sources(ctx)
	sources := list.Sources
	if err != nil || list.UnreadTotal != 2 || len(sources) != 1 || sources[0].UnreadCount != 3 || sources[0].LastError != "boom" || sources[0].LastErrorKind != "blocked" ||
		sources[0].FetchIntervalSec != 900 || !sources[0].FetchIntervalAuto || sources[0].SuggestedKind != "video" {
		t.Errorf("Sources = %+v, %v", sources, err)
	}
	if health, err := session.SourcesHealth(ctx); err != nil || health.WindowDays != 30 || len(health.Sources) != 2 ||
		health.Sources[0].State != HealthDead || health.Sources[0].Cause != HealthCauseSilent || health.Sources[0].Days != 143 ||
		health.Sources[1].State != HealthRarelyRead || health.Sources[1].Recent != 120 || health.Sources[1].RecentRead != 3 {
		t.Errorf("SourcesHealth = %+v, %v", health, err)
	}
	if article, err := session.Entry(ctx, 7); err != nil || article.ID != 7 || article.ExtractStatus != "ok" ||
		article.ContentHTML != "<p>x</p>" || article.ExtractedHTML != "<p>y</p>" {
		t.Errorf("Entry = %+v, %v", article, err)
	}
	starred := true
	if entry, err := session.SetEntry(ctx, 7, EntryPatch{Starred: &starred}); err != nil || !entry.Starred {
		t.Errorf("SetEntry = %+v, %v", entry, err)
	}
	progress := 0.5
	if entry, err := session.SetEntry(ctx, 7, EntryPatch{ReadProgress: &progress}); err != nil || entry.ReadProgress == nil || *entry.ReadProgress != 0.5 {
		t.Errorf("SetEntry progress = %+v, %v", entry, err)
	}
	read, err := session.MarkRead(ctx, ReadScope{Kind: "video", UpTo: 7})
	if err != nil || len(read.IDs) != 2 || read.IDs[0] != 7 || read.IDs[1] != 8 || read.ReadAt != 1700000000500 {
		t.Errorf("MarkRead = %v, %v", read, err)
	}
	if read, err := session.MarkRead(ctx, ReadScope{SourceID: 5, OlderThan: 1700000000000}); err != nil || len(read.IDs) != 2 {
		t.Errorf("MarkRead older = %v, %v", read, err)
	}
	if marked, err := session.MarkUnread(ctx, read); err != nil || len(marked) != 2 {
		t.Errorf("MarkUnread = %v, %v", marked, err)
	}
	if body, err := session.Image(ctx, "https://i/1.jpg?w=2&h=1"); err != nil || string(body) != "PNG" {
		t.Errorf("Image = %q, %v", body, err)
	}
	found, err := session.Discover(ctx, "https://blog.example/")
	if err != nil || len(found) != 2 || found[0] != (Candidate{URL: "https://blog.example/rss.xml", Title: "Blog", Origin: "alternate"}) ||
		found[1] != (Candidate{RSSHubPath: "/bilibili/user/video/1", Kind: "video", Origin: "rule", Site: "bilibili", Label: "videos", Subject: "1"}) {
		t.Errorf("Discover = %+v, %v", found, err)
	}
	if _, err := session.Discover(ctx, "https://down.example/"); !errors.As(err, &apiErr) || apiErr.Code != "page_unavailable" {
		t.Errorf("Discover down: %v", err)
	}
	routes, err := session.FindRoutes(ctx, "github issues", true)
	if err != nil || !routes.Available || routes.Instance != "http://rsshub.local" || routes.Total != 12 || len(routes.Routes) != 1 ||
		routes.Routes[0].Path != "/github/issue/:user/:repo" || routes.Routes[0].NamespaceName != "GitHub" ||
		len(routes.Routes[0].Parameters) != 2 || !routes.Routes[0].Parameters[1].Optional ||
		routes.Routes[0].Values["user"] != "rsshub" {
		t.Errorf("FindRoutes = %+v, %v", routes, err)
	}
	preview, err := session.PreviewRoute(ctx, "/github/issue/rsshub/rsshub")
	if err != nil || preview.Title != "Issues" || len(preview.Entries) != 2 || preview.Entries[0].PublishedAt == nil ||
		*preview.Entries[0].PublishedAt != 1700000000000 || preview.Entries[1].PublishedAt != nil {
		t.Errorf("PreviewRoute = %+v, %v", preview, err)
	}
	if _, err := session.PreviewRoute(ctx, "/gone"); !errors.As(err, &apiErr) || apiErr.Code != "route_not_found" {
		t.Errorf("PreviewRoute gone: %v", err)
	}
	if _, err := session.AddSource(ctx, NewSource{RSSHubPath: "/x/y", Kind: "social"}); err != nil {
		t.Errorf("AddSource: %v", err)
	}
	title, paused, keep := "New", true, 90
	if src, err := session.UpdateSource(ctx, 5, SourcePatch{Title: &title, Paused: &paused, KeepDays: &keep}); err != nil ||
		src.Title != "New" || !src.Paused || src.KeepDays != 90 || src.LastFetchedAt != 0 {
		t.Errorf("UpdateSource = %+v, %v", src, err)
	}
	if n, err := session.MutedCount(ctx, 5, "ad\nspam"); err != nil || n != 4 {
		t.Errorf("MutedCount = %d, %v", n, err)
	}
	if err := session.RefreshSource(ctx, 5); err != nil {
		t.Errorf("RefreshSource: %v", err)
	}
	if err := session.RefreshSource(ctx, 6); !errors.As(err, &apiErr) || apiErr.Code != "source_paused" {
		t.Errorf("RefreshSource paused: %v", err)
	}
	if res, err := session.RefreshAll(ctx); err != nil || res != (RefreshResult{Queued: 3, At: 1700000000000}) {
		t.Errorf("RefreshAll = %+v, %v", res, err)
	}
	if err := session.DeleteSource(ctx, 5); err != nil {
		t.Errorf("DeleteSource: %v", err)
	}
	if src, err := session.RestoreSource(ctx, 5); err != nil || src.ID != 5 || src.Title != "Back" {
		t.Errorf("RestoreSource = %+v, %v", src, err)
	}
	if settings, err := session.Settings(ctx); err != nil || settings.RetentionDays != 30 || !settings.GroupSameLink {
		t.Errorf("Settings = %+v, %v", settings, err)
	}
	root := "https://rsshub.app"
	if settings, err := session.SetSettings(ctx, SettingsPatch{RSSHubBaseURL: &root}); err != nil ||
		settings.RSSHubBaseURL != root {
		t.Errorf("SetSettings = %+v, %v", settings, err)
	}
	want := []string{
		"GET /api/entries?cursor=1.2&kind=video&limit=50&maxMinutes=10&order=asc&q=go+fmt&tz=Asia%2FShanghai&unread=1 ",
		"GET /api/entries?cursor=bad ",
		"POST /api/entries/7/extract ",
		"GET /api/sources ",
		"GET /api/sources/health ",
		"GET /api/entries/7 ",
		`PATCH /api/entries/7 {"starred":true}`,
		`PATCH /api/entries/7 {"readProgress":0.5}`,
		`POST /api/entries/read {"kind":"video","upTo":7}`,
		`POST /api/entries/read {"sourceId":5,"olderThan":1700000000000}`,
		`POST /api/entries/unread {"ids":[7,8],"readAt":1700000000500}`,
		"GET /api/image?url=https%3A%2F%2Fi%2F1.jpg%3Fw%3D2%26h%3D1 ",
		"GET /api/discover?url=https%3A%2F%2Fblog.example%2F ",
		"GET /api/discover?url=https%3A%2F%2Fdown.example%2F ",
		"GET /api/routes?q=github+issues&refresh=1 ",
		"GET /api/routes/preview?path=%2Fgithub%2Fissue%2Frsshub%2Frsshub ",
		"GET /api/routes/preview?path=%2Fgone ",
		`POST /api/sources {"rsshubPath":"/x/y","kind":"social"}`,
		`PATCH /api/sources/5 {"title":"New","paused":true,"keepDays":90}`,
		`POST /api/sources/5/muted-count {"muteKeywords":"ad\nspam"}`,
		"POST /api/sources/5/refresh ",
		"POST /api/sources/6/refresh ",
		"POST /api/sources/refresh ",
		"DELETE /api/sources/5 ",
		"POST /api/sources/5/restore ",
		"GET /api/settings ",
		`PATCH /api/settings {"rsshubBaseUrl":"https://rsshub.app"}`,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
	if _, err := (Session{Server: srv.URL, Token: "old"}).Sources(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expired session: %v", err)
	}
	if _, err := (Session{Server: "http://127.0.0.1:1"}).Sources(ctx); !errors.Is(err, ErrUnreachable) {
		t.Errorf("no server: %v", err)
	}
}

func TestActivityAndStorageAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		switch r.Method + " " + r.URL.Path {
		case "GET /api/activity":
			w.Write([]byte(`{"keep":10,"fetching":[{"id":2,"url":"https://b/feed","rsshubPath":"","title":"B"}],
				"sources":[{"id":1,"url":"https://a/feed","rsshubPath":"","title":"A","paused":true,
				"attempts":[{"at":1700000000000,"durationMs":1500,"ok":false,"status":503,"newEntries":0,"error":"status 503"},
				{"at":1699999000000,"durationMs":90,"ok":true,"status":200,"newEntries":4,"error":""}]}]}`))
		case "GET /api/storage":
			w.Write([]byte(`{"databaseBytes":1000,"reclaimableBytes":200,"entries":30,"images":4,"imageBytes":5000,"copies":2,"copyBytes":700,
				"premigrate":{"bytes":4096,"version":17},
				"vacuum":{"state":"done","startedAt":1,"finishedAt":2,"freedBytes":150,"error":""}}`))
		case "POST /api/storage/vacuum":
			if len(seen) > 3 {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":"vacuum_running"}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"vacuum":{"state":"running","startedAt":7}}`))
		case "DELETE /api/storage/premigrate":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "tok"}

	activity, err := session.Activity(ctx)
	if err != nil || activity.Keep != 10 || len(activity.Fetching) != 1 || activity.Fetching[0].Title != "B" || len(activity.Sources) != 1 {
		t.Fatalf("Activity = %+v, %v", activity, err)
	}
	src := activity.Sources[0]
	if !src.Paused || len(src.Attempts) != 2 || src.Attempts[0].OK || src.Attempts[0].Status != 503 ||
		src.Attempts[0].Error != "status 503" || src.Attempts[1].NewEntries != 4 || src.Attempts[1].DurationMs != 90 {
		t.Errorf("source = %+v", src)
	}
	storage, err := session.Storage(ctx)
	if err != nil || storage.DatabaseBytes != 1000 || storage.ReclaimableBytes != 200 || storage.Entries != 30 ||
		storage.Images != 4 || storage.ImageBytes != 5000 || storage.Copies != 2 || storage.CopyBytes != 700 || storage.Vacuum.State != VacuumDone || storage.Vacuum.FreedBytes != 150 {
		t.Errorf("Storage = %+v, %v", storage, err)
	}
	started, err := session.StartVacuum(ctx)
	if err != nil || started.State != VacuumRunning || started.StartedAt != 7 {
		t.Errorf("StartVacuum = %+v, %v", started, err)
	}
	var apiErr *APIError
	if _, err := session.StartVacuum(ctx); !errors.As(err, &apiErr) || apiErr.Code != "vacuum_running" || apiErr.Status != http.StatusConflict {
		t.Errorf("second StartVacuum: %v", err)
	}
	if storage.PreMigrate == nil || *storage.PreMigrate != (PreMigrate{Bytes: 4096, Version: 17}) {
		t.Errorf("PreMigrate = %+v", storage.PreMigrate)
	}
	if err := session.DeletePreMigrate(ctx); err != nil {
		t.Errorf("DeletePreMigrate: %v", err)
	}
	want := []string{"GET /api/activity Bearer tok", "GET /api/storage Bearer tok", "POST /api/storage/vacuum Bearer tok", "POST /api/storage/vacuum Bearer tok",
		"DELETE /api/storage/premigrate Bearer tok"}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestSettingsAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type")+" "+r.UserAgent()+" "+string(body))
		switch r.URL.Path {
		case "/api/sessions":
			w.Write([]byte(`{"sessions":[{"id":2,"userAgent":"Oriel/1.0 (macOS)","createdAt":1,"lastSeenAt":2,"current":true}]}`))
		case "/api/sessions/3":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not_found"}`))
		case "/api/opml/import/start":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":"a1","phase":"running","progress":{"done":0,"total":3}}`))
		case "/api/opml/import":
			w.Write([]byte(`{"id":"a1","phase":"done","progress":{"done":3,"total":3},"result":{"added":2,"existing":1,"failed":[{"title":"G","address":"ftp://x","error":"invalid_url"}]}}`))
		case "/api/opml/export":
			w.Header().Set("Content-Type", "text/x-opml")
			w.Write([]byte("<opml/>"))
		case "/api/diagnostics":
			w.Write([]byte("Oriel diagnostics\n"))
		}
	}))
	defer srv.Close()
	SetVersion("1.0")
	session := Session{Server: srv.URL}

	devices, err := session.Devices(ctx)
	if err != nil || len(devices) != 1 || devices[0] != (Device{ID: 2, UserAgent: "Oriel/1.0 (macOS)", CreatedAt: 1,
		LastSeenAt: 2, Current: true}) {
		t.Errorf("Devices = %+v, %v", devices, err)
	}
	var apiErr *APIError
	if err := session.RevokeDevice(ctx, 3); !errors.As(err, &apiErr) || apiErr.Code != "not_found" {
		t.Errorf("RevokeDevice: %v", err)
	}
	job, err := session.StartOPMLImport(ctx, []byte("<opml/>"))
	if err != nil || job.ID != "a1" || job.Phase != OPMLRunning || job.Progress.Total != 3 || job.Result != nil {
		t.Errorf("StartOPMLImport = %+v, %v", job, err)
	}
	job, err = session.OPMLImportStatus(ctx)
	if err != nil || job.Phase != OPMLDone || job.Progress.Done != 3 || job.Result == nil || job.Result.Added != 2 ||
		job.Result.Existing != 1 || job.Result.Failed[0] != (ImportFail{Title: "G", Address: "ftp://x", Error: "invalid_url"}) {
		t.Errorf("OPMLImportStatus = %+v, %v", job, err)
	}
	if opml, err := session.ExportOPML(ctx); err != nil || string(opml) != "<opml/>" {
		t.Errorf("ExportOPML = %q, %v", opml, err)
	}
	if text, err := session.Diagnostics(ctx); err != nil || string(text) != "Oriel diagnostics\n" {
		t.Errorf("Diagnostics = %q, %v", text, err)
	}
	agent := "Oriel/1.0 (" + platform() + ")"
	want := []string{
		"GET /api/sessions  " + agent + " ",
		"DELETE /api/sessions/3  " + agent + " ",
		"POST /api/opml/import/start text/x-opml " + agent + " <opml/>",
		"GET /api/opml/import  " + agent + " ",
		"GET /api/opml/export  " + agent + " ",
		"GET /api/diagnostics  " + agent + " ",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestBackupAPI(t *testing.T) {
	ctx := context.Background()
	var uploaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/backup":
			w.Header().Set("Content-Type", "application/zip")
			w.Write([]byte("zip bytes"))
		case "POST /api/restore":
			body, _ := io.ReadAll(r.Body)
			if uploaded = string(body); uploaded == "bad" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write([]byte(`{"error":"backup_newer"}`))
				return
			}
			w.Write([]byte(`{"sources":3,"entries":40}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL}

	var saved strings.Builder
	if n, err := session.Backup(ctx, &saved); err != nil || n != 9 || saved.String() != "zip bytes" {
		t.Errorf("Backup = %d %q, %v", n, saved.String(), err)
	}
	restored, err := session.Restore(ctx, strings.NewReader("zip bytes"))
	if err != nil || restored != (Restored{Sources: 3, Entries: 40}) || uploaded != "zip bytes" {
		t.Errorf("Restore = %+v, %v (uploaded %q)", restored, err, uploaded)
	}
	var apiErr *APIError
	if _, err := session.Restore(ctx, strings.NewReader("bad")); !errors.As(err, &apiErr) || apiErr.Code != "backup_newer" {
		t.Errorf("Restore of a newer backup: %v", err)
	}
}

func TestAIAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		switch r.URL.Path {
		case "/api/entries/7/ai":
			w.Write([]byte(`{"summary":{"status":"ok","text":"Short.","error":"","model":"m","updatedAt":1},"translation":null}`))
		case "/api/entries/7/translation":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"pending","text":"","error":"","model":"","updatedAt":null}`))
		case "/api/entries/8/summary":
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"ai_not_configured"}`))
		case "/api/ai/test":
			w.Write([]byte(`{"reachable":true,"model":"m-1","modelFound":true,"contextTokens":4096,"contextFits":false}`))
		case "/api/ai/selection":
			if strings.Contains(string(body), "too long") {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(`{"error":"ai_timeout"}`))
				return
			}
			w.Write([]byte(`{"text":"你好","model":"m-1"}`))
		case "/api/settings":
			w.Write([]byte(`{"aiBaseUrl":"http://x/v1","aiModel":"m","aiApiKeySet":true,"aiLanguage":"en"}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL}

	results, err := session.AI(ctx, 7)
	if err != nil || results.Summary == nil || *results.Summary != (AIResult{Status: "ok", Text: "Short.", Model: "m"}) ||
		results.Translation != nil {
		t.Errorf("AI = %+v, %v", results, err)
	}
	if result, err := session.RequestAI(ctx, 7, "translation"); err != nil || result.Status != "pending" {
		t.Errorf("RequestAI = %+v, %v", result, err)
	}
	var apiErr *APIError
	if _, err := session.RequestAI(ctx, 8, "summary"); !errors.As(err, &apiErr) || apiErr.Code != "ai_not_configured" {
		t.Errorf("RequestAI not configured: %v", err)
	}
	if got, err := session.TestAI(ctx); err != nil || got.Model != "m-1" || got.ModelFound == nil || !*got.ModelFound ||
		got.ContextTokens == nil || *got.ContextTokens != 4096 || got.ContextFits == nil || *got.ContextFits {
		t.Errorf("TestAI = %+v, %v", got, err)
	}
	if text, model, err := session.Selection(ctx, "translate", "hello", "Greetings"); err != nil || text != "你好" || model != "m-1" {
		t.Errorf("Selection = %q, %q, %v", text, model, err)
	}
	if _, _, err := session.Selection(ctx, "explain", "too long", ""); !errors.As(err, &apiErr) || apiErr.Code != "ai_timeout" {
		t.Errorf("Selection upstream failure: %v", err)
	}
	key := ""
	settings, err := session.SetSettings(ctx, SettingsPatch{AIAPIKey: &key})
	if err != nil || !settings.AIConfigured() || !settings.AIAPIKeySet || settings.AILanguage != "en" {
		t.Errorf("SetSettings = %+v, %v", settings, err)
	}
	on := true
	session.UpdateSource(ctx, 5, SourcePatch{AutoSummary: &on, PromptSummary: &key})
	want := []string{
		"GET /api/entries/7/ai ",
		"POST /api/entries/7/translation ",
		"POST /api/entries/8/summary ",
		"POST /api/ai/test ",
		`POST /api/ai/selection {"task":"translate","text":"hello","title":"Greetings"}`,
		`POST /api/ai/selection {"task":"explain","text":"too long","title":""}`,
		`PATCH /api/settings {"aiApiKey":""}`,
		`PATCH /api/sources/5 {"promptSummary":"","autoSummary":true}`,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestRulesAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		switch r.Method + " " + r.URL.Path {
		case "GET /api/rules":
			w.Write([]byte(`{"rules":[{"id":3,"sourceId":null,"kind":"video","keywords":"ad","author":"","star":true,"markRead":false,"enabled":true,"createdAt":5}]}`))
		case "POST /api/rules":
			if strings.Contains(string(body), `"keywords":"full"`) {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":"too_many_rules"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":4,"sourceId":7,"kind":"","keywords":"","author":"Ann","star":false,"markRead":true,"enabled":true,"createdAt":6}`))
		case "PATCH /api/rules/4":
			w.Write([]byte(`{"id":4,"sourceId":null,"kind":"","keywords":"","author":"Bob","star":true,"markRead":false,"enabled":false,"createdAt":6}`))
		case "POST /api/muted-count":
			w.Write([]byte(`{"count":9}`))
		case "DELETE /api/rules/4":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "t"}

	rules, err := session.Rules(ctx)
	if err != nil || len(rules) != 1 || rules[0].ID != 3 || rules[0].SourceID != nil || rules[0].Kind != "video" || !rules[0].Star {
		t.Errorf("Rules = %+v, %v", rules, err)
	}
	src := int64(7)
	created, err := session.CreateRule(ctx, NewRule{SourceID: &src, Author: "Ann", MarkRead: true, Enabled: true})
	if err != nil || created.ID != 4 || created.SourceID == nil || *created.SourceID != 7 {
		t.Errorf("CreateRule = %+v, %v", created, err)
	}
	var apiErr *APIError
	if _, err := session.CreateRule(ctx, NewRule{Keywords: "full", Star: true}); !errors.As(err, &apiErr) || apiErr.Code != "too_many_rules" {
		t.Errorf("CreateRule full: %v", err)
	}
	if back, err := session.RestoreRule(ctx, Rule{ID: 4, SourceID: &src, Author: "Ann", MarkRead: true, Enabled: true, CreatedAt: 6}); err != nil || back.ID != 4 {
		t.Errorf("RestoreRule = %+v, %v", back, err)
	}
	if changed, err := session.UpdateRule(ctx, 4, NewRule{Author: "Bob", Star: true, Enabled: true}); err != nil || changed.Author != "Bob" {
		t.Errorf("UpdateRule = %+v, %v", changed, err)
	}
	if changed, err := session.SetRuleEnabled(ctx, 4, false); err != nil || changed.Enabled {
		t.Errorf("SetRuleEnabled = %+v, %v", changed, err)
	}
	if n, err := session.MutedCountAll(ctx, "ad"); err != nil || n != 9 {
		t.Errorf("MutedCountAll = %d, %v", n, err)
	}
	if err := session.DeleteRule(ctx, 4); err != nil {
		t.Errorf("DeleteRule: %v", err)
	}
	want := []string{
		"GET /api/rules ",
		`POST /api/rules {"sourceId":7,"kind":"","keywords":"","author":"Ann","star":false,"markRead":true,"enabled":true}`,
		`POST /api/rules {"sourceId":null,"kind":"","keywords":"full","author":"","star":true,"markRead":false,"enabled":false}`,
		`POST /api/rules {"id":4,"createdAt":6,"sourceId":7,"kind":"","keywords":"","author":"Ann","star":false,"markRead":true,"enabled":true}`,
		`PATCH /api/rules/4 {"sourceId":null,"kind":"","keywords":"","author":"Bob","star":true,"markRead":false,"enabled":true}`,
		`PATCH /api/rules/4 {"enabled":false}`,
		`POST /api/muted-count {"muteKeywords":"ad"}`,
		"DELETE /api/rules/4 ",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestRSSHubHealth(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization")
		w.Write([]byte(`{"keep":50,"days":7,"instances":[
			{"url":"https://a","requests":50,"successRate":0.96,"medianMs":420,"coolingUntil":null},
			{"url":"https://b","requests":0,"successRate":null,"medianMs":null,"coolingUntil":1700000000000}]}`))
	}))
	defer srv.Close()
	got, err := (Session{Server: srv.URL, Token: "tok"}).RSSHubHealth(context.Background())
	if err != nil || seen != "GET /api/rsshub/health Bearer tok" || got.Keep != 50 || got.Days != 7 || len(got.Instances) != 2 {
		t.Fatalf("RSSHubHealth = %+v, %v (%s)", got, err, seen)
	}
	a, b := got.Instances[0], got.Instances[1]
	if a.URL != "https://a" || a.Requests != 50 || a.SuccessRate == nil || *a.SuccessRate != 0.96 || *a.MedianMs != 420 || a.CoolingUntil != nil {
		t.Errorf("a = %+v", a)
	}
	if b.SuccessRate != nil || b.MedianMs != nil || b.CoolingUntil == nil || *b.CoolingUntil != 1700000000000 {
		t.Errorf("b = %+v", b)
	}
}

func TestMoveSuggestionAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case strings.Contains(string(body), "taken"):
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"source_exists","sourceId":9}`))
		case strings.Contains(string(body), "stale"):
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"move_changed"}`))
		case r.URL.Path == "/api/sources/4/move":
			w.Write([]byte(`{"id":4,"url":"https://b.example/feed.atom","kind":"article"}`))
		default:
			w.Write([]byte(`{"id":4,"url":"http://b.example/feed","kind":"article"}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "t1"}

	if src, err := session.MoveSource(ctx, 4, "https://b.example/feed.atom"); err != nil || src.URL != "https://b.example/feed.atom" || src.MovedTo != "" {
		t.Errorf("MoveSource = %+v, %v", src, err)
	}
	if src, err := session.DismissMove(ctx, 4, "https://b.example/feed.atom"); err != nil || src.MovedTo != "" {
		t.Errorf("DismissMove = %+v, %v", src, err)
	}
	var apiErr *APIError
	if _, err := session.MoveSource(ctx, 4, "taken"); !errors.As(err, &apiErr) || apiErr.Code != "source_exists" || apiErr.SourceID != 9 {
		t.Errorf("MoveSource taken: %v", err)
	}
	if _, err := session.DismissMove(ctx, 4, "stale"); !errors.As(err, &apiErr) || apiErr.Code != "move_changed" {
		t.Errorf("DismissMove stale: %v", err)
	}
	want := []string{
		`POST /api/sources/4/move {"url":"https://b.example/feed.atom"}`,
		`POST /api/sources/4/dismiss-move {"url":"https://b.example/feed.atom"}`,
		`POST /api/sources/4/move {"url":"taken"}`,
		`POST /api/sources/4/dismiss-move {"url":"stale"}`,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests %q", seen)
	}
}
