package subscriptions

import (
	"slices"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Proxy row of the page: following the settings or going direct applies at once; a proxy of its own opens its
// boxes, which wait for Save proxy.
func TestPageProxyChoices(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		s, f, tt := paged(t, language, 0, nil)
		if !tt.HasText(s.text.Proxy) || !tt.HasText(s.text.ProxyFollow) || tt.HasText(s.text.ProxyAddress) {
			t.Fatalf("%s: follow: %v", language, tt.Texts())
		}
		s.pg.e.proxyMode = proxyCustom // as the select does
		tt.Frame()
		f.settle(tt)
		for _, text := range []string{s.text.ProxyAddress, s.text.ProxyUser, s.text.ProxyPassword, s.text.ProxyAuthHelp} {
			if !tt.HasText(text) {
				t.Fatalf("%s: no %q in %v", language, text, tt.Texts())
			}
		}
		if len(f.patches[f.sources[0].ID]) != 0 {
			t.Fatalf("%s: a proxy of its own was sent before its Save: %+v", language, f.patches)
		}
	}
}

// A title saved on the page of a subscription with a proxy of its own leaves the proxy alone.
func TestPageProxyUntouchedSendsNothing(t *testing.T) {
	s, f, tt := paged(t, "en", 0, func(src *client.Source) { src.Proxy = "socks5://127.0.0.1:1080" })
	if s.pg.e.proxyMode != proxyCustom || s.pg.e.proxyURL != "socks5://127.0.0.1:1080" {
		t.Fatalf("page %+v", s.pg.e)
	}
	s.pg.e.title = "Go"
	tt.Frame()
	mustClick(t, tt, s.text.SaveTitle)
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Title == nil || p[0].Proxy != nil || p[0].ProxyUser != nil || p[0].ProxyPassword != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestPageProxyDirectAndFollow(t *testing.T) {
	s, f, tt := paged(t, "en", 0, nil)
	s.pg.e.proxyMode = proxyDirect
	tt.Frame()
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Proxy == nil || *p[0].Proxy != "direct" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// Back to following the settings sends the empty value.
	s.pg.e.proxyMode = proxyFollow
	tt.Frame()
	f.settle(tt)
	p = f.patches[f.sources[0].ID]
	if len(p) != 2 || p[1].Proxy == nil || *p[1].Proxy != "" || p[1].ProxyUser != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestPageProxyCustom(t *testing.T) {
	s, f, tt := paged(t, "en", 0, nil)
	s.pg.e.proxyMode = proxyCustom
	tt.Frame()
	focusField(t, tt, s.text.ProxyAddress)
	tt.Type(" socks5://127.0.0.1:1080 ")
	focusField(t, tt, s.text.ProxyUser)
	tt.Type("alice")
	focusField(t, tt, s.text.ProxyPassword)
	tt.Type("hunter2")
	mustClick(t, tt, s.text.SaveProxy)
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].Proxy == nil || *p[0].Proxy != "socks5://127.0.0.1:1080" || p[0].ProxyUser == nil ||
		*p[0].ProxyUser != "alice" || p[0].ProxyPassword == nil || *p[0].ProxyPassword != "hunter2" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}
	if s.pg.e.proxyUser != "" || s.pg.e.proxyPass != "" {
		t.Fatalf("the sign-in stays in the boxes after its save: %+v", s.pg.e)
	}
}

// TestPageProxySignInNotShown shows a stored sign-in only as "Set", and that Remove sign-in waits for Save proxy.
func TestPageProxySignInNotShown(t *testing.T) {
	s, f, tt := paged(t, "en", 0, func(src *client.Source) {
		src.Proxy, src.ProxyAuthSet = "http://proxy.example:8080", true
	})
	if s.pg.e.proxyUser != "" || s.pg.e.proxyPass != "" || !tt.HasText(s.text.ProxyRemoveAuth) {
		t.Fatalf("page %+v, texts %v", s.pg.e, tt.Texts())
	}
	mustClick(t, tt, s.text.ProxyRemoveAuth)
	tt.Frame()
	f.settle(tt)
	if len(f.patches[f.sources[0].ID]) != 0 || tt.HasText(s.text.ProxyRemoveAuth) {
		t.Fatalf("sent before Save: %+v, texts %v", f.patches, tt.Texts())
	}
	mustClick(t, tt, s.text.SaveProxy)
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].ProxyUser == nil || *p[0].ProxyUser != "" || p[0].ProxyPassword == nil || *p[0].ProxyPassword != "" {
		t.Fatalf("patches %+v", p)
	}
}

func TestPageProxyRefused(t *testing.T) {
	s, f, tt := paged(t, "en", 0, func(src *client.Source) { src.Proxy = "direct" })
	s.pg.e.proxyMode = proxyCustom
	tt.Frame()
	f.settle(tt)

	// No address at all is refused here, without asking the server.
	mustClick(t, tt, s.text.SaveProxy)
	f.settle(tt)
	if len(f.patches[f.sources[0].ID]) != 0 || !tt.HasText(s.text.ProxyError) || !s.pg.open {
		t.Fatalf("patches %+v, texts %v", f.patches, tt.Texts())
	}

	focusField(t, tt, s.text.ProxyAddress)
	tt.Type("127.0.0.1:7890")
	f.updateErr = &client.APIError{Status: 400, Code: "invalid_proxy"}
	mustClick(t, tt, s.text.SaveProxy)
	f.settle(tt)
	if !s.pg.open || !s.pg.e.proxyErr || !tt.HasText(s.text.ProxyError) || !slices.Contains(tt.Announcements(), s.text.ProxyError) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestPageProxyRoute(t *testing.T) {
	s, _, tt := paged(t, "en", 1, nil)
	if !tt.HasText(s.text.Proxy) {
		t.Fatalf("no Proxy row on a route: %v", tt.Texts())
	}
}
