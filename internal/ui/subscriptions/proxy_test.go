package subscriptions

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// editProxy opens the edit dialog of src with the Proxy group open.
func editProxy(t *testing.T, language string, src func(*client.Source)) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language)
	tt.SetSize(1000, 2800)
	source := f.sources[0]
	if src != nil {
		src(&source)
	}
	s.Edit(source)
	tt.Frame()
	f.settle(tt)
	return s, f, tt
}

func TestEditProxyChoices(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		s, _, tt := editProxy(t, language, nil)
		if !tt.HasText(s.text.Proxy) || tt.HasText(s.text.ProxyFollowHelp) {
			t.Fatalf("%s: closed group: %v", language, tt.Texts())
		}
		if err := tt.Click(s.text.Proxy); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText(s.text.ProxyMode) || !tt.HasText(s.text.ProxyFollowHelp) || tt.HasText(s.text.ProxyAddress) {
			t.Fatalf("%s: follow: %v", language, tt.Texts())
		}
		s.edit.proxyMode = proxyDirect
		tt.Frame()
		if !tt.HasText(s.text.ProxyDirectHelp) {
			t.Fatalf("%s: direct: %v", language, tt.Texts())
		}
		s.edit.proxyMode = proxyCustom
		tt.Frame()
		for _, text := range []string{s.text.ProxyAddress, s.text.ProxyUser, s.text.ProxyPassword, s.text.ProxyAuthHelp} {
			if !tt.HasText(text) {
				t.Fatalf("%s: no %q in %v", language, text, tt.Texts())
			}
		}
	}
}

func TestEditProxyUntouchedSendsNothing(t *testing.T) {
	s, f, tt := editProxy(t, "en", func(src *client.Source) { src.Proxy = "socks5://127.0.0.1:1080" })
	if !s.edit.proxyOpen || s.edit.proxyMode != proxyCustom || s.edit.proxyURL != "socks5://127.0.0.1:1080" {
		t.Fatalf("edit %+v", s.edit)
	}
	focusField(t, tt, "Title")
	tt.Type("x")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Proxy != nil || p[0].ProxyUser != nil || p[0].ProxyPassword != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestEditProxyDirectAndFollow(t *testing.T) {
	s, f, tt := editProxy(t, "en", nil)
	s.edit.proxyMode = proxyDirect
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Proxy == nil || *p[0].Proxy != "direct" {
		t.Fatalf("patches %+v", p)
	}

	// Back to following the settings sends the empty value.
	s.Edit(client.Source{ID: f.sources[0].ID, Title: "A", URL: "https://example.org/feed", Proxy: "direct"})
	tt.Frame()
	if s.edit.proxyMode != proxyDirect || !s.edit.proxyOpen {
		t.Fatalf("edit %+v", s.edit)
	}
	s.edit.proxyMode = proxyFollow
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p = f.patches[f.sources[0].ID]
	if len(p) != 2 || p[1].Proxy == nil || *p[1].Proxy != "" || p[1].ProxyUser != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestEditProxyCustom(t *testing.T) {
	s, f, tt := editProxy(t, "en", nil)
	if err := tt.Click(s.text.Proxy); err != nil {
		t.Fatal(err)
	}
	s.edit.proxyMode = proxyCustom
	tt.Frame()
	focusField(t, tt, s.text.ProxyAddress)
	tt.Type(" socks5://127.0.0.1:1080 ")
	focusField(t, tt, s.text.ProxyUser)
	tt.Type("alice")
	focusField(t, tt, s.text.ProxyPassword)
	tt.Type("hunter2")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Proxy == nil || *p[0].Proxy != "socks5://127.0.0.1:1080" || p[0].ProxyUser == nil ||
		*p[0].ProxyUser != "alice" || p[0].ProxyPassword == nil || *p[0].ProxyPassword != "hunter2" {
		t.Fatalf("patches %+v", p)
	}
}

// TestEditProxySignInNotShown shows a stored sign-in only as "Set", and that Remove sign-in is staged until Save.
func TestEditProxySignInNotShown(t *testing.T) {
	s, f, tt := editProxy(t, "en", func(src *client.Source) {
		src.Proxy, src.ProxyAuthSet = "http://proxy.example:8080", true
	})
	if s.edit.proxyUser != "" || s.edit.proxyPass != "" || !tt.HasText(s.text.ProxyRemoveAuth) {
		t.Fatalf("edit %+v, texts %v", s.edit, tt.Texts())
	}
	if err := tt.Click(s.text.ProxyRemoveAuth); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(f.patches[f.sources[0].ID]) != 0 || tt.HasText(s.text.ProxyRemoveAuth) {
		t.Fatalf("sent before Save: %+v, texts %v", f.patches, tt.Texts())
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Proxy != nil || p[0].ProxyUser == nil || *p[0].ProxyUser != "" ||
		p[0].ProxyPassword == nil || *p[0].ProxyPassword != "" {
		t.Fatalf("patches %+v", p)
	}
}

func TestEditProxyRefused(t *testing.T) {
	s, f, tt := editProxy(t, "en", nil)
	s.edit.proxyOpen, s.edit.proxyMode = true, proxyCustom
	tt.Frame()

	// No address at all is refused here, without asking the server.
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.patches[f.sources[0].ID]) != 0 || !tt.HasText(s.text.ProxyError) || !s.edit.open {
		t.Fatalf("patches %+v, texts %v", f.patches, tt.Texts())
	}

	focusField(t, tt, s.text.ProxyAddress)
	tt.Type("127.0.0.1:7890")
	f.updateErr = &client.APIError{Status: 400, Code: "invalid_proxy"}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !s.edit.open || !s.edit.proxyErr || !tt.HasText(s.text.ProxyError) || !slices.Contains(tt.Announcements(), s.text.ProxyError) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestEditProxyRoute(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 2800)
	s.Edit(f.sources[1])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText(s.text.Proxy) {
		t.Fatalf("no Proxy group on a route: %v", tt.Texts())
	}
}
