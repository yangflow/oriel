package store

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A backup is a zip file holding a manifest and a snapshot of the database. docs/product.md describes it.
const (
	manifestName  = "manifest.json"
	snapshotName  = "oriel.sqlite"
	backupFormat  = 1
	backupApp     = "oriel"
	scratchPrefix = ".oriel-tmp-"
	apiKeySetting = "ai_api_key" // the value of ai.APIKeySetting, which this package cannot import
	maxManifest   = 64 << 10
	checkpoint    = "PRAGMA wal_checkpoint(TRUNCATE)"
)

// secretSettings are the settings a backup leaves out and a restore keeps: the API key and the credentials of the
// global fetch proxy (the values of ai.APIKeySetting and fetch.ProxyUserSetting and ProxyPasswordSetting, which this
// package cannot import). The credentials of each source's own proxy are columns and are cleared from the snapshot.
const secretSettings = "('" + apiKeySetting + "', 'fetch_proxy_user', 'fetch_proxy_password')"

// MaxBackup is the largest snapshot a restore unpacks, and the upload limit the server uses.
const MaxBackup = 4 << 30

var (
	// ErrInvalidBackup reports a file that is not an Oriel backup: not a zip, or without a manifest or a snapshot.
	ErrInvalidBackup = errors.New("not an Oriel backup")
	// ErrCorruptBackup reports a backup whose snapshot fails the SQLite integrity check.
	ErrCorruptBackup = errors.New("the backup is damaged")
	// ErrBackupTooLarge reports a backup over the limit.
	ErrBackupTooLarge = errors.New("the backup is too large")
)

// Manifest is the manifest.json of a backup.
type Manifest struct {
	Format    int       `json:"format"`    // the layout of the file, 1
	App       string    `json:"app"`       // always "oriel"
	Version   string    `json:"version"`   // the app version that made it
	Schema    int       `json:"schema"`    // PRAGMA user_version of the snapshot
	CreatedAt time.Time `json:"createdAt"` // UTC
}

// Backup writes a backup of the database to w while the app keeps running. The snapshot is taken with VACUUM INTO, so
// it is consistent. Sessions and the AI API key are left out of the copy, and so is the search index, which a restore
// builds again; the live database is not touched.
func (s *Store) Backup(ctx context.Context, w io.Writer, version string) error {
	return writeBackup(ctx, s.db, filepath.Dir(s.path), w, version)
}

// BackupFile does the same for a database file that no Store has open, including one an Oriel process is using.
func BackupFile(ctx context.Context, file string, w io.Writer, version string) error {
	if _, err := os.Stat(file); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	return writeBackup(ctx, db, filepath.Dir(file), w, version)
}

func writeBackup(ctx context.Context, db *sql.DB, dir string, w io.Writer, version string) error {
	scratch, err := os.MkdirTemp(dir, scratchPrefix+"*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	snapshot := filepath.Join(scratch, snapshotName)
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	schema, err := strip(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("strip: %w", err)
	}
	file, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(w)
	manifest, err := zw.Create(manifestName)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(manifest).Encode(Manifest{
		Format: backupFormat, App: backupApp, Version: version, Schema: schema, CreatedAt: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		return err
	}
	data, err := zw.CreateHeader(&zip.FileHeader{Name: snapshotName, Method: zip.Deflate, Modified: time.Now().UTC()})
	if err != nil {
		return err
	}
	if _, err := io.Copy(data, file); err != nil {
		return err
	}
	return zw.Close()
}

// strip removes from a snapshot what a backup leaves out, shrinks it, and returns its schema version. A subscription
// deleted within the undo window goes too, with all that hangs off it.
func strip(ctx context.Context, snapshot string) (int, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(snapshot)+"?_pragma=foreign_keys(1)")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		"DELETE FROM sessions",
		"DELETE FROM sources WHERE deleted_at IS NOT NULL",
		"DELETE FROM settings WHERE key IN " + secretSettings,
		"UPDATE sources SET proxy_user = '', proxy_password = ''",
		"INSERT INTO entries_fts (entries_fts) VALUES ('delete-all')",
		"VACUUM",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return 0, err
		}
	}
	var schema int
	err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schema)
	return schema, err
}

// Restore is an uploaded backup that has been checked and brought up to this build's schema, and waits to replace the
// data. Discard it when done, whether or not it was applied.
type Restore struct {
	store    *Store
	scratch  string
	snapshot string
}

// RestoreResult counts what a restore brought in.
type RestoreResult struct {
	Sources int
	Entries int
}

// PrepareRestore reads a backup from src, at most limit bytes, and checks it: the manifest, the integrity of the
// snapshot, and its schema version. An older schema is migrated up; a newer one is refused with ErrSchemaNewer. The
// database in use is not touched. It returns ErrInvalidBackup, ErrCorruptBackup, ErrBackupTooLarge or ErrSchemaNewer
// for a file it will not restore.
func (s *Store) PrepareRestore(ctx context.Context, src io.Reader, limit int64) (*Restore, error) {
	scratch, err := os.MkdirTemp(filepath.Dir(s.path), scratchPrefix+"*")
	if err != nil {
		return nil, err
	}
	r := &Restore{store: s, scratch: scratch, snapshot: filepath.Join(scratch, snapshotName)}
	if err := r.prepare(ctx, src, limit); err != nil {
		r.Discard()
		return nil, err
	}
	return r, nil
}

