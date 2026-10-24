package main

import (
	_ "embed"
	"errors"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/tray"
)

// The icons of the menu bar and the tray: a template of black and transparent pixels that macOS tints (made from
// icon.png, at 32x32), and the colored icon the other systems show.
var (
	//go:embed resources/trayTemplate@2x.png
	trayTemplate []byte
	//go:embed resources/trayIcon.png
	trayColor []byte
)

// unreadEvery is how often a hidden window asks the server for the unread count; a window that shows keeps it fresh
// itself, and a hidden one never draws, so the timeline's own poll does not run.
const unreadEvery = time.Minute

var errNeedsBundle = errors.New("starting at login needs the installed app")

// trayView is what the tray icon shows: the unread count for the title of the macOS menu bar, the hover text and
// the menu.
type trayView struct {
	Title   string
	Tooltip string
	Unread  string
	Words   tray.Words
}

// trayHandle is a tray icon that is on screen.
type trayHandle interface {
	Show(v trayView)
	Destroy()
}

// desktopHost is what the system gives the desktop integration; main wires MyGo in and the tests stand fakes in, since
// the tray, the Dock and the login items need the native event loop.
type desktopHost struct {
	OS             string // runtime.GOOS
	Packaged       bool   // whether Oriel runs from an app bundle (macOS)
	NewTray        func(v trayView) (trayHandle, error)
	Badge          func(label string) // the Dock badge, "" clears it (macOS)
	OpenAtLogin    func() bool
	SetOpenAtLogin func(on bool) error
	Poll           func(every time.Duration) (stop func()) // calls the unread poll on the UI thread, now and then
}

// desktopFailure says which feature could not be turned on.
type desktopFailure struct{ Login, Tray bool }

// desktopController keeps the login item, the tray icon and the Dock badge as the preferences ask, and the unread
// count they show. It runs on the UI thread.
type desktopController struct {
	host     desktopHost
	language string
	unread   int

	login    bool // the login item is on
	tray     trayHandle
	hasTray  atomic.Bool // tray != nil, for the close of the window, which is told on another thread
	badge    bool
	stopPoll func()
}

func newDesktopController(host desktopHost, language string) *desktopController {
	return &desktopController{host: host, language: language}
}

// Start makes the system match the saved preferences, once at launch, and returns the preferences as the system now
// has them: a login item that was removed outside Oriel, or a tray the system cannot show, reads off. Those changes
// stay in memory; the saved file changes only with the next choice.
func (c *desktopController) Start(p prefs.Preferences) prefs.Preferences {
	c.login = c.host.OpenAtLogin()
	p.OpenAtLogin = c.login
	p, _ = c.Sync(p)
	return p
}

// Sync makes the login item, the tray and the badge what p asks. A feature that could not change is given back off
// (or as it was) in the returned preferences, and named in failed.
func (c *desktopController) Sync(p prefs.Preferences) (prefs.Preferences, desktopFailure) {
	var failed desktopFailure
	if p.OpenAtLogin != c.login {
		var err error
		if c.host.OS == "darwin" && !c.host.Packaged {
			err = errNeedsBundle
		} else {
			err = c.host.SetOpenAtLogin(p.OpenAtLogin)
		}
		if err != nil {
			slog.Error("Cannot change opening at login", "error", err)
			failed.Login = true
			p.OpenAtLogin = c.login
		} else {
			c.login = p.OpenAtLogin
		}
	}
	switch {
	case p.TrayIcon && c.tray == nil:
		handle, err := c.host.NewTray(c.view())
		if err != nil {
			slog.Error("Cannot show the tray icon", "error", err)
			failed.Tray = true
			p.TrayIcon = false
		} else {
			c.tray = handle
		}
	case !p.TrayIcon && c.tray != nil:
		c.tray.Destroy()
		c.tray = nil
	}
	c.hasTray.Store(c.tray != nil)
	c.badge = p.DockBadge && c.host.OS == "darwin"
	c.show()
	c.poll()
	return p, failed
}

