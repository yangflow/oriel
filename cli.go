package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/yangflow/oriel/internal/store"
)

// databaseFile is the name of the database inside a data directory.
const databaseFile = "oriel.sqlite"

// dataDirectory is dir, or the user data directory when dir is empty.
func dataDirectory(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	return mygo.App.Path(mygo.PathUserData)
}

// backupCommand is `oriel backup`: it writes a backup of the data directory's database to a file. It works while
// Oriel runs, and it never replaces a file that exists.
func backupCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("oriel backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data", "", "data directory (default: the user data directory)")
	out := flags.String("o", "", "backup file to write, usually ending in .oriel-backup")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: oriel backup [-data dir] -o file")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 || *out == "" {
		flags.Usage()
		return 2
	}
	dir, err := dataDirectory(*dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "No user data directory:", err)
		return 1
	}
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot create the backup file:", err)
		return 1
	}
	err = store.BackupFile(ctx, filepath.Join(dir, databaseFile), file, version)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(*out)
		fmt.Fprintln(stderr, "Cannot back up:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Backup written to", *out)
	return 0
}

// restoreCommand is `oriel restore`: it replaces the data of a data directory with a backup. Oriel must not be
// running on that directory, and existing data is only replaced with -force. A refused or failed restore leaves the
// data as it was.
func restoreCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("oriel restore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data", "", "data directory (default: the user data directory)")
	force := flags.Bool("force", false, "replace data that is already there")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: oriel restore [-data dir] [-force] file")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	src, err := os.Open(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "Cannot read the backup file:", err)
		return 1
	}
	defer src.Close()
	dir, err := dataDirectory(*dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "No user data directory:", err)
		return 1
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(stderr, "Cannot use the data directory:", err)
		return 1
	}
	st, err := store.Open(ctx, filepath.Join(dir, databaseFile))
	if errors.Is(err, store.ErrLocked) {
		fmt.Fprintln(stderr, "Oriel is using this data. Quit it first, then restore again.")
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "Cannot open the data:", err)
		return 1
	}
	defer st.Close()
	sources, err := st.Sources(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot read the data:", err)
		return 1
	}
	if len(sources) > 0 && !*force {
		fmt.Fprintf(stderr, "This data already holds %d sources. Restoring replaces all of it; run again with -force to do that.\n", len(sources))
		return 1
	}
	restore, err := st.PrepareRestore(ctx, src, store.MaxBackup)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot restore:", restoreProblem(err))
		return 1
	}
	defer restore.Discard()
	result, err := restore.Apply(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot restore:", restoreProblem(err))
		return 1
	}
	fmt.Fprintf(stdout, "Restored %d sources and %d entries.\n", result.Sources, result.Entries)
	return 0
}

// restoreProblem says in a sentence why a backup was refused.
func restoreProblem(err error) string {
	switch {
	case errors.Is(err, store.ErrSchemaNewer):
		return "the backup is from a newer version of Oriel; update Oriel first."
	case errors.Is(err, store.ErrInvalidBackup):
		return "the file is not an Oriel backup."
	case errors.Is(err, store.ErrCorruptBackup):
		return "the backup is damaged."
	case errors.Is(err, store.ErrBackupTooLarge):
		return "the backup is too large."
	}
	return err.Error()
}
