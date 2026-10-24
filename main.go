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
	"runtime"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	_ "github.com/egoist/mygo/locales/zh-Hans" // MyGo's own words (find bar, dialogs, menus) in Simplified Chinese
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/server"
	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/ui/connect"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/settings"
	"github.com/yangflow/oriel/internal/ui/startup"
	"github.com/yangflow/oriel/internal/ui/timeline"
)

// defaultPort is where `oriel serve` listens unless -port says otherwise.
const defaultPort = 7470

var (
	// logs keeps the recent warnings and errors of this process for the diagnostics file.
	logs *diag.Handler
	// logFile is the rotated log file in the data directory; it takes records once openLog has found the directory.
	logFile = diag.NewRotator()
)

// systemLocale is the language the system reads best. MyGo's own Locale is the language the app shows, which Oriel sets
// to its interface language, so it cannot say what the system reads.
func systemLocale() string {
	if preferred := mygo.App.PreferredLanguages(); len(preferred) > 0 {
		return preferred[0]
	}
	return mygo.App.Locale()
}

// showMyGoLanguage makes MyGo's own words (find bar, dialogs, menus) follow the interface language, which Oriel keeps in
// its preferences: chosen is the Interface language preference, "" for System, and preferred the languages the system
// reads. MyGo keeps what SetLocale is given for the next launches (on macOS as the app's AppleLanguages, which beat the
// system's), so a language is only given when it must be:
//   - A language the user chose is given, so MyGo shows it whatever the system reads.
//   - System gives "": MyGo follows the system by itself and picks the language of the app's languages (en, zh-Hans)
//     that suits it. That is the language Oriel picks too, except for a Chinese in another script (zh-Hant, zh-TW):
//     Oriel reads every Chinese as Simplified, MyGo finds zh-Hans no match for it and falls back to English. Only then
//     is zh-Hans given, so that a window never mixes the two. PreferredLanguages is not changed by what SetLocale gives, so
//     systemLocale still reads the system, and a later launch where the two agree gives "" again.
func showMyGoLanguage(set func(string), chosen string, preferred []string) {
	if slices.Contains(locale.Languages, chosen) {
		set(chosen)
		return
	}
	system := ""
	if len(preferred) > 0 {
		system = preferred[0]
	}
	own := i18n.Match(preferred, locale.Languages)
	if own == "" {
		own = locale.Languages[0]
	}
	if own == locale.Language(system) {
		set("")
		return
	}
	set(locale.Language(system))
}

// openLog starts the log file in dataDir. A file that cannot be opened is a warning on stderr, not the end of the app.
func openLog(dataDir string) {
	if err := logFile.Open(dataDir); err != nil {
		slog.Warn("Cannot open the log file", "error", err)
	}
}

