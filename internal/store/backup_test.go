package store

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// seeded opens a store with two subscriptions, three entries, one read and one starred, an AI result, a per-source
// setting, a session and an AI API key.
func seeded(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s := open(t, file)
	a := newLogSource(t, s, "https://a.example/feed")
	b := newLogSource(t, s, "https://b.example/feed")
	now := time.Now()
	if _, err := s.SaveFeed(ctx, FeedSource{ID: a.ID, Kind: "article"}, "A", "", []FeedEntry{
		{GUID: "1", Title: "Quantum gardens", URL: "https://a.example/1", Summary: "tomatoes"},
		{GUID: "2", Title: "Second", URL: "https://a.example/2"},
	}, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: b.ID, Kind: "article"}, "B", "", []FeedEntry{{GUID: "3", Title: "Third", URL: "https://b.example/3"}}, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(ctx, EntryFilter{}, 10)
	if err != nil || len(entries) != 3 {
		t.Fatalf("%v %v", entries, err)
	}
	yes := true
	if _, err := s.UpdateEntry(ctx, entries[0].ID, EntryPatch{IsRead: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateEntry(ctx, entries[1].ID, EntryPatch{Starred: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestAI(ctx, entries[2].ID, "summary", now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAI(ctx, entries[2].ID, "summary", "a short text", "m", "fp", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE sources SET mute_keywords = 'spam' WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, apiKeySetting, "sk-secret"); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"fetch_proxy": "socks5://127.0.0.1:1080", "fetch_proxy_user": "alice", "fetch_proxy_password": "pw-global"} {
		if err := s.SetSetting(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE sources SET proxy = 'http://127.0.0.1:7890', proxy_user = 'bob', proxy_password = 'pw-source' WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, []byte("hash"), "test", now); err != nil {
		t.Fatal(err)
	}
	return s, file
}

func backupOf(t *testing.T, s *Store) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := s.Backup(context.Background(), &buf, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBackupLeavesOutTheKeyAndSessions(t *testing.T) {
	s, file := seeded(t)
	data := backupOf(t, s)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("%v %v", zr, err)
	}
	var manifest Manifest
	in, _ := zr.File[0].Open()
	if zr.File[0].Name != manifestName || json.NewDecoder(in).Decode(&manifest) != nil {
		t.Fatalf("first file %s", zr.File[0].Name)
	}
	if manifest.App != "oriel" || manifest.Format != 1 || manifest.Version != "1.2.3" || manifest.Schema != count(t, s, "PRAGMA user_version") || time.Since(manifest.CreatedAt) > time.Minute {
		t.Fatalf("manifest %+v", manifest)
	}
	snap, _ := zr.File[1].Open()
	raw, err := io.ReadAll(snap)
	if err != nil || bytes.Contains(raw, []byte("sk-secret")) {
		t.Fatalf("the key is in the snapshot, or it cannot be read: %v", err)
	}
	copyFile := filepath.Join(t.TempDir(), "copy.sqlite")
	if err := os.WriteFile(copyFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", copyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sessions, keys, sources, settings int
	db.QueryRow("SELECT count(*) FROM sessions").Scan(&sessions)
	db.QueryRow("SELECT count(*) FROM settings WHERE key = ?", apiKeySetting).Scan(&keys)
	db.QueryRow("SELECT count(*) FROM sources").Scan(&sources)
	db.QueryRow("SELECT count(*) FROM settings WHERE key = 'theme'").Scan(&settings)
	if sessions != 0 || keys != 0 || sources != 2 || settings != 1 {
		t.Fatalf("sessions %d, keys %d, sources %d, theme %d", sessions, keys, sources, settings)
	}
	// The proxy addresses are kept, their user names and passwords are not.
	var users, address, sourceProxy, sourceSecrets int
	db.QueryRow("SELECT count(*) FROM settings WHERE key IN ('fetch_proxy_user', 'fetch_proxy_password')").Scan(&users)
	db.QueryRow("SELECT count(*) FROM settings WHERE key = 'fetch_proxy'").Scan(&address)
	db.QueryRow("SELECT count(*) FROM sources WHERE proxy = 'http://127.0.0.1:7890'").Scan(&sourceProxy)
	db.QueryRow("SELECT count(*) FROM sources WHERE proxy_user <> '' OR proxy_password <> ''").Scan(&sourceSecrets)
	if users != 0 || address != 1 || sourceProxy != 1 || sourceSecrets != 0 {
		t.Fatalf("proxy secrets %d, address %d, source proxy %d, source secrets %d", users, address, sourceProxy, sourceSecrets)
	}
	if count(t, s, "SELECT count(*) FROM sources WHERE proxy_user = 'bob' AND proxy_password = 'pw-source'") != 1 {
		t.Fatal("the backup changed the source's proxy credentials")
	}
	// The live database keeps both.
	if count(t, s, "SELECT count(*) FROM sessions") != 1 || count(t, s, "SELECT count(*) FROM settings WHERE key = ?", apiKeySetting) != 1 {
		t.Fatal("the backup changed the live data")
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), scratchPrefix) {
			t.Fatalf("scratch left behind: %s", e.Name())
		}
	}
}

func TestBackupFileWorksWhileTheStoreIsOpen(t *testing.T) {
	s, file := seeded(t)
	var buf bytes.Buffer
	if err := BackupFile(context.Background(), file, &buf, "dev"); err != nil {
		t.Fatal(err)
	}
	fresh := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	r, err := fresh.PrepareRestore(context.Background(), &buf, MaxBackup)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Discard()
	if _, err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count(t, fresh, "SELECT count(*) FROM entries") != count(t, s, "SELECT count(*) FROM entries") {
		t.Fatal("entries differ")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _ := seeded(t)
	var ids []int64
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM sources ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if _, err := s.CreateRule(ctx, Rule{SourceIDs: ids, Keywords: "go", Exclude: "ads", Star: true, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	data := backupOf(t, s)

	other := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	c := newLogSource(t, other, "https://c.example/feed")
	if _, err := other.CreateRule(ctx, Rule{SourceIDs: []int64{c.ID}, Keywords: "old", Star: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := other.SaveFeed(ctx, FeedSource{ID: c.ID, Kind: "article"}, "C", "", []FeedEntry{{GUID: "x", Title: "Elsewhere", URL: "https://c.example/x"}}, "", time.Time{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := other.SetSetting(ctx, apiKeySetting, "sk-mine"); err != nil {
		t.Fatal(err)
	}
	if err := other.SetSetting(ctx, "fetch_proxy_user", "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.CreateSession(ctx, []byte("mine"), "ua", time.Now()); err != nil {
		t.Fatal(err)
	}

	r, err := other.PrepareRestore(ctx, bytes.NewReader(data), MaxBackup)
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Apply(ctx)
	r.Discard()
	if err != nil || res.Sources != 2 || res.Entries != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	if count(t, other, "SELECT count(*) FROM sources WHERE url = 'https://c.example/feed'") != 0 {
		t.Fatal("the old subscription survived")
	}
	if rules, err := other.Rules(ctx); err != nil || len(rules) != 1 || !slices.Equal(rules[0].SourceIDs, ids) || rules[0].Exclude != "ads" || rules[0].Keywords != "go" {
		t.Fatalf("the rule did not come back as it was: %+v %v", rules, err)
	}
	if count(t, other, "SELECT count(*) FROM entries WHERE is_read = 1") != 1 || count(t, other, "SELECT count(*) FROM entries WHERE starred = 1") != 1 {
		t.Fatal("read or starred state lost")
	}
	if count(t, other, "SELECT count(*) FROM entry_ai WHERE status = 'ok'") != 1 {
		t.Fatal("AI result lost")
	}
	if count(t, other, "SELECT count(*) FROM sources WHERE mute_keywords = 'spam'") != 1 {
		t.Fatal("per-source setting lost")
	}
	if v, err := other.Setting(ctx, "theme"); err != nil || v != "dark" {
		t.Fatalf("setting %q %v", v, err)
	}
	// This server keeps its own key and sessions.
	if v, _ := other.Setting(ctx, apiKeySetting); v != "sk-mine" {
		t.Fatalf("key %q", v)
	}
	if v, _ := other.Setting(ctx, "fetch_proxy_user"); v != "mine" {
		t.Fatalf("proxy user %q", v)
	}
	if v, _ := other.Setting(ctx, "fetch_proxy_password"); v != "" {
		t.Fatalf("the backup's proxy password was restored: %q", v)
	}
	if v, _ := other.Setting(ctx, "fetch_proxy"); v != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy address %q", v)
	}
	if count(t, other, "SELECT count(*) FROM sources WHERE proxy = 'http://127.0.0.1:7890' AND proxy_user = '' AND proxy_password = ''") != 1 {
		t.Fatal("the source's proxy was not restored without its credentials")
	}
	if count(t, other, "SELECT count(*) FROM sessions") != 1 {
		t.Fatal("sessions lost")
	}
	// The search index was rebuilt.
	found, err := other.Entries(ctx, EntryFilter{Query: "Quantum"}, 10)
	if err != nil || len(found) != 1 {
		t.Fatalf("search: %v %v", found, err)
	}
	found, err = other.Entries(ctx, EntryFilter{Query: "Elsewhere"}, 10)
	if err != nil || len(found) != 0 {
		t.Fatalf("old entry still found: %v %v", found, err)
	}
	if count(t, other, "SELECT count(*) FROM pragma_foreign_key_check") != 0 {
		t.Fatal("foreign keys broken")
	}
}

// zipOf builds a backup file from a manifest and a snapshot.
func zipOf(t *testing.T, manifest any, snapshot []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if manifest != nil {
		w, _ := zw.Create(manifestName)
		json.NewEncoder(w).Encode(manifest)
	}
	if snapshot != nil {
		w, _ := zw.Create(snapshotName)
		w.Write(snapshot)
	}
	zw.Close()
	return buf.Bytes()
}

func snapshotOf(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := zr.Open(snapshotName)
	raw, _ := io.ReadAll(f)
	return raw
}

func TestRestoreRefusesWhatItCannotTrust(t *testing.T) {
	ctx := context.Background()
	s, _ := seeded(t)
	data := backupOf(t, s)
	snap := snapshotOf(t, data)
	good := Manifest{Format: 1, App: "oriel", Schema: 16}

	damaged := bytes.Clone(snap)
	for i := 4096; i < len(damaged) && i < 4096+2048; i++ {
		damaged[i] ^= 0xff
	}
	newer := filepath.Join(t.TempDir(), "newer.sqlite")
	if err := os.WriteFile(newer, snap, 0o600); err != nil {
		t.Fatal(err)
	}
	ndb, _ := sql.Open("sqlite", newer)
	if _, err := ndb.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	ndb.Close()
	newerSnap, _ := os.ReadFile(newer)

	tests := []struct {
		name string
		file []byte
		want error
	}{
		{"not a zip", []byte("hello"), ErrInvalidBackup},
		{"no manifest", zipOf(t, nil, snap), ErrInvalidBackup},
		{"no snapshot", zipOf(t, good, nil), ErrInvalidBackup},
		{"other app", zipOf(t, Manifest{Format: 1, App: "other"}, snap), ErrInvalidBackup},
		{"not a database", zipOf(t, good, []byte("this is not a database file at all")), ErrCorruptBackup},
		{"damaged database", zipOf(t, good, damaged), ErrCorruptBackup},
		{"newer schema", zipOf(t, good, newerSnap), ErrSchemaNewer},
		{"newer format", zipOf(t, Manifest{Format: 2, App: "oriel"}, snap), ErrSchemaNewer},
	}
	other := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	c := newLogSource(t, other, "https://c.example/feed")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := other.PrepareRestore(ctx, bytes.NewReader(tc.file), MaxBackup)
			if r != nil {
				r.Discard()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if count(t, other, "SELECT count(*) FROM sources WHERE id = ?", c.ID) != 1 || count(t, other, "SELECT count(*) FROM sources") != 1 {
				t.Fatal("the data changed")
			}
		})
	}
	entries, _ := os.ReadDir(filepath.Dir(other.path))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), scratchPrefix) {
			t.Fatalf("scratch left behind: %s", e.Name())
		}
	}
}

func TestRestoreRefusesATooLargeUpload(t *testing.T) {
	s, _ := seeded(t)
	data := backupOf(t, s)
	if _, err := s.PrepareRestore(context.Background(), bytes.NewReader(data), int64(len(data)-1)); !errors.Is(err, ErrBackupTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if r, err := s.PrepareRestore(context.Background(), bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	} else {
		r.Discard()
	}
}

func TestRestoreMigratesAnOlderBackup(t *testing.T) {
	ctx := context.Background()
	s, _ := seeded(t)
	old := filepath.Join(t.TempDir(), "old.sqlite")
	if err := os.WriteFile(old, snapshotOf(t, backupOf(t, s)), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	// Make it look like version 20, before the link key that groups an article carried by several sources, the rules and the extraction and AI queue indexes.
	for _, stmt := range []string{
		"DROP TRIGGER rules_lose_last_source", "DROP TABLE rule_sources", "DROP TABLE rules", "DROP INDEX entry_ai_open", "CREATE INDEX entry_ai_status ON entry_ai (status)", "DROP INDEX entries_extract_open", "DROP INDEX entries_link_key", "ALTER TABLE entries DROP COLUMN link_key", "ALTER TABLE sources DROP COLUMN proxy", "ALTER TABLE sources DROP COLUMN proxy_user", "ALTER TABLE sources DROP COLUMN proxy_password", "ALTER TABLE sources DROP COLUMN moved_url", "ALTER TABLE sources DROP COLUMN moved_seen", "ALTER TABLE sources DROP COLUMN moved_dismissed", "ALTER TABLE sources DROP COLUMN feed_title", "ALTER TABLE sources DROP COLUMN fetch_user_agent", "ALTER TABLE sources DROP COLUMN fetch_referer", "ALTER TABLE sources DROP COLUMN open_target", "ALTER TABLE sources DROP COLUMN hide_from_all", "PRAGMA user_version = 20",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	raw, _ := os.ReadFile(old)

	other := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	r, err := other.PrepareRestore(ctx, bytes.NewReader(zipOf(t, Manifest{Format: 1, App: "oriel", Schema: 20}, raw)), MaxBackup)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Discard()
	if res, err := r.Apply(ctx); err != nil || res.Sources != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if count(t, other, "SELECT count(*) FROM entries WHERE link_key <> ''") == 0 {
		t.Fatal("the migrated link keys are missing")
	}
	if count(t, other, "SELECT count(*) FROM rules") != 0 {
		t.Fatal("the rules table is missing")
	}
}

func TestOpenRemovesScratchLeftByACrash(t *testing.T) {
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	open(t, file).Close()
	left := filepath.Join(filepath.Dir(file), scratchPrefix+"123")
	if err := os.Mkdir(left, 0o700); err != nil {
		t.Fatal(err)
	}
	open(t, file)
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Fatalf("scratch survived: %v", err)
	}
}

func TestRestoreWaitsOutAVacuum(t *testing.T) {
	s, _ := seeded(t)
	r, err := s.PrepareRestore(context.Background(), bytes.NewReader(backupOf(t, s)), MaxBackup)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Discard()
	s.vacuuming.Store(true)
	if _, err := r.Apply(context.Background()); !errors.Is(err, ErrVacuumRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestBackupLeavesOutADeletedSubscription(t *testing.T) {
	s, _ := seeded(t)
	ctx := context.Background()
	sources, err := s.Sources(ctx)
	if err != nil || len(sources) != 2 {
		t.Fatalf("%v %v", sources, err)
	}
	if err := s.DeleteSource(ctx, sources[0].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	data := backupOf(t, s)
	other := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	restore, err := other.PrepareRestore(ctx, bytes.NewReader(data), MaxBackup)
	if err != nil {
		t.Fatal(err)
	}
	defer restore.Discard()
	result, err := restore.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sources != 1 || count(t, other, "SELECT count(*) FROM sources WHERE deleted_at IS NOT NULL") != 0 ||
		count(t, other, "SELECT count(*) FROM entries WHERE source_id = ?", sources[0].ID) != 0 {
		t.Fatalf("result %+v", result)
	}
}
