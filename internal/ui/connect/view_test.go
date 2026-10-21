package connect

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/locale"
)

// fake stands in for the server and the background: work waits in a queue until step runs it.
type fake struct {
	queue     []func() func()
	probed    []string
	passwords []string
	probeErr  error
	loginErr  error
	connected []client.Session
	local     int
}

func (f *fake) deps() Deps {
	return Deps{
		Probe: func(_ context.Context, address string) (client.Server, error) {
			f.probed = append(f.probed, address)
			return client.Server{URL: "http://" + address, Auth: true}, f.probeErr
		},
		Login: func(_ context.Context, server client.Server, password string) (client.Session, error) {
			f.passwords = append(f.passwords, password)
			if f.loginErr != nil {
				return client.Session{}, f.loginErr
			}
			return client.Session{Server: server.URL, Token: "t1"}, nil
		},
		Go:        func(work func() func()) { f.queue = append(f.queue, work) },
		Connected: func(s client.Session) { f.connected = append(f.connected, s) },
		UseLocal:  func() { f.local++ },
	}
}

// step runs the oldest queued work and applies its result, as the window does on the UI thread.
func (f *fake) step(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if len(f.queue) == 0 {
		t.Fatal("no work queued")
	}
	work := f.queue[0]
	f.queue = f.queue[1:]
	work()()
	tt.Frame()
}

func setup(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{}
	s := New(language, f.deps())
	tt := ui.NewTester(s.View, 960, 640)
	return s, f, tt
}

func TestSignIn(t *testing.T) {
	s, f, tt := setup(t, "en")
	if !tt.Focused("Server address") {
		t.Fatal("the address field does not have the focus")
	}
	tt.Type("home.lan")
	if err := tt.Click("Password"); err != nil {
		t.Fatal(err)
	}
	tt.Type("secret")
	tt.Key(0, ui.KeyEnter)
	if texts := tt.Texts(); !slices.Contains(texts, "Connecting") || slices.Contains(texts, "Connect") {
		t.Fatalf("not busy: %v", tt.Texts())
	}
	f.step(t, tt)
	if _, ok := tt.Find("Server found"); !ok {
		t.Fatal("the server found is not shown")
	}
	f.step(t, tt)
	want := []client.Session{{Server: "http://home.lan", Token: "t1"}}
	if !reflect.DeepEqual(f.connected, want) || !slices.Equal(f.probed, []string{"home.lan"}) {
		t.Fatalf("connected %v, probed %v", f.connected, f.probed)
	}
	if !slices.Equal(f.passwords, []string{"secret"}) {
		t.Fatalf("passwords sent: %d", len(f.passwords))
	}
	if s.password != "" {
		t.Fatal("the password stays in memory after signing in")
	}
	if !slices.Contains(tt.Texts(), "Connect") {
		t.Fatalf("still busy: %v", tt.Texts())
	}
}

func TestShowPassword(t *testing.T) {
	s, _, tt := setup(t, "en")
	if err := tt.Click("Show password"); err != nil {
		t.Fatal(err)
	}
	if !s.shown || !tt.HasText("Show password") || tt.HasText("Hide password") {
		t.Fatalf("shown %v, texts %v", s.shown, tt.Texts())
	}
	if err := tt.Click("Show password"); err != nil {
		t.Fatal(err)
	}
	if s.shown || !tt.HasText("Show password") || tt.HasText("Hide password") {
		t.Fatalf("shown %v, texts %v", s.shown, tt.Texts())
	}
}

