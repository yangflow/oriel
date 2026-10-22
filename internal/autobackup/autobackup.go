// Package autobackup writes a backup file into a chosen folder on a schedule and keeps only the newest few of them
// (docs/product.md, "自动备份"). The settings and the outcome of the last try live in the settings table.
package autobackup

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"
)

// The settings keys.
const (
	EnabledSetting   = "auto_backup_enabled"
	FolderSetting    = "auto_backup_folder"
	FrequencySetting = "auto_backup_frequency"
	KeepSetting      = "auto_backup_keep"
	LastAtSetting    = "auto_backup_last_at"
	LastFileSetting  = "auto_backup_last_file"
	FailedAtSetting  = "auto_backup_failed_at"
	ErrorSetting     = "auto_backup_error"
)

// The frequencies, the keep range and the default.
const (
	Daily       = "daily"
	Weekly      = "weekly"
	MinKeep     = 1
	MaxKeep     = 100
	DefaultKeep = 7
)

// The codes of a failure.
const (
	FolderMissing     = "folder_missing"
	FolderNotDir      = "folder_not_directory"
	FolderNotWritable = "folder_not_writable"
	DiskFull          = "disk_full"
	Failed            = "failed"
)

const (
	// CheckEvery is how often the scheduler looks whether a backup is due.
	CheckEvery = time.Hour
	// staleTemp is how old a leftover temporary file must be before it is removed.
	staleTemp  = time.Hour
	fileLayout = "2006-01-02-150405"
	suffix     = ".oriel-backup"
)

var (
	// ours matches exactly the names this package writes; nothing else in the folder is ever removed as an old backup.
	ours = regexp.MustCompile(`^Oriel-\d{4}-\d{2}-\d{2}-\d{6}\.oriel-backup$`)
	temp = regexp.MustCompile(`^\.oriel-backup-.*\.tmp$`)
)

// Store is the part of the database the runner uses.
type Store interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	Backup(ctx context.Context, w io.Writer, version string) error
}

// Config is what the user chose.
type Config struct {
	Enabled   bool
	Folder    string
	Frequency string
	Keep      int
}

// Status is the outcome of the last tries, in Unix milliseconds. FailedAt is 0 when the last try succeeded.
type Status struct {
	LastAt   int64
	LastFile string
	FailedAt int64
	Error    string
}

// Load reads the settings and the status. A value that cannot be read is the default.
func Load(ctx context.Context, st Store) (Config, Status, error) {
	var raw [8]string
	for i, key := range []string{EnabledSetting, FolderSetting, FrequencySetting, KeepSetting, LastAtSetting, LastFileSetting,
		FailedAtSetting, ErrorSetting} {
		v, err := st.Setting(ctx, key)
		if err != nil {
			return Config{}, Status{}, err
		}
		raw[i] = v
	}
	cfg := Config{Enabled: raw[0] == "1", Folder: raw[1], Frequency: Daily, Keep: DefaultKeep}
	if raw[2] == Weekly {
		cfg.Frequency = Weekly
	}
	if n, err := strconv.Atoi(raw[3]); err == nil && n >= MinKeep && n <= MaxKeep {
		cfg.Keep = n
	}
	status := Status{LastFile: raw[5], Error: raw[7]}
	status.LastAt, _ = strconv.ParseInt(raw[4], 10, 64)
	status.FailedAt, _ = strconv.ParseInt(raw[6], 10, 64)
	return cfg, status, nil
}

// Interval is how long after a success the next backup is due.
func (c Config) Interval() time.Duration {
	if c.Frequency == Weekly {
		return 7 * 24 * time.Hour
	}
	return 24 * time.Hour
}

// ValidateFolder checks that folder exists, is a directory and can be written to, by making a file in it and removing
// it again. It returns "" or FolderMissing, FolderNotDir or FolderNotWritable.
func ValidateFolder(folder string) string {
	info, err := os.Stat(folder)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return FolderMissing
	case err != nil:
		return FolderNotWritable
	case !info.IsDir():
		return FolderNotDir
	}
	f, err := os.CreateTemp(folder, ".oriel-backup-check-*.tmp")
	if err != nil {
		return FolderNotWritable
	}
	f.Close()
	os.Remove(f.Name())
	return ""
}

