package main

import (
	"context"
	"log/slog"
	"net/url"
	"runtime"
	"slices"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/connect"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/settings"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/timeline"
)

// sessionFile is the file in the data directory that keeps the session of a remote server; the desktop reads the
// data of this computer while there is none.
const sessionFile = "remote.json"

// home is the screen under the connect screen: the timeline of the server used.
type home interface {
	Use(api timeline.API, server string)
	SetLanguage(language string)
	Refresh()
	ShowKeys()
	View(c *ui.Context)
}

// shell is the content of the window: Oriel's palette over the connect screen, the settings or home. Disk and
// network work runs through goAsync, which runs work away from the UI thread and the function it returns on the UI
// thread.
type shell struct {
	path         string // of the session file
	prefsPath    string // of the preferences file
	systemLocale string
	files        settings.Deps
	goAsync      func(work func() (apply func()))
	newHome      func(language string) home // makes the timeline; tests put a stand-in
	language     func(language string)      // told the interface language in use, at first and when it changes
	connect      *connect.Screen
	settings     *settings.Screen
	home         home
	local        client.Session  // the server of this computer
	session      *client.Session // the remote server signed in to, nil for this computer
	showing      bool            // whether the connect screen shows
	setting      bool            // whether the settings show, under the connect screen
	prefs        prefs.Preferences
	unfocused    bool     // the window is not the one in front; see focus
	fonts        []string // the font families of the system, empty until they are listed
	fontsListed  bool
	face         string // the font list of an article's body, "" for the interface font; see updateFace

	writes  int        // counts changes of the session file, on the UI thread
	file    sync.Mutex // guards written and the file
	written int        // the latest change written, so an older one finishing late does not undo it

	changes   int        // counts changes of the preferences, on the UI thread
	prefsFile sync.Mutex // guards saved and the preferences file
	saved     int        // the latest change saved
}

// newShell makes the content of the window; files has the dialogs that choose the OPML files of the settings. The
// preferences are read from prefsPath here, a small file read once before the window shows.
func newShell(systemLocale, path, prefsPath string, local client.Session, goAsync func(func() func()),
	files settings.Deps) *shell {
	s := &shell{path: path, prefsPath: prefsPath, systemLocale: systemLocale, files: files, goAsync: goAsync,
		local: local, prefs: prefs.Load(prefsPath)}
	s.newHome = func(language string) home {
		return timeline.New(language, timeline.Deps{Go: goAsync, OpenConnect: s.openConnect,
			OpenSettings: s.openSettings, OpenAISettings: s.openAISettings, OpenImport: s.openImportSettings,
			OpenSettingsPage: s.openSettingsPage,
			Expired:          s.expired,
			Reading:          func() prefs.Preferences { return s.prefs }, ReadingFont: func() string { return s.face },
			Change: s.change, Save: s.files.Save, Active: s.active, Look: s.look})
	}
	s.updateFace()
	s.build()
	return s
}

// build makes the screens in the interface language the preferences and the system give.
func (s *shell) build() {
	language := locale.Resolve(s.prefs.Language, s.systemLocale)
	s.home = s.newHome(language)
	s.settings = settings.New(language, settings.Deps{
		Go:          s.goAsync,
		Open:        s.files.Open,
		Save:        s.files.Save,
		Back:        func() { s.setting = false },
		Imported:    func() { s.home.Refresh() },
		Expired:     s.expired,
		Preferences: func() prefs.Preferences { return s.prefs },
		Fonts:       func() []string { return s.fonts },
		ReadingFont: func() string { return s.face },
		Change:      s.change,
	})
	s.connect = connect.New(language, connect.Deps{
		Probe:     client.Probe,
		Login:     client.Login,
		Go:        s.goAsync,
		Connected: s.connected,
		UseLocal:  s.useLocal,
	})
	if s.language != nil {
		s.language(language)
	}
}

// use shows home with the data of the remote server signed in to, or of this computer.
func (s *shell) use() {
	s.setting = false
	s.attach()
}

// attach gives the screens the server in use.
func (s *shell) attach() {
	if s.session == nil {
		s.home.Use(s.local, "")
		s.settings.Use(s.local, "", s.local.Token != "")
		return
	}
	host := s.session.Server
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Host
	}
	s.home.Use(*s.session, host)
	s.settings.Use(*s.session, host, s.session.Token != "")
}

// change applies the preferences the settings chose at once and saves them in the background; when the latest
// change cannot be saved, the preferences go back to what they were and the settings say so.
func (s *shell) change(p prefs.Preferences) { s.save(p, true) }

// look keeps where the reader last looked in the timeline. The reader did not choose it, so a failure to save it goes
// to the log and nowhere else.
func (s *shell) look(m prefs.Marker) {
	p := s.prefs
	p.LastLooked = m
	s.save(p, false)
}

// focus tells the shell whether the window is the one in front; the marker of what the reader last looked at moves
// only while it is. It runs on the UI thread.
func (s *shell) focus(on bool) { s.unfocused = !on }

// active tells whether the timeline is what the reader has in front of them.
func (s *shell) active() bool { return !s.unfocused && !s.showing && !s.setting }