// HasTray tells whether the tray icon is on screen, which decides that closing the window only hides it.
func (c *desktopController) HasTray() bool { return c.hasTray.Load() }

// HideAtLaunch tells whether the window starts hidden: a launch at login leaves only the icon, if there is one.
func (c *desktopController) HideAtLaunch(openedAtLogin bool) bool {
	return openedAtLogin && c.tray != nil
}

// SetUnread is told the unread count whenever it changes.
func (c *desktopController) SetUnread(n int) {
	if n == c.unread {
		return
	}
	c.unread = n
	c.show()
}

// SetLanguage changes the words of the tray.
func (c *desktopController) SetLanguage(language string) {
	c.language = language
	c.show()
}

func (c *desktopController) view() trayView {
	count := ""
	if c.host.OS == "darwin" {
		count = tray.Count(c.unread)
	}
	return trayView{Title: count, Tooltip: tray.Tooltip(c.language, c.unread), Unread: tray.Unread(c.language, c.unread),
		Words: tray.Menu(c.language)}
}

// show puts the unread count on the tray and the badge.
func (c *desktopController) show() {
	if c.tray != nil {
		c.tray.Show(c.view())
	}
	if c.host.OS == "darwin" && c.host.Badge != nil {
		label := ""
		if c.badge {
			label = tray.Count(c.unread)
		}
		c.host.Badge(label)
	}
}

// poll runs while the tray or the badge shows the count, which a hidden window does not keep up to date.
func (c *desktopController) poll() {
	want := c.tray != nil || c.badge
	switch {
	case want && c.stopPoll == nil:
		c.stopPoll = c.host.Poll(unreadEvery)
	case !want && c.stopPoll != nil:
		c.stopPoll()
		c.stopPoll = nil
	}
}

// trayActions are what the tray menu does; each runs on the UI thread of the window or on the main thread.
type trayActions struct{ Open, Refresh, Quit func() }

// nativeHost is the desktop integration on MyGo: the tray, the Dock badge and the login item of this system. tick is
// the unread poll, which runs every unreadEvery on a goroutine of its own and must hand its work to the UI thread.
func nativeHost(actions trayActions, tick func()) desktopHost {
	return desktopHost{
		OS:       runtime.GOOS,
		Packaged: mygo.App.IsPackaged(),
		NewTray: func(v trayView) (trayHandle, error) {
			icon, template := trayColor, false
			if runtime.GOOS == "darwin" {
				icon, template = trayTemplate, true
			}
			t, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: template, Title: v.Title,
				ToolTip: v.Tooltip, Menu: trayMenu(v, actions)})
			if err != nil {
				return nil, err
			}
			t.OnClick(actions.Open)
			return nativeTray{t, actions}, nil
		},
		Badge:          mygo.App.Dock.SetBadge,
		OpenAtLogin:    mygo.App.OpenAtLogin,
		SetOpenAtLogin: mygo.App.SetOpenAtLogin,
		Poll: func(every time.Duration) func() {
			done := make(chan struct{})
			go func() {
				ticker := time.NewTicker(every)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						tick()
					}
				}
			}()
			return func() { close(done) }
		},
	}
}

type nativeTray struct {
	*mygo.Tray
	actions trayActions
}

func (t nativeTray) Show(v trayView) {
	t.SetTitle(v.Title)
	t.SetToolTip(v.Tooltip)
	t.SetMenu(trayMenu(v, t.actions))
}

// trayMenu is the menu of the icon: the unread count, then open, refresh and quit.
func trayMenu(v trayView, actions trayActions) *mygo.Menu {
	click := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { fn() }
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: v.Unread, Disabled: true}, mygo.Separator(),
		{Label: v.Words.Open, Click: click(actions.Open)},
		{Label: v.Words.Refresh, Click: click(actions.Refresh)},
		mygo.Separator(),
		{Label: v.Words.Quit, Click: click(actions.Quit)},
	})
}