// Runner is the scheduler. Run it in a goroutine; it stops with its context.
type Runner struct {
	store   Store
	version string
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Busy reports that a restore or a vacuum is under way, when no backup should start. Nil means never.
	Busy func() bool
	poke chan struct{}
}

// New makes a runner for the store; version goes into the backup files.
func New(st Store, version string) *Runner {
	return &Runner{store: st, version: version, Now: time.Now, poke: make(chan struct{}, 1)}
}

// Poke asks for a check now, after the settings changed. It never blocks.
func (r *Runner) Poke() {
	select {
	case r.poke <- struct{}{}:
	default:
	}
}

// Run checks when it starts, then every hour and whenever poked, until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	tick := time.NewTicker(CheckEvery)
	defer tick.Stop()
	for {
		r.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-r.poke:
		}
	}
}

// Check makes one backup if it is switched on and due, and records how it went. A failure is not retried before the
// next check.
func (r *Runner) Check(ctx context.Context) {
	if ctx.Err() != nil || (r.Busy != nil && r.Busy()) {
		return
	}
	cfg, status, err := Load(ctx, r.store)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot read the automatic backup settings", "error", err)
		}
		return
	}
	if !cfg.Enabled {
		return
	}
	now := r.Now()
	if status.LastAt != 0 && now.Sub(time.UnixMilli(status.LastAt)) < cfg.Interval() {
		return
	}
	name, err := r.write(ctx, cfg, now)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		code := classify(err)
		slog.Warn("Automatic backup failed", "code", code, "error", err)
		r.record(ctx, FailedAtSetting, strconv.FormatInt(now.UnixMilli(), 10), ErrorSetting, code)
		return
	}
	// Written and renamed: that is the success, whatever the cleaning of old files does.
	r.record(ctx, LastAtSetting, strconv.FormatInt(now.UnixMilli(), 10), LastFileSetting, name, FailedAtSetting, "", ErrorSetting, "")
	prune(cfg.Folder, cfg.Keep)
}

// record stores key and value pairs, and stops at the first that fails.
func (r *Runner) record(ctx context.Context, pairs ...string) {
	for i := 0; i < len(pairs); i += 2 {
		if err := r.store.SetSetting(ctx, pairs[i], pairs[i+1]); err != nil {
			slog.Error("Cannot record the automatic backup", "error", err)
			return
		}
	}
}

// codeError carries a failure code of ValidateFolder.
type codeError string

func (e codeError) Error() string { return string(e) }

// write makes the file through a temporary one in the same folder, so that a name that ends in .oriel-backup is always
// a whole backup. It returns the name of the file.
func (r *Runner) write(ctx context.Context, cfg Config, now time.Time) (string, error) {
	if cfg.Folder == "" {
		return "", codeError(FolderMissing)
	}
	if code := ValidateFolder(cfg.Folder); code != "" {
		return "", codeError(code)
	}
	removeStale(cfg.Folder, now)
	f, err := os.CreateTemp(cfg.Folder, ".oriel-backup-*.tmp")
	if err != nil {
		return "", err
	}
	done := false
	defer func() {
		if !done {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	if err := r.store.Backup(ctx, f, r.version); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	name := "Oriel-" + now.Format(fileLayout) + suffix
	if err := os.Rename(f.Name(), filepath.Join(cfg.Folder, name)); err != nil {
		return "", err
	}
	done = true
	return name, nil
}

func classify(err error) string {
	var code codeError
	switch {
	case errors.As(err, &code):
		return string(code)
	case errors.Is(err, syscall.ENOSPC):
		return DiskFull
	case errors.Is(err, fs.ErrNotExist):
		return FolderMissing
	case errors.Is(err, fs.ErrPermission):
		return FolderNotWritable
	}
	return Failed
}

// removeStale deletes temporary files an earlier crash left behind.
func removeStale(folder string, now time.Time) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !temp.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > staleTemp {
			os.Remove(filepath.Join(folder, e.Name()))
		}
	}
}

// prune deletes the oldest of the backups this package wrote beyond keep. The names sort by time. Nothing else is
// touched; a failure is only logged.
func prune(folder string, keep int) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		slog.Warn("Cannot list the automatic backups", "error", err)
		return
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && ours.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.Remove(filepath.Join(folder, names[0])); err != nil {
			slog.Warn("Cannot remove an old automatic backup", "error", err)
		}
		names = names[1:]
	}
}