func (r *Restore) prepare(ctx context.Context, src io.Reader, limit int64) error {
	upload := filepath.Join(r.scratch, "upload")
	if err := copyLimited(upload, src, limit); err != nil {
		return err
	}
	zr, err := zip.OpenReader(upload)
	if err != nil {
		return ErrInvalidBackup
	}
	defer zr.Close()
	var manifest, snapshot *zip.File
	for _, f := range zr.File {
		switch f.Name {
		case manifestName:
			manifest = f
		case snapshotName:
			snapshot = f
		}
	}
	if manifest == nil || snapshot == nil {
		return ErrInvalidBackup
	}
	if err := checkManifest(manifest); err != nil {
		return err
	}
	if snapshot.UncompressedSize64 > MaxBackup {
		return ErrBackupTooLarge
	}
	in, err := snapshot.Open()
	if err != nil {
		return ErrInvalidBackup
	}
	defer in.Close()
	if err := copyLimited(r.snapshot, in, MaxBackup); err != nil {
		if errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrInvalidBackup
		}
		return err
	}
	db, err := sql.Open("sqlite", dsn(r.snapshot))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var verdict string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&verdict); err != nil || verdict != "ok" {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrCorruptBackup
	}
	var schema int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schema); err != nil {
		return ErrCorruptBackup
	}
	if schema < 1 {
		return ErrInvalidBackup
	}
	if err := (&Store{db: db}).migrate(ctx); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, checkpoint)
	return err
}

// checkManifest reads the manifest and refuses what is not an Oriel backup of a layout this build knows.
func checkManifest(f *zip.File) error {
	in, err := f.Open()
	if err != nil {
		return ErrInvalidBackup
	}
	defer in.Close()
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(in, maxManifest)).Decode(&m); err != nil || m.App != backupApp || m.Format < 1 {
		return ErrInvalidBackup
	}
	if m.Format > backupFormat {
		return ErrSchemaNewer
	}
	return nil
}

// copyLimited writes src to a new file at most limit bytes long, and returns ErrBackupTooLarge for more.
func copyLimited(file string, src io.Reader, limit int64) error {
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(src, limit+1))
	if err := errors.Join(err, out.Close()); err != nil {
		return err
	}
	if n > limit {
		return ErrBackupTooLarge
	}
	return nil
}

// Discard removes the scratch files.
func (r *Restore) Discard() { os.RemoveAll(r.scratch) }

// Apply replaces the subscriptions, entries, read and starred state, AI results, fetch records and settings with the
// backup's, in one transaction: if anything fails the transaction rolls back and the data is as it was. Sessions and
// the AI API key stay as they are on this server, because a backup holds neither. The search index is built again
// inside the same transaction. Requests that run meanwhile see either all of the old data or all of the new, and the
// database is never closed. The caller stops the fetch loop and the AI worker first (see the server).
func (r *Restore) Apply(ctx context.Context) (result RestoreResult, err error) {
	s := r.store
	if s.vacuuming.Load() {
		return result, ErrVacuumRunning
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS backup", r.snapshot); err != nil {
		return result, err
	}
	defer func() {
		if _, derr := conn.ExecContext(context.WithoutCancel(ctx), "DETACH DATABASE backup"); derr != nil {
			err = errors.Join(err, derr)
		}
	}()
	tables, err := tablesToCarry(ctx, conn)
	if err != nil {
		return result, err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if _, err := conn.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		return result, err
	}
	// Children are deleted before the parents they point to, and written after them.
	for i := len(tables) - 1; i >= 0; i-- {
		stmt := "DELETE FROM main." + quote(tables[i])
		if tables[i] == "settings" {
			stmt += " WHERE key NOT IN " + secretSettings
		}
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return result, fmt.Errorf("clear %s: %w", tables[i], err)
		}
	}
	for _, table := range tables {
		columns, err := columnList(ctx, conn, table)
		if err != nil {
			return result, err
		}
		stmt := fmt.Sprintf("INSERT INTO main.%[1]s (%[2]s) SELECT %[2]s FROM backup.%[1]s", quote(table), columns)
		if table == "settings" {
			stmt += " WHERE key NOT IN " + secretSettings
		}
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return result, fmt.Errorf("restore %s: %w", table, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO main.entries_fts (entries_fts) VALUES ('rebuild')"); err != nil {
		return result, fmt.Errorf("rebuild the search index: %w", err)
	}
	var broken int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&broken); err != nil {
		return result, err
	}
	if broken > 0 {
		return result, ErrCorruptBackup
	}
	if err := conn.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM main.sources), (SELECT count(*) FROM main.entries)").
		Scan(&result.Sources, &result.Entries); err != nil {
		return result, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return result, err
	}
	committed = true
	// A failed checkpoint only leaves the log larger.
	conn.ExecContext(ctx, checkpoint)
	return result, nil
}

// tablesToCarry lists the tables to carry over, in the order they were created: not the sessions, not SQLite's own
// tables, and not the search index and its shadow tables. Each must exist in the backup too.
func tablesToCarry(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT name FROM main.sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name NOT LIKE 'entries\_fts%' ESCAPE '\' AND name <> 'sessions'
		ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range names {
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM backup.sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, fmt.Errorf("%w: no table %s", ErrInvalidBackup, name)
		}
	}
	return names, nil
}

// columnList is the quoted, comma-separated columns of a table of the database in use.
func columnList(ctx context.Context, conn *sql.Conn, table string) (string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		names = append(names, quote(name))
	}
	return strings.Join(names, ", "), rows.Err()
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// removeScratch deletes the scratch directories a crashed backup or restore left in dir. The caller holds the lock of
// the database, so none of them is in use.
func removeScratch(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), scratchPrefix) {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}
