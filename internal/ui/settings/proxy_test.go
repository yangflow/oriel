package settings

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// openProxy opens the Fetching group, which holds the Proxy card, in a window tall enough for all of it.
func openProxy(t *testing.T, language string, saved client.Settings) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, true)
	f.settings.ProxyURL, f.settings.ProxyAuthSet = saved.ProxyURL, saved.ProxyAuthSet
	if saved.ProxyAuthSet {
		f.proxyUser, f.proxyPass = "alice", "hunter2"
	}
	tt.SetSize(1000, 1800)
	s.Open()
	f.settle(tt)
	return s, f, tt
}

// submitProxy presses Enter in the address box. The page has a Save for each card, so the text alone does not name
// the Save of the proxy.
func submitProxy(tt *ui.Tester, s *Screen) {
	s.focusProxy = true
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
}

func TestProxyShows(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		s, _, tt := openProxy(t, language, client.Settings{})
		for _, text := range []string{s.text.Proxy, s.text.ProxyAddress, s.text.ProxyAuth, s.text.KeyUnset, s.text.Save} {
			if !tt.HasText(text) {
				t.Errorf("%s: no %q in %v", language, text, tt.Texts())
			}
		}
		if tt.HasText(s.text.ProxyRemoveAuth) || tt.HasText(s.text.ProxyChanged) {
			t.Errorf("%s: a sign-in to remove or a warning with none stored: %v", language, tt.Texts())
		}
	}
}

func TestSaveProxy(t *testing.T) {
	s, f, tt := openProxy(t, "en", client.Settings{})
	if s.proxyDirty() || s.unsaved() {
		t.Fatal("a proxy not changed counts as unsaved")
	}
	edit(tt, func() { s.proxy, s.proxyUser, s.proxyPass = " socks5://127.0.0.1:1080 ", "alice", "hunter2" })
	if !s.unsaved() {
		t.Fatal("a typed proxy does not count as unsaved")
	}
	f.set = nil
	submitProxy(tt, s)
	f.settle(tt)
	if len(f.set) != 1 {
		t.Fatalf("%d saves", len(f.set))
	}
	got := f.set[0]
	if got.ProxyURL == nil || *got.ProxyURL != "socks5://127.0.0.1:1080" || got.ProxyUser == nil || *got.ProxyUser != "alice" ||
		got.ProxyPassword == nil || *got.ProxyPassword != "hunter2" {
		t.Fatalf("patch %+v", got)
	}
	if f.settings.ProxyURL != "socks5://127.0.0.1:1080" || !f.settings.ProxyAuthSet {
		t.Fatalf("saved %+v", f.settings)
	}
	if !tt.HasText(s.text.ProxySaved) || !tt.HasText(s.text.KeySet) || !tt.HasText(s.text.ProxyRemoveAuth) ||
		!slices.Contains(tt.Announcements(), s.text.ProxySaved) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if s.proxy != "socks5://127.0.0.1:1080" || s.proxyUser != "" || s.proxyPass != "" || s.proxyDirty() {
		t.Fatalf("typed %q %q %q", s.proxy, s.proxyUser, s.proxyPass)
	}
}

// TestProxyPasswordNotShown shows the password never comes back to the screen: after a save the boxes are empty and
// only say a sign-in is set, and a reload does not fill them either.
func TestProxyPasswordNotShown(t *testing.T) {
	s, f, tt := openProxy(t, "en", client.Settings{ProxyURL: "http://proxy.example:8080", ProxyAuthSet: true})
	for _, secret := range []string{"alice", "hunter2"} {
		if slices.ContainsFunc(tt.Texts(), func(text string) bool { return strings.Contains(text, secret) }) {
			t.Fatalf("%q shown in %v", secret, tt.Texts())
		}
	}
	if s.proxyUser != "" || s.proxyPass != "" || !tt.HasText(s.text.KeySet) || !tt.HasText(s.text.ProxyRemoveAuth) {
		t.Fatalf("user %q, password %q, texts %v", s.proxyUser, s.proxyPass, tt.Texts())
	}
	// Saving only the address sends neither the user name nor the password, so the server keeps what it has.
	edit(tt, func() { s.proxy = "http://proxy.example:8080" })
	f.set = nil
	edit(tt, func() { s.proxyUser = "bob" })
	submitProxy(tt, s)
	f.settle(tt)
	if len(f.set) != 1 || f.set[0].ProxyURL != nil || f.set[0].ProxyPassword != nil || *f.set[0].ProxyUser != "bob" {
		t.Fatalf("patch %+v", f.set)
	}
	if s.proxyUser != "" || f.proxyPass != "hunter2" {
		t.Fatalf("typed %q, the server keeps %q", s.proxyUser, f.proxyPass)
	}
	s.Open()
	f.settle(tt)
	if s.proxyUser != "" || s.proxyPass != "" {
		t.Fatalf("a reload filled %q %q", s.proxyUser, s.proxyPass)
	}
}

