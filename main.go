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
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/ui/connect"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/settings"
	"github.com/yangflow/oriel/internal/ui/startup"
	"github.com/yangflow/oriel/internal/ui/timeline"
)

// defaultPort is where `oriel serve` listens unless -port says otherwise.
const defaultPort = 7470

// logs keeps the recent warnings and errors of this process for the diagnostics file.
var logs *diag.Handler

func main() {
	logs = diag.NewHandler(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Time(attr.Key, attr.Value.Time().UTC())
			}
			return attr
		},
	}), diag.Keep)
	slog.SetDefault(slog.New(logs))
	// The name decides the user data directory: ~/Library/Application Support/Oriel, %AppData%\Oriel, ~/.config/Oriel.
	mygo.App.SetName("Oriel")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Exit(serve(os.Args[2:]))
		case "backup", "restore":
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			command := backupCommand
			if os.Args[1] == "restore" {
				command = restoreCommand
			}
			code := command(ctx, os.Args[2:], os.Stdout, os.Stderr)
			stop()
			os.Exit(code)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "diagnostics" {
		os.Exit(diagnostics(os.Args[2:], os.Stdout))
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
	b, err := start(ctx, *dataDir, password, diag.ModeHeadless, *port)
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

// windowStateKey names the main window's remembered position, size and maximized state (window-state.json in the
// user data directory). MyGo captures it after the window settles, in memory, and writes it when the window closes.
const windowStateKey = "oriel-main"

// mainWindowOptions is the main window: 960x640 centered the first time, never below 640x480, and back where the reader
// left it. A saved position that no display shows any more falls back to centered on the primary display.
func mainWindowOptions(content mygo.Content) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title: "Oriel", Width: 960, Height: 640, MinWidth: 640, MinHeight: 480, StateKey: windowStateKey,
		// The view draws its own toolbar row, 52 high: the window buttons (14 high) sit centered in it.
		TitleBarStyle: mygo.TitleBarHidden, TrafficLightPosition: &mygo.Point{X: 19, Y: 19},
		Content: content,
	}
}

// desktop serves on a free loopback port without a password and opens the window. Signed in to a remote server, it
// shows that server's data instead.
func desktop() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// The update window speaks the UI's language, so a Traditional Chinese system reads Simplified there too.
	mygo.Use(native.New(updater.Options{Language: locale.Language(mygo.App.Locale())}))
	client.SetVersion(version)
	var b *backend
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err == nil {
		b, err = start(context.Background(), dataDir, "", diag.ModeDesktop, 0)
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
		goAsync := func(work func() func()) {
			go func() {
				apply := work()
				window.Update(apply)
			}()
		}
		// The files asked for are OPML, the diagnostics text or a backup (the settings) or Markdown (the reader), told by
		// the name offered.
		filters := func(name, label string) []mygo.FileFilter {
			switch filepath.Ext(name) {
			case ".md":
				return []mygo.FileFilter{{Name: label, Extensions: []string{"md"}}}
			case ".txt":
				return []mygo.FileFilter{{Name: label, Extensions: []string{"txt"}}}
			case ".oriel-backup":
				return []mygo.FileFilter{{Name: label, Extensions: []string{"oriel-backup"}}}
			}
			return []mygo.FileFilter{{Name: label, Extensions: []string{"opml", "xml"}}}
		}
		opened := func(name string, extensions []string) []mygo.FileFilter {
			if len(extensions) == 0 {
				extensions = []string{"opml", "xml"}
			}
			return []mygo.FileFilter{{Name: name, Extensions: extensions}}
		}
		files := settings.Deps{
			Open: func(filter string, extensions ...string) (string, error) {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Filters: opened(filter, extensions)})
				if err != nil {
					slog.Error("Cannot show the open dialog", "error", err)
				}
				if err != nil || len(paths) == 0 {
					return "", err
				}
				return paths[0], nil
			},
			Save: func(name, filter string) (string, error) {
				path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, DefaultPath: name,
					Filters: filters(name, filter), CreateDirectories: true})
				if err != nil {
					slog.Error("Cannot show the save dialog", "error", err)
				}
				return path, err
			},
		}
		root := newShell(systemLocale, filepath.Join(dataDir, sessionFile), filepath.Join(dataDir, prefs.File),
			client.Session{Server: "http://" + b.addr}, goAsync, files)
		setMenu := func(language string) {
			help, keys := timeline.MenuLabels(language)
			view, commands := timeline.TimelineMenu(language)
			var items []*mygo.MenuItem
			for _, it := range commands {
				items = append(items, &mygo.MenuItem{Label: it.Label, Click: func(*mygo.MenuItem, *mygo.Window) {
					window.Update(func() { root.run(it.Command) })
				}})
			}
			subscriptions, refreshAll := timeline.RefreshMenuLabels(language)
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
				{Label: "Oriel", Submenu: []*mygo.MenuItem{
					{Role: mygo.RoleAbout}, updater.MenuItem(), mygo.Separator(),
					{Label: connect.MenuLabel(language), Click: func(*mygo.MenuItem, *mygo.Window) {
						window.Update(root.openConnect)
						window.Restore()
						window.Focus()
					}},
					{Label: settings.MenuLabel(language), Accelerator: "CmdOrCtrl+,",
						Click: func(*mygo.MenuItem, *mygo.Window) {
							window.Update(root.openSettings)
							window.Restore()
							window.Focus()
						}},
					mygo.Separator(), {Role: mygo.RoleQuit},
				}},
				{Role: mygo.RoleEditMenu}, {Label: view, Submenu: items},
				{Label: subscriptions, Submenu: []*mygo.MenuItem{
					{Label: refreshAll, Accelerator: "CmdOrCtrl+R", Click: func(*mygo.MenuItem, *mygo.Window) {
						window.Update(root.refreshAll)
					}},
				}},
				{Role: mygo.RoleWindowMenu},
				{Label: help, Submenu: []*mygo.MenuItem{
					{Label: keys, Accelerator: "CmdOrCtrl+/", Click: func(*mygo.MenuItem, *mygo.Window) {
						window.Update(root.showKeys)
					}},
					{Label: settings.DiagnosticsMenuLabel(language), Click: func(*mygo.MenuItem, *mygo.Window) {
						window.Update(root.exportDiagnostics)
						window.Restore()
						window.Focus()
					}},
				}},
			}))
		}
		root.language = setMenu
		setMenu(locale.Resolve(root.prefs.Language, systemLocale))
		window = mygo.NewWindow(mainWindowOptions(ui.View(root.View)))
		// The marker of what the reader last looked at moves only while the window is in front.
		focus := func(on bool) func() { return func() { window.Update(func() { root.focus(on) }) } }
		window.OnFocus(focus(true))
		window.OnBlur(focus(false))
		window.OnHide(focus(false))
		window.OnMinimize(focus(false))
		// A computer that wakes may have its network back: the timeline checks the server at once.
		mygo.Power.OnResume(func() { window.Update(root.wake) })
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
