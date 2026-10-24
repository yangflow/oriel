package main

import (
	"errors"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/prefs"
)

type fakeTray struct {
	shown     []trayView
	destroyed bool
}

func (f *fakeTray) Show(v trayView) { f.shown = append(f.shown, v) }
func (f *fakeTray) Destroy()        { f.destroyed = true }

// fakeSystem stands in for the tray, Dock and login items of the system.
type fakeSystem struct {
	login     bool
	loginErr  error
	trayErr   error
	trays     []*fakeTray
	badges    []string
	polling   int // polls running
	pollStart int
}

func (f *fakeSystem) host(goos string, packaged bool) desktopHost {
	return desktopHost{OS: goos, Packaged: packaged,
		NewTray: func(trayView) (trayHandle, error) {
			if f.trayErr != nil {
				return nil, f.trayErr
			}
			t := &fakeTray{}
			f.trays = append(f.trays, t)
			return t, nil
		},
		Badge:       func(label string) { f.badges = append(f.badges, label) },
		OpenAtLogin: func() bool { return f.login },
		SetOpenAtLogin: func(on bool) error {
			if f.loginErr != nil {
				return f.loginErr
			}
			f.login = on
			return nil
		},
		Poll: func(time.Duration) func() {
			f.polling++
			f.pollStart++
			return func() { f.polling-- }
		},
	}
}

func TestLoginItem(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("linux", false), "en")
	p, failed := c.Sync(prefs.Preferences{OpenAtLogin: true})
	if !sys.login || !p.OpenAtLogin || failed != (desktopFailure{}) {
		t.Fatalf("on: login %v, prefs %v, failed %+v", sys.login, p.OpenAtLogin, failed)
	}
	if _, failed = c.Sync(prefs.Preferences{OpenAtLogin: true}); failed.Login {
		t.Error("an unchanged login item was written again")
	}
	if p, _ = c.Sync(prefs.Preferences{}); sys.login || p.OpenAtLogin {
		t.Errorf("off: login item still on")
	}

	sys.loginErr = errors.New("read-only")
	p, failed = c.Sync(prefs.Preferences{OpenAtLogin: true})
	if !failed.Login || p.OpenAtLogin || sys.login {
		t.Errorf("failure: failed %+v, prefs %v", failed, p.OpenAtLogin)
	}
}

func TestLoginItemNeedsBundle(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("darwin", false), "en")
	p, failed := c.Sync(prefs.Preferences{OpenAtLogin: true})
	if !failed.Login || p.OpenAtLogin || sys.login {
		t.Errorf("outside a bundle: failed %+v, prefs %v, login %v", failed, p.OpenAtLogin, sys.login)
	}
	c = newDesktopController(sys.host("darwin", true), "en")
	if _, failed = c.Sync(prefs.Preferences{OpenAtLogin: true}); failed.Login || !sys.login {
		t.Errorf("in a bundle: failed %+v, login %v", failed, sys.login)
	}
}

func TestStartAlignsWithTheSystem(t *testing.T) {
	sys := &fakeSystem{login: false}
	c := newDesktopController(sys.host("windows", false), "en")
	p := c.Start(prefs.Preferences{OpenAtLogin: true})
	if p.OpenAtLogin || sys.login {
		t.Error("a login item removed outside Oriel came back")
	}
	sys.login = true
	c = newDesktopController(sys.host("windows", false), "en")
	if p = c.Start(prefs.Preferences{}); !p.OpenAtLogin {
		t.Error("a login item added outside Oriel reads off")
	}
}

func TestStartRevertsATrayThatCannotShow(t *testing.T) {
	sys := &fakeSystem{trayErr: errors.New("no appindicator")}
	c := newDesktopController(sys.host("linux", false), "en")
	p := c.Start(prefs.Preferences{TrayIcon: true})
	if p.TrayIcon || c.HasTray() || sys.polling != 0 {
		t.Errorf("tray %v, has %v, polls %d", p.TrayIcon, c.HasTray(), sys.polling)
	}
}

