package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceProxyCredentials(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now()
	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/p", Kind: "article", FetchInterval: time.Hour}, "P", now)
	if err != nil {
		t.Fatal(err)
	}
	if src.Proxy != "" || src.ProxyAuthSet {
		t.Fatalf("new source: %+v", src)
	}
	str := func(v string) *string { return &v }
	secrets := func() (string, string) {
		var user, password string
		if err := s.db.QueryRowContext(ctx, "SELECT proxy_user, proxy_password FROM sources WHERE id = ?", src.ID).Scan(&user, &password); err != nil {
			t.Fatal(err)
		}
		return user, password
	}

	got, err := s.UpdateSource(ctx, src.ID, SourcePatch{Proxy: str("http://127.0.0.1:7890"), ProxyUser: str("bob"), ProxyPassword: str("pw")}, now)
	if err != nil || got.Proxy != "http://127.0.0.1:7890" || !got.ProxyAuthSet {
		t.Fatalf("set: %+v, %v", got, err)
	}
	if u, p := secrets(); u != "bob" || p != "pw" {
		t.Fatalf("stored %q %q", u, p)
	}

	// An unrelated patch and the same address keep the credentials.
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{Proxy: str("http://127.0.0.1:7890")}, now); err != nil || !got.ProxyAuthSet {
		t.Fatalf("same address: %+v, %v", got, err)
	}
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{Paused: new(bool)}, now); err != nil || !got.ProxyAuthSet {
		t.Fatalf("other field: %+v, %v", got, err)
	}

	// A new address deletes them, unless new ones come with it.
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{Proxy: str("socks5://127.0.0.1:1080")}, now); err != nil || got.ProxyAuthSet {
		t.Fatalf("new address: %+v, %v", got, err)
	}
	if u, p := secrets(); u != "" || p != "" {
		t.Fatalf("kept %q %q", u, p)
	}
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{Proxy: str("http://127.0.0.1:1"), ProxyUser: str("amy")}, now); err != nil || !got.ProxyAuthSet {
		t.Fatalf("new address with user: %+v, %v", got, err)
	}
	if u, p := secrets(); u != "amy" || p != "" {
		t.Fatalf("stored %q %q", u, p)
	}

	// Empty strings delete a credential; direct and the empty proxy clear them too.
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{ProxyUser: str("")}, now); err != nil || got.ProxyAuthSet {
		t.Fatalf("delete user: %+v, %v", got, err)
	}
	if _, err = s.UpdateSource(ctx, src.ID, SourcePatch{ProxyUser: str("amy")}, now); err != nil {
		t.Fatal(err)
	}
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{Proxy: str(ProxyDirect)}, now); err != nil || got.Proxy != ProxyDirect || got.ProxyAuthSet {
		t.Fatalf("direct: %+v, %v", got, err)
	}
	if got, err = s.UpdateSource(ctx, src.ID, SourcePatch{Proxy: str("")}, now); err != nil || got.Proxy != "" {
		t.Fatalf("follow global: %+v, %v", got, err)
	}
}