func TestEmptyAddressDoesNothing(t *testing.T) {
	_, f, tt := setup(t, "en")
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	if len(f.queue) != 0 || !tt.Focused("Server address") {
		t.Fatalf("queued %d, address focused %v", len(f.queue), tt.Focused("Server address"))
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct {
		probe, login error
		message      string
		focus        string
	}{
		{client.ErrUnreachable, nil, "Can’t reach this address", "Server address"},
		{client.ErrNotOriel, nil, "Not an Oriel server", "Server address"},
		{client.ErrUnwritable, nil, "The server can’t open its data", "Server address"},
		{nil, client.ErrPasswordRequired, "Password required", "Password"},
		{nil, client.ErrWrongPassword, "Wrong password", "Password"},
		{nil, client.ErrTooManyAttempts, "Too many attempts. Try again in a moment", "Password"},
		{nil, errors.New("login: status 500"), "Couldn’t sign in", "Server address"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			s, f, tt := setup(t, "en")
			f.probeErr, f.loginErr = tc.probe, tc.login
			s.address = "home.lan"
			tt.Frame()
			if err := tt.Click("Connect"); err != nil {
				t.Fatal(err)
			}
			for len(f.queue) > 0 {
				f.step(t, tt)
			}
			if !tt.HasText(tc.message) || !tt.Focused(tc.focus) {
				t.Fatalf("texts %v, %s focused %v", tt.Texts(), tc.focus, tt.Focused(tc.focus))
			}
			if a := tt.Announcements(); len(a) == 0 || a[len(a)-1] != tc.message {
				t.Fatalf("announcements %v", a)
			}
			if len(f.connected) != 0 {
				t.Fatal("connected after an error")
			}
			// Editing the field the error is about clears it.
			if tc.focus == "Password" || tc.message != "The server can’t open its data" && tc.message != "Couldn’t sign in" {
				tt.Type("x")
				if tt.HasText(tc.message) {
					t.Fatal("the error stays after editing")
				}
			}
		})
	}
}

func TestExpired(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	s.Open("https://home.example:7470", client.ErrUnauthorized)
	tt.Frame()
	if s.address != "https://home.example:7470" || !tt.Focused("口令") {
		t.Fatalf("address %q, password focused %v", s.address, tt.Focused("口令"))
	}
	if !tt.HasText("home.example:7470 的登录已失效") {
		t.Fatalf("the expired server is not named: %v", tt.Texts())
	}
	tt.Type("secret")
	tt.Key(0, ui.KeyEnter)
	f.step(t, tt)
	f.step(t, tt)
	if len(f.connected) != 1 || s.expired != "" {
		t.Fatalf("connected %v, expired %q", f.connected, s.expired)
	}
}

func TestOpenWithError(t *testing.T) {
	s, _, tt := setup(t, "en")
	s.Open("http://home.lan:7470", client.ErrUnreachable)
	tt.Frame()
	if !tt.HasText("Can’t reach this address") || !tt.Focused("Server address") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestOpenDropsConnectionUnderWay(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.address, s.password = "home.lan", "secret"
	tt.Frame()
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	s.Open("", nil)
	tt.Frame()
	f.step(t, tt)
	f.step(t, tt)
	if len(f.connected) != 0 || s.phase != idle {
		t.Fatalf("connected %v, phase %v", f.connected, s.phase)
	}
}

func TestUseLocal(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("This computer"); err != nil {
		t.Fatal(err)
	}
	if f.local != 1 {
		t.Fatal("use local was not called")
	}
	s.address = "home.lan"
	tt.Frame()
	tt.Click("Connect")
	tt.Click("This computer")
	if f.local != 1 {
		t.Fatal("use local while connecting")
	}
}

func TestLanguages(t *testing.T) {
	en := reflect.ValueOf(translations["en"])
	for _, language := range locale.Languages {
		t.Run(language, func(t *testing.T) {
			v := reflect.ValueOf(translations[language])
			for i := range v.NumField() {
				name, value := v.Type().Field(i).Name, v.Field(i).String()
				if value == "" {
					t.Errorf("%s is empty", name)
				}
				if language != "en" && value == en.Field(i).String() {
					t.Errorf("%s is not translated", name)
				}
			}
			if MenuLabel(language) != translations[language].Menu {
				t.Error("menu label")
			}
			_, _, tt := setup(t, language)
			if !tt.HasText(translations[language].Title) {
				t.Fatalf("texts %v", tt.Texts())
			}
		})
	}
}

func TestAppearance(t *testing.T) {
	_, _, tt := setup(t, "en")
	tt.SetDark(true)
	tt.SetPreferences(ui.Preferences{HighContrast: true, TextScale: 2})
	tt.SetSize(640, 480)
	if !tt.HasText("Connect to a server") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Connect"); err != nil {
		t.Fatalf("connect is out of reach at the minimum size: %v", err)
	}
}
