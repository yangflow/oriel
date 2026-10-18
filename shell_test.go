package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// remote answers as `oriel serve` with the password "secret", for which it hands out the token "t1".
func remote(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"app":"oriel","version":"0.8.0","auth":true,"database":"ok"}`))
	})
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"token":"t1"}`))
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func newTestShell(t *testing.T) (*shell, *ui.Tester, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), sessionFile)
	home := func(c *ui.Context) { ui.Text(c, "Home") }
	s := newShell("en", path, home, func(work func() func()) { work()() })
	return s, ui.NewTester(s.View, 960, 640), path
}

func TestShellWithoutSession(t *testing.T) {
	s, tt, _ := newTestShell(t)
	s.restore()
	tt.Frame()
	if !tt.HasText("Home") || s.session != nil {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestShellRestoresSession(t *testing.T) {
	server := remote(t)
	s, tt, path := newTestShell(t)
	client.SaveSession(path, client.Session{Server: server.URL, Token: "t1"})
	s.restore()
	tt.Frame()
	if !tt.HasText("Home") || s.session == nil || s.session.Server != server.URL {
		t.Fatalf("session %v, texts %v", s.session, tt.Texts())
	}
}

func TestShellExpiredSession(t *testing.T) {
	server := remote(t)
	s, tt, path := newTestShell(t)
	client.SaveSession(path, client.Session{Server: server.URL, Token: "old"})
	s.restore()
	tt.Frame()
	host := strings.TrimPrefix(server.URL, "http://")
	if !tt.HasText("Signed out of "+host) || !tt.Focused("Password") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type("secret")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("Home") || s.session == nil || s.session.Token != "t1" {
		t.Fatalf("session %v, texts %v", s.session, tt.Texts())
	}
	saved, ok, err := client.LoadSession(path)
	if !ok || err != nil || saved.Token != "t1" {
		t.Fatalf("saved %v, %v, %v", saved, ok, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "secret") {
		t.Fatal("the password was saved")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", info.Mode().Perm())
		}
	}
}

func TestShellUnreachableSession(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	s, tt, path := newTestShell(t)
	client.SaveSession(path, client.Session{Server: gone.URL, Token: "t1"})
	s.restore()
	tt.Frame()
	if !tt.HasText("Can’t reach this address") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("This computer"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Home") || s.session != nil {
		t.Fatalf("texts %v", tt.Texts())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the session file stays: %v", err)
	}
}

func TestShellConnectFromMenu(t *testing.T) {
	server := remote(t)
	s, tt, path := newTestShell(t)
	s.openConnect()
	tt.Frame()
	if !tt.HasText("Connect to a server") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type(server.URL)
	tt.Click("Password")
	tt.Type("secret")
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Home") || s.session == nil {
		t.Fatalf("texts %v", tt.Texts())
	}
	if _, ok, _ := client.LoadSession(path); !ok {
		t.Fatal("the session was not saved")
	}
	s.openConnect()
	tt.Frame()
	if !s.showing || !tt.Focused("Server address") {
		t.Fatal("the connect screen did not open again")
	}
}
