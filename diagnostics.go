package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/store"
)

// diagnostics writes the diagnostics file from a data directory, without a running server (docs/product.md, "诊断文件").
// The recent errors live in the memory of a server, so the file says it has none to show.
func diagnostics(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("oriel diagnostics", flag.ContinueOnError)
	dataDir := flags.String("data", "", "data directory (default: the user data directory)")
	output := flags.String("o", "", "file to write, - for standard output")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: oriel diagnostics [-data dir] -o file")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 || *output == "" {
		flags.Usage()
		return 2
	}
	if *dataDir == "" {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			slog.Error("No user data directory", "error", err)
			return 1
		}
		*dataDir = dir
	}
	database := filepath.Join(*dataDir, "oriel.sqlite")
	if _, err := os.Stat(database); err != nil {
		fmt.Fprintf(flags.Output(), "No Oriel database in %s.\n", *dataDir)
		return 1
	}
	ctx := context.Background()
	st, err := store.Open(ctx, database)
	if errors.Is(err, store.ErrLocked) {
		fmt.Fprintln(flags.Output(), "Another Oriel process is using this data directory. Quit it first, or export the diagnostics file from Settings.")
		return 1
	}
	if err != nil {
		slog.Error("Cannot open the database", "error", err)
		return 1
	}
	defer st.Close()
	body, err := diag.Build(ctx, st, diag.Info{Version: version, Mode: diag.ModeDataDir, Now: time.Now()})
	if err != nil {
		slog.Error("Cannot write the diagnostics", "error", err)
		return 1
	}
	if *output == "-" {
		_, err = stdout.Write(body)
	} else {
		err = os.WriteFile(*output, body, 0o644)
	}
	if err != nil {
		slog.Error("Cannot save the diagnostics", "error", err)
		return 1
	}
	return 0
}