// TestProxyAddressChangeClearsSignIn shows a new address without a sign-in warns first, sends no user name or password
// and, as the server clears the stored ones, ends up with none; a new sign-in typed with it is kept.
func TestProxyAddressChangeClearsSignIn(t *testing.T) {
	s, f, tt := openProxy(t, "zh-Hans", client.Settings{ProxyURL: "http://proxy.example:8080", ProxyAuthSet: true})
	edit(tt, func() { s.proxy = "http://other.example:3128" })
	if !tt.HasText(s.text.ProxyChanged) {
		t.Fatalf("no warning in %v", tt.Texts())
	}
	f.set = nil
	submitProxy(tt, s)
	f.settle(tt)
	if len(f.set) != 1 || f.set[0].ProxyUser != nil || f.set[0].ProxyPassword != nil || *f.set[0].ProxyURL != "http://other.example:3128" {
		t.Fatalf("patch %+v", f.set)
	}
	if s.settings.ProxyAuthSet || tt.HasText(s.text.ProxyRemoveAuth) || tt.HasText(s.text.ProxyChanged) || !tt.HasText(s.text.KeyUnset) {
		t.Fatalf("sign-in kept: %v", tt.Texts())
	}

	// A sign-in typed with the new address is not dropped, and no warning shows once one is typed.
	edit(tt, func() { s.proxy, s.proxyUser, s.proxyPass = "http://third.example:3128", "carol", "pw" })
	submitProxy(tt, s)
	f.settle(tt)
	if !s.settings.ProxyAuthSet || f.proxyUser != "carol" {
		t.Fatalf("saved %+v, server %q", s.settings, f.proxyUser)
	}
}

func TestRemoveProxySignIn(t *testing.T) {
	s, f, tt := openProxy(t, "en", client.Settings{ProxyURL: "http://proxy.example:8080", ProxyAuthSet: true})
	f.set = nil
	press(t, tt, s.text.ProxyRemoveAuth)
	f.settle(tt)
	if len(f.set) != 1 || f.set[0].ProxyURL != nil || f.set[0].ProxyUser == nil || *f.set[0].ProxyUser != "" ||
		f.set[0].ProxyPassword == nil || *f.set[0].ProxyPassword != "" {
		t.Fatalf("patch %+v", f.set)
	}
	if s.settings.ProxyAuthSet || s.settings.ProxyURL != "http://proxy.example:8080" || !tt.HasText(s.text.ProxyAuthRemoved) ||
		tt.HasText(s.text.ProxyRemoveAuth) || !tt.HasText(s.text.KeyUnset) {
		t.Fatalf("settings %+v, texts %v", s.settings, tt.Texts())
	}
}

func TestInvalidProxy(t *testing.T) {
	s, f, tt := openProxy(t, "en", client.Settings{})
	f.setErr = &client.APIError{Status: 400, Code: "invalid_proxy_url"}
	edit(tt, func() { s.proxy = "127.0.0.1:7890" })
	submitProxy(tt, s)
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.InvalidProxy) || !tt.Focused(s.text.ProxyAddress) || s.proxy != "127.0.0.1:7890" ||
		!slices.Contains(tt.Announcements(), s.text.InvalidProxy) {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type("0")
	if tt.HasText(s.text.InvalidProxy) || len(s.proxy) != len("127.0.0.1:7890")+1 {
		t.Fatalf("the error stays after typing %q", s.proxy)
	}
	f.setErr = errors.New("offline")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.SaveFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.setErr = &client.APIError{Status: 400, Code: "invalid_proxy_credentials"}
	submitProxy(tt, s)
	f.settle(tt)
	if !tt.HasText(s.text.InvalidProxyAuth) {
		t.Fatalf("texts %v", tt.Texts())
	}
}
