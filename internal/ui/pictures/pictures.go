// Package pictures is the queue of pictures a screen downloads and keeps decoded: the reader's pictures of an article
// and the timeline's thumbnails, icons and covers. The screen asks for a picture while it draws and gets the bitmap
// once it has arrived; the download runs in the background and its result is applied back on the UI thread.
package pictures

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// MaxDownloads is how many pictures download at a time.
	MaxDownloads = 4
	// Tries is how many times one picture is downloaded, the first included.
	Tries = 3
	// Retry is how long a failed picture waits before the next draw asks for it again.
	Retry = 10 * time.Second
)

// Picture is a picture, once it loads: Bitmap, or Failed, which leaves it out until it is tried again.
type Picture struct {
	Bitmap   *ui.Bitmap
	Failed   bool
	used     int       // when it was last asked for, to drop the least recently drawn
	tries    int       // downloads that have finished, whether they worked or not
	failedAt time.Time // when the last one failed
}

// Fetcher gets the bytes of a picture; ctx cancels the request.
type Fetcher interface {
	Image(ctx context.Context, address string) ([]byte, error)
}

// Queue holds the pictures of one screen. Every method but the work Download hands to run belongs to the UI thread.
type Queue struct {
	latestFirst bool
	keep        int
	pics        map[string]*Picture
	wanted      []string
	active      int
	drawn       int
	ctx         context.Context    // the downloads share it; Stop cancels it
	stop        context.CancelFunc // cancels ctx
}

// New returns an empty queue. It downloads the picture asked for last first when latestFirst is set and in the order
// of the asking otherwise, and keeps at most keep decoded pictures (no limit for 0).
func New(latestFirst bool, keep int) *Queue {
	return &Queue{latestFirst: latestFirst, keep: keep, pics: map[string]*Picture{}}
}

// Get returns the picture at address, asking for it if it has not been, and asking again for one that failed once
// Retry has passed, up to Tries downloads in all. It marks the picture as drawn now. Now is the current time.
func (q *Queue) Get(address string, now time.Time) *Picture {
	p := q.pics[address]
	if p == nil {
		p = &Picture{}
		q.pics[address] = p
		q.wanted = append(q.wanted, address)
	} else if p.Failed && p.tries < Tries && now.Sub(p.failedAt) >= Retry {
		p.Failed = false
		q.wanted = append(q.wanted, address)
	}
	q.drawn++
	p.used = q.drawn
	return p
}

// Peek returns the picture at address, nil when it was not asked for, without asking for it.
func (q *Queue) Peek(address string) *Picture { return q.pics[address] }

// Len is how many pictures the queue holds, loaded or not.
func (q *Queue) Len() int { return len(q.pics) }

// Active is how many downloads are under way.
func (q *Queue) Active() int { return q.active }

// Stop cancels the downloads under way, as when another article opens or the server changes. Their results are not
// applied once the screen drops them with Reset or its own generation.
func (q *Queue) Stop() {
	if q.stop != nil {
		q.stop()
	}
	q.ctx, q.stop = nil, nil
}

// Reset cancels the downloads under way and forgets every picture and every request.
func (q *Queue) Reset() {
	q.Stop()
	q.pics, q.wanted, q.active = map[string]*Picture{}, nil, 0
}

// ForgetFailed drops the pictures that failed, so the next draw asks for them again from the start.
func (q *Queue) ForgetFailed() {
	for address, p := range q.pics {
		if p.Failed {
			delete(q.pics, address)
		}
	}
}

// Again drops the picture at address when it failed, so the next draw asks for it again from the start, at once and
// with all its tries; it is what a reader's own request for one picture does.
func (q *Queue) Again(address string) {
	if p := q.pics[address]; p != nil && p.Failed {
		delete(q.pics, address)
	}
}

// Download starts the pictures asked for, up to MaxDownloads at a time. Each runs through run, which does the work
// in the background and applies its result on the UI thread; alive tells, on the UI thread, whether the screen still
// wants that result, and the result of one that does not is dropped.
func (q *Queue) Download(api Fetcher, decode func([]byte) (*ui.Bitmap, error), run func(func() func()),
	now func() time.Time, alive func() bool) {
	for q.active < MaxDownloads && len(q.wanted) > 0 {
		var address string
		if q.latestFirst {
			address, q.wanted = q.wanted[len(q.wanted)-1], q.wanted[:len(q.wanted)-1]
		} else {
			address, q.wanted = q.wanted[0], q.wanted[1:]
		}
		if p := q.pics[address]; p == nil || p.Bitmap != nil || p.Failed {
			continue
		}
		q.active++
		if q.ctx == nil {
			q.ctx, q.stop = context.WithCancel(context.Background())
		}
		ctx := q.ctx
		run(func() func() {
			data, err := api.Image(ctx, address)
			var bitmap *ui.Bitmap
			if err == nil {
				bitmap, err = decode(data)
			}
			return func() {
				if !alive() {
					return
				}
				q.active--
				if p := q.pics[address]; p != nil {
					p.Bitmap, p.Failed = bitmap, err != nil
					p.tries++
					if err != nil {
						p.failedAt = now()
					}
				}
				q.forget()
			}
		})
	}
}

// forget drops the least recently drawn pictures beyond keep.
func (q *Queue) forget() {
	for q.keep > 0 && len(q.pics) > q.keep {
		oldest, at := "", 0
		for address, p := range q.pics {
			if (p.Bitmap != nil || p.Failed) && (oldest == "" || p.used < at) {
				oldest, at = address, p.used
			}
		}
		if oldest == "" {
			return
		}
		delete(q.pics, oldest)
	}
}