// save applies p and saves it; tell says whether a failure is shown in the settings.
func (s *shell) save(p prefs.Preferences, tell bool) {
	p = p.Normalize()
	before := s.prefs
	if p == before {
		return
	}
	s.apply(p)
	s.changes++
	n := s.changes
	s.goAsync(func() func() {
		s.prefsFile.Lock()
		defer s.prefsFile.Unlock()
		if n < s.saved {
			return func() {}
		}
		s.saved = n
		err := prefs.Save(s.prefsPath, p)
		return func() {
			if err == nil {
				return
			}
			slog.Error("Cannot save the preferences", "error", err)
			if n == s.changes { // a later change decides for itself
				s.apply(before)
				if tell {
					s.settings.PreferenceFailed()
				}
			}
		}
	})
}

// apply makes p the preferences in use; a new interface language changes the words of the screens in place, so
// each keeps what it shows.
func (s *shell) apply(p prefs.Preferences) {
	language := locale.Resolve(p.Language, s.systemLocale)
	changed := language != locale.Resolve(s.prefs.Language, s.systemLocale)
	s.prefs = p
	s.updateFace()
	if !changed {
		return
	}
	s.home.SetLanguage(language)
	s.settings.SetLanguage(language)
	s.connect.SetLanguage(language)
	if s.language != nil {
		s.language(language)
	}
}

// listFonts lists the font families of the system in the background; the first listing takes a while.
func (s *shell) listFonts() {
	s.goAsync(func() func() {
		names := []string{} // empty, not nil: nil means not listed yet
		for _, f := range ui.FontFamilies() {
			names = append(names, f.Name)
		}
		return func() {
			s.fonts, s.fontsListed = names, true
			s.updateFace()
		}
	})
}

// updateFace works out the font of an article's body again. It reads the font list, so it runs when the preferences or
// the list change and not for each frame: the screens only read the result.
func (s *shell) updateFace() {
	s.face = s.prefs.ReadingFace(runtime.GOOS, s.fonts, s.font())
}

// font is the family to draw with: the one chosen, unless the system no longer has it.
func (s *shell) font() string {
	if s.fontsListed && !slices.Contains(s.fonts, s.prefs.Font) {
		return ""
	}
	return s.prefs.Font
}

// restore reads the saved session and checks it with its server, then shows the connect screen if it can no longer
// be used. Home shows the data of this computer until the session is checked.
func (s *shell) restore() {
	s.listFonts()
	s.goAsync(func() func() {
		session, ok, err := client.LoadSession(s.path)
		if err != nil {
			slog.Warn("Cannot read the remote session", "error", err)
		}
		if !ok {
			return func() {
				if s.writes == 0 {
					s.use()
				}
			}
		}
		err = client.Check(context.Background(), session)
		return func() {
			if s.writes > 0 {
				return // the user connected or chose this computer meanwhile
			}
			if err == nil {
				s.session = &session
				s.use()
				return
			}
			slog.Warn("Remote session not usable", "server", session.Server, "error", err)
			s.connect.Open(session.Server, err)
			s.showing = true
		}
	})
}

// openConnect shows the connect screen, from the menu.
func (s *shell) openConnect() {
	server := ""
	if s.session != nil {
		server = s.session.Server
	}
	s.connect.Open(server, nil)
	s.showing = true
}

// showKeys shows the key sheet, from the menu, when the timeline shows.
func (s *shell) showKeys() {
	if s.showing || s.setting {
		return
	}
	s.home.ShowKeys()
}

// openSettings shows the settings, from the menu or the timeline, unless the connect screen shows.
func (s *shell) openSettings() {
	if s.showing || s.setting {
		return
	}
	s.setting = true
	s.settings.Open()
}

// openAISettings shows the settings on the AI group, from an entry point of summaries and translations.
func (s *shell) openAISettings() {
	if s.showing || s.setting {
		return
	}
	s.setting = true
	s.settings.OpenAI()
}

// openImportSettings shows the settings on the import and export group, from the empty timeline of a first run.
func (s *shell) openImportSettings() {
	if s.showing || s.setting {
		return
	}
	s.setting = true
	s.settings.OpenImport()
}

// openSettingsPage shows the settings on the group called page, from the command palette of the timeline.
func (s *shell) openSettingsPage(page string) {
	if s.showing || s.setting {
		return
	}
	s.setting = true
	s.settings.OpenPage(page)
}

// expired shows the connect screen because the remote server no longer takes the session.
func (s *shell) expired() {
	if s.session == nil {
		return
	}
	s.connect.Open(s.session.Server, client.ErrUnauthorized)
	s.showing, s.setting = true, false
}

func (s *shell) connected(session client.Session) {
	s.session, s.showing = &session, false
	s.write(&session)
	s.use()
}

func (s *shell) useLocal() {
	s.session, s.showing = nil, false
	s.write(nil)
	s.use()
}

// write saves the session, or removes the file for none, in the background.
func (s *shell) write(session *client.Session) {
	s.writes++
	n := s.writes
	s.goAsync(func() func() {
		s.file.Lock()
		defer s.file.Unlock()
		if n < s.written {
			return func() {}
		}
		s.written = n
		if session == nil {
			if err := client.ForgetSession(s.path); err != nil {
				slog.Error("Cannot remove the remote session", "error", err)
			}
		} else if err := client.SaveSession(s.path, *session); err != nil {
			slog.Error("Cannot save the remote session", "error", err)
		}
		return func() {}
	})
}

func (s *shell) View(c *ui.Context) {
	p := s.prefs
	p.Font = s.font()
	theme.Apply(c, p)
	switch {
	case s.showing:
		s.connect.View(c)
	case s.setting:
		s.settings.View(c)
	default:
		s.home.View(c)
	}
	theme.Toasts(c)
}
