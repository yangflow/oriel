package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/settings"
	"github.com/yangflow/oriel/internal/ui/timeline"
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
		w.Write([]byte(`{"rsshubBaseUrl":"https://rsshub.example.com","retentionDays":30}`))
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"sessions":[{"id":1,"userAgent":"Oriel/0.9.0 (macOS)","current":true}]}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// fakeHome stands in for the timeline and keeps the servers it was told to use.
type fakeHome struct {
	used      []string
	refreshed int
	keys      int
	languages []string
}

func (h *fakeHome) Use(api timeline.API, server string) {
	h.used = append(h.used, api.(client.Session).Server+" "+server)
}

func (h *fakeHome) SetLanguage(language string) { h.languages = append(h.languages, language) }

func (h *fakeHome) Refresh() { h.refreshed++ }

func (h *fakeHome) ShowKeys() { h.keys++ }

func (h *fakeHome) View(c *ui.Context) { ui.Text(c, "Home") }

func newTestShell(t *testing.T) (*shell, *ui.Tester, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, sessionFile)
	s := newShell("en", path, filepath.Join(dir, prefs.File), client.Session{Server: "http://127.0.0.1:7470"},
		func(work func() func()) { work()() }, settings.Deps{})
	s.newHome = func(string) home { return &fakeHome{} }
	s.home = &fakeHome{}
	return s, ui.NewTester(s.View, 960, 640), path
}

func used(s *shell) []string { return s.home.(*fakeHome).used }

