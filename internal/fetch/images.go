package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
)

// ErrNotImage reports a response that is not a picture the interface can decode.
var ErrNotImage = errors.New("not a decodable image")

// imageTypes are the formats the interface decodes, by their sniffed content type. SVG is left out:
// it can carry script, and opened from the server it would run with the session cookie.
var imageTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true,
}

// Images downloads pictures found inside feeds for the interface, through the guarded client.
type Images struct {
	guarded   *http.Client
	userAgent string
}

// NewImages returns an image fetcher that identifies itself as Oriel/version.
func NewImages(version string) *Images {
	return &Images{
		guarded:   guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) }),
		userAgent: "Oriel/" + version,
	}
}

// Get downloads the picture at address and returns its sniffed content type and bytes.
func (im *Images) Get(ctx context.Context, address string) (string, []byte, error) {
	link, err := url.Parse(address)
	if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Host == "" {
		return "", nil, fmt.Errorf("not a web address: %q", address)
	}
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
		return "", nil, fmt.Errorf("status %d", resp.StatusCode)
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