func TestTray(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("darwin", true), "en")
	c.SetUnread(12)
	p, failed := c.Sync(prefs.Preferences{TrayIcon: true})
	if failed.Tray || !p.TrayIcon || !c.HasTray() || len(sys.trays) != 1 {
		t.Fatalf("on: failed %+v, trays %d", failed, len(sys.trays))
	}
	c.SetUnread(1500)
	shown := sys.trays[0].shown
	if got := shown[len(shown)-1]; got.Title != "999+" || got.Unread != "999+ unread" {
		t.Errorf("shown %+v", got)
	}
	c.SetLanguage("zh-Hans")
	shown = sys.trays[0].shown
	if got := shown[len(shown)-1]; got.Words.Quit != "退出" {
		t.Errorf("language: %+v", got)
	}
	c.Sync(prefs.Preferences{TrayIcon: true})
	if len(sys.trays) != 1 {
		t.Error("an unchanged tray was made again")
	}
	c.Sync(prefs.Preferences{})
	if !sys.trays[0].destroyed || c.HasTray() {
		t.Error("the tray stayed after it was turned off")
	}

	sys.trayErr = errors.New("no appindicator")
	p, failed = c.Sync(prefs.Preferences{TrayIcon: true})
	if !failed.Tray || p.TrayIcon || c.HasTray() {
		t.Errorf("failure: failed %+v, tray %v", failed, p.TrayIcon)
	}
}

func TestTrayTitleIsMacOnly(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("windows", false), "en")
	c.SetUnread(4)
	c.Sync(prefs.Preferences{TrayIcon: true})
	shown := sys.trays[0].shown
	if v := c.view(); v.Title != "" || v.Tooltip != "Oriel, 4 unread" || len(shown) == 0 {
		t.Errorf("view %+v", v)
	}
}

func TestBadge(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("darwin", true), "en")
	c.SetUnread(7)
	c.Sync(prefs.Preferences{DockBadge: true})
	c.SetUnread(2000)
	c.SetUnread(0)
	c.Sync(prefs.Preferences{})
	want := []string{"7", "999+", "", ""}
	last := sys.badges[len(sys.badges)-4:]
	for i := range want {
		if last[i] != want[i] {
			t.Fatalf("badges %q, want ...%q", sys.badges, want)
		}
	}

	other := &fakeSystem{}
	c = newDesktopController(other.host("linux", false), "en")
	c.SetUnread(7)
	c.Sync(prefs.Preferences{DockBadge: true})
	if len(other.badges) != 0 || other.polling != 0 {
		t.Errorf("a badge on another system: %q, polls %d", other.badges, other.polling)
	}
}

func TestPollRunsOnlyWhileTheCountIsShown(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("darwin", true), "en")
	c.Sync(prefs.Preferences{})
	if sys.polling != 0 {
		t.Fatal("polling with nothing to show")
	}
	c.Sync(prefs.Preferences{TrayIcon: true})
	c.Sync(prefs.Preferences{TrayIcon: true, DockBadge: true})
	if sys.polling != 1 || sys.pollStart != 1 {
		t.Fatalf("running %d, started %d", sys.polling, sys.pollStart)
	}
	c.Sync(prefs.Preferences{DockBadge: true})
	if sys.polling != 1 {
		t.Error("the badge alone stopped the poll")
	}
	c.Sync(prefs.Preferences{})
	if sys.polling != 0 {
		t.Error("the poll kept running with the tray and the badge off")
	}
}

func TestOpenedAtLoginHidesOnlyWithTheTray(t *testing.T) {
	sys := &fakeSystem{}
	c := newDesktopController(sys.host("linux", false), "en")
	if c.HideAtLaunch(true) {
		t.Error("hidden without a tray")
	}
	c.Sync(prefs.Preferences{TrayIcon: true})
	if !c.HideAtLaunch(true) || c.HideAtLaunch(false) {
		t.Error("hidden only when opened at login with the tray on")
	}
}
