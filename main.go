package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/ui/connect"
	"github.com/yangflow/oriel/internal/ui/example"
	"github.com/yangflow/oriel/internal/ui/startup"
)

// defaultPort is where `oriel serve` listens unless -port says otherwise.
const defaultPort = 7470

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Time(attr.Key, attr.Value.Time().UTC())
			}
			return attr
		},
	})))
	// The name decides the user data directory: ~/Library/Application Support/Oriel, %AppData%\Oriel, ~/.config/Oriel.
	mygo.App.SetName("Oriel")
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		os.Exit(serve(os.Args[2:]))
	}
	desktop()
}

// serve runs the server without a window. It never starts the app event loop, so no UI library is loaded.
func serve(args []string) int {
	flags := flag.NewFlagSet("oriel serve", flag.ContinueOnError)
	dataDir := flags.String("data", "", "data directory (default: the user data directory)")
	port := flags.Int("port", defaultPort, "TCP port")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: oriel serve [-data dir] [-port n]")
		fmt.Fprintln(flags.Output(), "Set ORIEL_PASSWORD to require sign-in and listen on every interface.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
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
	password := os.Getenv("ORIEL_PASSWORD")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	b, err := start(ctx, *dataDir, password, *port)
	if err != nil {
		slog.Error("Cannot start", "error", err, "data", *dataDir)
		return 1
	}
	slog.Info("Serving", "address", b.addr, "data", *dataDir, "auth", password != "", "version", version)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.close(shutdown); err != nil {
		slog.Error("Shutdown failed", "error", err)
		return 1
	}
	return 0
}

// desktop serves on a free loopback port without a password and opens the window. Signed in to a remote server, it
// shows that server's data instead.
func desktop() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.Use(native.Plugin)
	var b *backend
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err == nil {
		b, err = start(context.Background(), dataDir, "", 0)
	}
	if err != nil {
		slog.Error("Cannot start", "error", err, "data", dataDir)
	} else {
		slog.Info("Serving", "address", b.addr, "data", dataDir, "version", version)
	}
	var window *mygo.Window
	mygo.App.WhenReady(func() {
		if err != nil {
			mygo.Dialog.Error(startup.Message(mygo.App.Locale(), errors.Is(err, store.ErrLocked), err))
			mygo.App.Quit()
			return
		}
		systemLocale := mygo.App.Locale()
		home := example.New(systemLocale)
		goAsync := func(work func() func()) {
			go func() {
				apply := work()
				window.Update(apply)
			}()
		}
		root := newShell(systemLocale, filepath.Join(dataDir, sessionFile), home.View, goAsync)
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Oriel", Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout}, updater.MenuItem(), mygo.Separator(),
				{Label: connect.MenuLabel(systemLocale), Click: func(*mygo.MenuItem, *mygo.Window) {
					window.Update(root.openConnect)
					window.Restore()
					window.Focus()
				}},
				mygo.Separator(), {Role: mygo.RoleQuit},
			}},
			{Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu},
		}))
		window = mygo.NewWindow(mygo.WindowOptions{
			Title: "Oriel", Width: 960, Height: 640, MinWidth: 640, MinHeight: 480,
			Content: ui.View(root.View),
		})
		root.restore()
	})
	mygo.App.OnSecondInstance(func([]string, string) {
		if window != nil {
			window.Restore()
			window.Focus()
		}
	})
	mygo.App.OnWindowAllClosed(func() { mygo.App.Quit() })
	mygo.App.OnQuit(func() {
		if b == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.close(ctx); err != nil {
			slog.Error("Shutdown failed", "error", err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		slog.Error("Application failed", "error", err)
		os.Exit(1)
	}
}