func TestShellWithoutSession(t *testing.T) {
	s, tt, _ := newTestShell(t)
	s.restore()
	tt.Frame()
	if !tt.HasText("Home") || s.session != nil {
		t.Fatalf("texts %v", tt.Texts())
	}
	if want := []string{"http://127.0.0.1:7470 "}; !slices.Equal(used(s), want) {
		t.Fatalf("used %v", used(s))
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
	if want := []string{server.URL + " " + strings.TrimPrefix(server.URL, "http://")}; !slices.Equal(used(s), want) {
		t.Fatalf("used %v", used(s))
	}
}

func TestShellSessionExpiresInUse(t *testing.T) {
	server := remote(t)
	s, tt, path := newTestShell(t)
	client.SaveSession(path, client.Session{Server: server.URL, Token: "t1"})
	s.restore()
	tt.Frame()
	s.expired()
	tt.Frame()
	if !s.showing || !tt.HasText("Signed out of "+strings.TrimPrefix(server.URL, "http://")) {
		t.Fatalf("texts %v", tt.Texts())
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
	if want := []string{"http://127.0.0.1:7470 "}; !slices.Equal(used(s), want) {
		t.Fatalf("used %v", used(s))
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

func TestShellSettings(t *testing.T) {
	server := remote(t)
	s, tt, _ := newTestShell(t)
	s.connected(client.Session{Server: server.URL, Token: "t1"})
	s.openSettings()
	tt.Frame()
	if !tt.HasText("Keep entries") || !tt.HasText(strings.TrimPrefix(server.URL, "http://")) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Devices"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Oriel") || !tt.HasText("macOS") || !tt.HasText("This device") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if !tt.HasText("Home") || s.setting {
		t.Fatalf("texts %v", tt.Texts())
	}
	s.openConnect()
	s.openSettings()
	tt.Frame()
	if s.setting || !tt.HasText("Connect to a server") {
		t.Fatal("the settings opened over the connect screen")
	}
}

func TestShellImportSettings(t *testing.T) {
	server := remote(t)
	s, tt, _ := newTestShell(t)
	s.connected(client.Session{Server: server.URL, Token: "t1"})
	s.openImportSettings()
	tt.Frame()
	if !s.setting || !tt.HasText("Choose File…") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestShellSettingsPage(t *testing.T) {
	server := remote(t)
	s, tt, _ := newTestShell(t)
	s.connected(client.Session{Server: server.URL, Token: "t1"})
	s.openSettingsPage("devices")
	tt.Frame()
	if !s.setting || !tt.HasText("This device") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	s.openSettingsPage("nowhere")
	tt.Frame()
	if !s.setting || !tt.HasText("This device") {
		t.Fatalf("an unknown page did not keep the last one: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	s.openConnect()
	s.openSettingsPage("ai")
	tt.Frame()
	if s.setting || !tt.HasText("Connect to a server") {
		t.Fatal("the settings opened over the connect screen")
	}
}

func TestShellShowKeys(t *testing.T) {
	s, _, _ := newTestShell(t)
	home := s.home.(*fakeHome)
	s.showKeys()
	if home.keys != 1 {
		t.Fatalf("keys %d", home.keys)
	}
	s.openConnect()
	s.showKeys()
	if home.keys != 1 {
		t.Fatal("the key sheet opened over the connect screen")
	}
}

func TestShellSettingsExpire(t *testing.T) {
	server := remote(t)
	s, tt, _ := newTestShell(t)
	s.connected(client.Session{Server: server.URL, Token: "old"})
	s.openSettings()
	tt.Frame()
	if s.setting || !s.showing || !tt.HasText("Connect to a server") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestShellPreferences(t *testing.T) {
	s, tt, path := newTestShell(t)
	var languages []string
	s.language = func(l string) { languages = append(languages, l) }
	s.use()
	s.openSettings()
	s.settings.OpenAppearance()
	tt.Frame()
	if !tt.HasText("Interface language") {
		t.Fatalf("texts %v", tt.Texts())
	}
	p := s.prefs
	p.Theme, p.ReadingSize = prefs.ThemeDark, prefs.SizeLarge
	s.change(p)
	file := filepath.Join(filepath.Dir(path), prefs.File)
	if got := prefs.Load(file); got != p || s.prefs != p || len(languages) != 0 {
		t.Fatalf("saved %v, in use %v, languages %v", got, s.prefs, languages)
	}
	p.Language = prefs.LanguageChinese
	s.change(p)
	tt.Frame()
	if !s.setting || !tt.HasText("界面语言") || !slices.Equal(languages, []string{"zh-Hans"}) {
		t.Fatalf("setting %v, languages %v, texts %v", s.setting, languages, tt.Texts())
	}
	if home := s.home.(*fakeHome); len(home.used) != 1 || !slices.Equal(home.languages, []string{"zh-Hans"}) {
		t.Fatalf("home used %v, languages %v; it was made again or not told", home.used, home.languages)
	}
	if prefs.Load(file) != p {
		t.Fatal("the language was not saved")
	}
}

func TestShellPreferencesNotSaved(t *testing.T) {
	s, tt, path := newTestShell(t)
	s.prefsPath = filepath.Join(filepath.Dir(path), "missing", prefs.File)
	s.use()
	s.openSettings()
	s.settings.OpenAppearance()
	before := s.prefs
	p := before
	p.Language = prefs.LanguageChinese
	s.change(p)
	tt.Frame()
	if s.prefs != before || !tt.HasText("Interface language") || !tt.HasText("Couldn’t save that choice. It went back to what it was.") {
		t.Fatalf("in use %v, texts %v", s.prefs, tt.Texts())
	}
}

func TestShellLook(t *testing.T) {
	s, _, path := newTestShell(t)
	s.use()
	if !s.active() {
		t.Fatal("the timeline in front of the reader is not active")
	}
	s.focus(false)
	if s.active() {
		t.Fatal("a window that lost focus is active")
	}
	s.focus(true)
	s.openSettings()
	if s.active() {
		t.Fatal("the timeline under the settings is active")
	}
	m := prefs.Marker{At: 1700000000000, ID: 7}
	s.look(m)
	file := filepath.Join(filepath.Dir(path), prefs.File)
	if s.prefs.LastLooked != m || prefs.Load(file).LastLooked != m {
		t.Fatalf("in use %v, saved %v", s.prefs.LastLooked, prefs.Load(file).LastLooked)
	}
}

func TestShellLookNotSaved(t *testing.T) {
	s, tt, path := newTestShell(t)
	s.prefsPath = filepath.Join(filepath.Dir(path), "missing", prefs.File)
	s.use()
	s.look(prefs.Marker{At: 1700000000000, ID: 7})
	tt.Frame()
	if s.prefs.LastLooked.Valid() || tt.HasText("Couldn’t save that choice. It went back to what it was.") {
		t.Fatalf("in use %v, texts %v", s.prefs.LastLooked, tt.Texts())
	}
}

func TestShellFont(t *testing.T) {
	s, _, _ := newTestShell(t)
	s.prefs.Font = "Gone Sans"
	if s.font() != "Gone Sans" {
		t.Fatal("the font was dropped before the system fonts were listed")
	}
	s.fonts, s.fontsListed = []string{"Serif"}, true
	if s.font() != "" || s.prefs.Font != "Gone Sans" {
		t.Fatalf("font %q, kept %q", s.font(), s.prefs.Font)
	}
	s.prefs.Font = "Serif"
	if s.font() != "Serif" {
		t.Fatal("a listed font was dropped")
	}
}

func TestShellReadingFace(t *testing.T) {
	s, _, _ := newTestShell(t)
	s.apply(prefs.Preferences{ReadingFont: "Gone Serif"})
	if s.face != "Gone Serif" {
		t.Fatalf("face %q before the fonts are listed", s.face)
	}
	s.fonts, s.fontsListed = []string{"Inter"}, true
	s.updateFace()
	if s.face != "" || s.prefs.ReadingFont != "Gone Serif" {
		t.Fatalf("face %q, kept %q", s.face, s.prefs.ReadingFont)
	}
	s.fonts = append(s.fonts, "Gone Serif")
	s.updateFace()
	if s.face != "Gone Serif" {
		t.Fatalf("face %q once installed", s.face)
	}
	s.apply(prefs.Preferences{})
	if s.face != "" {
		t.Fatalf("face %q when following the interface", s.face)
	}
}
