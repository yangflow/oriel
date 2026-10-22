package fetch

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/tracker"
)

const (
	// MaxCopyImages is the most pictures copied for one starred entry; the rest stay links to the original site.
	MaxCopyImages = 30
	copyEvery     = 5 * time.Second
	// copyBatch is how many entries one round works on, each one picture after the other.
	copyBatch = 3
	// maxCopyAttempts stops automatic retries of an entry whose pictures keep failing to download.
	maxCopyAttempts = 6
)

// copyDelays is the wait after the first, second, ... failed attempt at a copy.
var copyDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// pictures is what the copier downloads pictures with; Images is the one that runs.
type pictures interface {
	Get(ctx context.Context, address string) (string, []byte, error)
}

// Copier keeps a local copy of the pictures of starred entries, in a loop of its own that never waits for the
// fetch loop or the interface. The pictures go through Images, so the guarded client still applies.
type Copier struct {
	store  *store.Store
	images pictures
	now    func() time.Time
}

// NewCopier returns a copier that stores what images downloads.
func NewCopier(st *store.Store, images *Images) *Copier {
	return &Copier{store: st, images: images, now: time.Now}
}

// Run copies until ctx is canceled, starting right away.
func (c *Copier) Run(ctx context.Context) { every(ctx, copyEvery, c.round) }

// round works on the starred entries that are due.
func (c *Copier) round(ctx context.Context) {
	now := c.now()
	waiting, err := c.store.PendingCopies(ctx, now, copyBatch)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list pending copies", "error", err)
		}
		return
	}
	for _, target := range waiting {
		c.copyEntry(ctx, target)
	}
}

// copyEntry downloads the pictures of one entry that are not copied yet. A picture that can never be copied (a
// tracker, too large, not a picture, a 404) is skipped for good; one that may work later fails the attempt, which is
// tried again after a wait, up to maxCopyAttempts times. The pictures copied so far stay.
func (c *Copier) copyEntry(ctx context.Context, target store.CopyTarget) {
	have, err := c.store.CopiedAddresses(ctx, target.ID)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot read the copies of an entry", "entry", target.ID, "error", err)
		}
		return
	}
	retry := false
	for _, address := range copyAddresses(target) {
		if have[address] {
			continue
		}
		kind, body, err := c.images.Get(ctx, address)
		if ctx.Err() != nil {
			return // shutting down: the attempt does not count
		}
		if err != nil {
			slog.Info("Cannot copy a picture", "entry", target.ID, "error", err)
			retry = retry || temporaryPictureError(err)
			continue
		}
		if err := c.store.SaveImageCopy(ctx, target.ID, address, kind, body, c.now()); err != nil {
			slog.Error("Cannot save a copy", "entry", target.ID, "error", err)
			retry = true
		}
	}
	var next time.Time
	if attempt := target.Attempts + 1; retry && attempt < maxCopyAttempts {
		next = c.now().Add(copyDelays[min(attempt, len(copyDelays))-1])
	}
	if err := c.store.FinishCopy(ctx, target.ID, !retry, next); err != nil && ctx.Err() == nil {
		slog.Error("Cannot save the state of a copy", "entry", target.ID, "error", err)
	}
}

// temporaryPictureError reports whether a picture that failed to download may download later.
func temporaryPictureError(err error) bool {
	var status *StatusError
	switch {
	case errors.Is(err, ErrBlockedAddress), errors.Is(err, ErrNotImage), errors.Is(err, errTooLarge), errors.Is(err, errNotWebAddress):
		return false
	case errors.As(err, &status):
		return temporaryStatus(status.Code)
	}
	return true
}

// copyAddresses lists the pictures of an entry worth keeping, in the order the reader meets them, at most
// MaxCopyImages: its thumbnail, the pictures the feed named, and those in the full text and in the feed's own
// content. Known trackers, pictures of a pixel or two and hidden ones are left out, as the reader leaves them out.
func copyAddresses(t store.CopyTarget) []string {
	var base *url.URL
	if u, err := url.Parse(t.URL); err == nil {
		base = u
	}
	var found []string
	seen := map[string]bool{}
	add := func(raw string) {
		address := resolveAddress(base, raw)
		if address == "" || seen[address] || len(found) >= MaxCopyImages || tracker.KnownAddress(address) {
			return
		}
		seen[address] = true
		found = append(found, address)
	}
	add(t.ThumbnailURL)
	for _, image := range t.Images {
		if image.Width > 0 && image.Width <= 2 || image.Height > 0 && image.Height <= 2 {
			continue
		}
		add(image.URL)
	}
	for _, content := range []string{t.ExtractedHTML, t.ContentHTML} {
		walk(content, func(n *html.Node) {
			if n.DataAtom != atom.Img {
				return
			}
			src := attr(n, "src")
			if src == "" || strings.HasPrefix(src, "data:") {
				src = attr(n, "data-src")
			}
			if address := resolveAddress(base, src); address != "" && !tracker.Counts(n, address) {
				add(address)
			}
		})
	}
	return found
}

// resolveAddress makes raw an absolute http or https address against base; anything else is "".
func resolveAddress(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if scheme := strings.ToLower(u.Scheme); (scheme == "http" || scheme == "https") && u.Host != "" {
		return u.String()
	}
	return ""
}
