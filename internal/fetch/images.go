package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"

	"github.com/yangflow/oriel/internal/tracker"
)

// ErrNotImage reports a response that is not a picture the interface can decode.
var ErrNotImage = errors.New("not a decodable image")

// StatusError reports an origin that answered a picture request with a status other than 2xx.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("status %d", e.Code) }

// imageTypes are the formats the interface decodes, by their sniffed content type. SVG is left out:
// it can carry script, and opened from the server it would run with the session cookie.
var imageTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true,
}

var errNotWebAddress = errors.New("not a web address")

// Images downloads pictures found inside feeds for the interface, through the guarded client.
type Images struct {
	guarded   *http.Client
	userAgent string
	cache     *imageCache
}

// NewImages returns an image fetcher that identifies itself as Oriel/version. A non-empty cacheDir keeps the
// pictures it downloads there, up to 256 MiB for 30 days; an empty one downloads every time.
func NewImages(version, cacheDir string) *Images {
	im := &Images{
		guarded:   guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) }),
		userAgent: "Oriel/" + version,
	}
	if cacheDir != "" {
		im.cache = newImageCache(cacheDir)
	}
	return im
}

// Get returns the picture at address as its sniffed content type and bytes, from the cache when it holds a
// fresh copy and from the origin otherwise. Known trackers are refused before the cache is read.
func (im *Images) Get(ctx context.Context, address string) (string, []byte, error) {
	link, err := url.Parse(address)
	if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Host == "" {
		return "", nil, fmt.Errorf("%w: %q", errNotWebAddress, address)
	}
	if tracker.KnownAddress(address) {
		return "", nil, ErrBlockedAddress
	}
	if im.cache != nil {
		if kind, body, ok := im.cache.get(address); ok {
			return kind, body, nil
		}
	}
	kind, body, err := im.download(ctx, address)
	if err == nil && im.cache != nil {
		im.cache.put(address, kind, body)
	}
	return kind, body, err
}

// CacheStats returns how many pictures the disk cache holds and the bytes they take; both are zero without a cache.
func (im *Images) CacheStats() (count int, bytes int64) {
	if im.cache == nil {
		return 0, 0
	}
	return im.cache.stats()
}

// download fetches the picture from the origin through the guarded client.
func (im *Images) download(ctx context.Context, address string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", im.userAgent)
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif,image/bmp;q=0.9,*/*;q=0.1")
	resp, err := im.guarded.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, &StatusError{Code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(body) > maxBytes {
		return "", nil, errTooLarge
	}
	kind := http.DetectContentType(body)
	if !imageTypes[kind] {
		return "", nil, fmt.Errorf("%w: %s", ErrNotImage, kind)
	}
	return kind, body, nil
}
