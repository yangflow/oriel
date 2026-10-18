package main

import (
	"context"
	"log/slog"
	"net/url"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/connect"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/timeline"
)

// sessionFile is the file in the data directory that keeps the session of a remote server; the desktop reads the
// data of this computer while there is none.
const sessionFile = "remote.json"

// home is the screen under the connect screen: the timeline of the server used.
type home interface {
	Use(api timeline.API, server string)
	View(c *ui.Context)
}

// shell is the content of the window: Oriel's palette over the connect screen or home. Disk and network work runs
// through goAsync, which runs work away from the UI thread and the function it returns on the UI thread.
type shell struct {
	path    string // of the session file
	goAsync func(work func() (apply func()))
	connect *connect.Screen
	home    home
	local   client.Session  // the server of this computer
	session *client.Session // the remote server signed in to, nil for this computer
	showing bool            // whether the connect screen shows

	writes  int        // counts changes of the session file, on the UI thread
	file    sync.Mutex // guards written and the file
	written int        // the latest change written, so an older one finishing late does not undo it
}

func newShell(systemLocale, path string, local client.Session, goAsync func(func() func())) *shell {
	s := &shell{path: path, goAsync: goAsync, local: local}
	s.home = timeline.New(systemLocale, timeline.Deps{Go: goAsync, OpenConnect: s.openConnect, Expired: s.expired})
	s.connect = connect.New(systemLocale, connect.Deps{
		Probe:     client.Probe,
		Login:     client.Login,
		Go:        goAsync,
		Connected: s.connected,
		UseLocal:  s.useLocal,
	})
	return s
}

// use shows home with the data of the remote server signed in to, or of this computer.
func (s *shell) use() {
	if s.session == nil {
		s.home.Use(s.local, "")
		return
	}
	host := s.session.Server
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Host
	}
	s.home.Use(*s.session, host)
}

// restore reads the saved session and checks it with its server, then shows the connect screen if it can no longer
// be used. Home shows the data of this computer until the session is checked.
func (s *shell) restore() {
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

// expired shows the connect screen because the remote server no longer takes the session.
func (s *shell) expired() {
	if s.session == nil {
		return
	}
	s.connect.Open(s.session.Server, client.ErrUnauthorized)
	s.showing = true
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
	theme.Apply(c)
	if s.showing {
		s.connect.View(c)
		return
	}
	s.home.View(c)
}