func main() {
	// stderr gets the records as they are, the log file the redacted ones.
	logs = diag.NewHandler(slog.NewMultiHandler(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{ReplaceAttr: diag.UTC}),
		diag.NewFileHandler(logFile),
	), diag.Keep)
	slog.SetDefault(slog.New(logs))
	// A panic that reaches the top of the main goroutine is written to the log, with its stack, before the app ends.
	defer func() {
		if value := recover(); value != nil {
			diag.LogPanic("main", value)
			os.Exit(2)
		}
	}()
	// The name decides the user data directory: ~/Library/Application Support/Oriel, %AppData%\Oriel, ~/.config/Oriel.
	mygo.App.SetName("Oriel")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Exit(serve(os.Args[2:]))
		case "backup", "restore", "healthcheck":
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			command := backupCommand
			switch os.Args[1] {
			case "restore":
				command = restoreCommand
			case "healthcheck":
				command = healthcheckCommand
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
	proxy := flags.String("trusted-proxy", "", "reverse proxies (comma-separated IPs or CIDRs) whose X-Forwarded-For the login throttle believes")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: oriel serve [-data dir] [-port n] [-trusted-proxy list]")
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
	proxies, err := server.ParseProxies(*proxy)
	if err != nil {
		hint := ""
		if errors.Is(err, server.ErrProxyFormat) {
			hint = " (expected IP addresses or CIDR ranges, comma-separated, e.g. 10.0.0.1 or 10.0.0.0/8,192.0.2.7)"
		}
		fmt.Fprintln(flags.Output(), "-trusted-proxy:", err.Error()+hint)
		return 2
	}
	password := os.Getenv("ORIEL_PASSWORD")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	openLog(*dataDir)
	defer logFile.Close()
	b, err := start(ctx, *dataDir, password, diag.ModeHeadless, *port, proxies)
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
	// MyGo shows one of the languages the app is written in: the ones Oriel's catalogs carry, so that its own controls
	// can follow the interface language (see showLanguage).
	mygo.App.SetLanguages(locale.Languages...)
	// The update window speaks the UI's language, so a Traditional Chinese system reads Simplified there too.
	mygo.Use(native.New(updater.Options{Language: locale.Language(systemLocale())}))
	client.SetVersion(version)
	var b *backend
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err == nil {
		openLog(dataDir)
		b, err = start(context.Background(), dataDir, "", diag.ModeDesktop, 0, nil)
	}
	if err != nil {
		slog.Error("Cannot start", "error", err, "data", dataDir)
	} else {
		slog.Info("Serving", "address", b.addr, "data", dataDir, "version", version)
	}
	var window *mygo.Window
	mygo.App.WhenReady(func() {
		if err != nil {
			mygo.Dialog.Error(startup.Message(systemLocale(), errors.Is(err, store.ErrLocked), err))
			mygo.App.Quit()
			return
		}
		system := systemLocale()
		goAsync := func(work func() func()) {
			go func() {
				// A task that panics is logged with its stack and shows nothing; the app stays usable.
				defer diag.Recover("window task")
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
			Folder: func() (string, error) {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Directory: true})
				if err != nil {
					slog.Error("Cannot show the folder dialog", "error", err)
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
		files.Desktop = &settings.Desktop{OS: runtime.GOOS, Bundled: mygo.App.IsPackaged()}
		root := newShell(system, filepath.Join(dataDir, sessionFile), filepath.Join(dataDir, prefs.File),
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
		// The menu, and MyGo's own controls and menus, speak the interface language, whichever the system reads.
		showLanguage := func(language string) {
			setMenu(language)
			showMyGoLanguage(mygo.App.SetLocale, root.prefs.Language, mygo.App.PreferredLanguages())
		}
		root.language = showLanguage
		showLanguage(locale.Resolve(root.prefs.Language, system))
		made := make(chan struct{}) // closed once window is set, which the poll goroutine reads
		// The window, shown again from the tray, the Dock or a second launch.
		reveal := func() {
			window.Show()
			window.Restore()
			window.Focus()
		}
		// A hidden window never draws, so the timeline does not update the count the icon shows; this does, and only
		// while the window is hidden or minimized.
		tick := func() {
			<-made
			if window.IsVisible() && !window.IsMinimized() {
				return
			}
			window.Update(root.pollUnread)
		}
		integration := newDesktopController(nativeHost(trayActions{
			Open:    reveal,
			Refresh: func() { window.Update(root.refreshAll) },
			Quit:    mygo.App.Quit,
		}, tick), locale.Resolve(root.prefs.Language, system))
		root.startDesktop(integration)
		options := mainWindowOptions(ui.View(root.View))
		// A launch at login leaves only the icon, when there is one.
		options.Hidden = integration.HideAtLaunch(mygo.App.WasOpenedAtLogin())
		window = mygo.NewWindow(options)
		close(made)
		// Closing the window leaves Oriel running in the tray; Quit still closes it.
		var quitting atomic.Bool
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting.Store(true) })
		window.OnClose(func(e *mygo.CloseEvent) {
			if integration.HasTray() && !quitting.Load() {
				e.PreventDefault()
				window.Hide()
			}
		})
		mygo.App.OnActivate(func(visible bool) {
			if !visible {
				reveal()
			}
		})
		// The marker of what the reader last looked at moves only while the window is in front.
		focus := func(on bool) func() { return func() { window.Update(func() { root.focus(on) }) } }
		window.OnFocus(focus(true))
		window.OnBlur(focus(false))
		window.OnHide(focus(false))
		window.OnMinimize(focus(false))
		// Files dropped where no element took them are imported like the ones dropped on a drop zone.
		window.OnFileDrop(func(e *mygo.FileDropEvent) { window.Update(func() { root.dropped(e.Paths) }) })
		// A computer that wakes may have its network back: the timeline checks the server at once.
		mygo.Power.OnResume(func() { window.Update(root.wake) })
		root.restore()
	})
	mygo.App.OnSecondInstance(func([]string, string) {
		if window != nil {
			window.Show()
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
