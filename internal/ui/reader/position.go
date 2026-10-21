package reader

import (
	"context"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// The reading position, see docs/product.md "阅读器": where the reader stopped is kept as a fraction of the article's
// scrollable height, Y over MaxY of the scroll state.
const (
	saveAfter    = time.Second // how long the offset stays still before the position is stored
	saveStep     = 0.01        // the smallest change stored
	resumeFrom   = 0.02        // below it the article opens at the top, and 0 is stored
	finishedFrom = 0.95        // from it the article counts as read to the end and opens at the top
	resumedToast = "reader.resumed"
)

// startPosition works out, as e opens, where the article is to open: at the position stored when it lies between
// resumeFrom and finishedFrom, otherwise at the top. The position this session stored wins over the entry, which
// the list or the cache may hold as it was before.
func (s *Screen) startPosition(e client.Entry) {
	p := 0.0
	if e.ReadProgress != nil {
		p = *e.ReadProgress
	}
	if k, ok := s.known[e.ID]; ok {
		p = k
	}
	s.stored, s.resume = p, -1
	if p >= resumeFrom && p < finishedFrom {
		s.resume = p
	}
	s.moved, s.laid, s.lastY, s.bodyHow = false, 0, 0, -1
}

// track restores the position once the body is laid out and stores the offset after it stays still. It runs after
// the page is drawn, reads only what the scroll container has set, and leaves the writing to Deps.Go.
func (s *Screen) track(c *ui.Context) {
	if s.id == 0 {
		return
	}
	if s.loading || s.failed {
		s.laid = 0
		return
	}
	_, how := s.body()
	if how != s.bodyHow {
		s.bodyHow, s.laid = how, 0
	}
	// The scroll range is the one of the last layout, so it is the body's only from the second frame it was drawn.
	if s.laid < 2 {
		s.laid++
		c.Invalidate()
	}
	y := s.scroll.Y
	if y != s.lastY {
		if s.resume >= 0 {
			s.resume = -1 // the reader scrolled before the position was restored
		}
		s.lastY, s.moved, s.movedAt = y, true, s.deps.Now()
	}
	if s.resume >= 0 && s.laid >= 2 && (how == whole || how == carried) {
		if s.scroll.MaxY > 0 {
			s.scroll.Y = float32(s.resume) * s.scroll.MaxY
			s.lastY = s.scroll.Y
			s.resumed = true
			c.Invalidate()
		}
		s.resume = -1
	}
	if s.moved {
		if wait := saveAfter - s.deps.Now().Sub(s.movedAt); wait > 0 {
			c.After(wait)
		} else {
			s.save()
		}
	}
}

// Leave stores the position at once if it moved, as the article is left for another or for the list.
func (s *Screen) Leave() {
	if s.moved {
		s.save()
	}
}

// save stores the offset as a fraction when it differs from the stored one by saveStep or more.
func (s *Screen) save() {
	s.moved = false
	if s.id == 0 || s.laid < 2 || s.scroll.MaxY <= 0 {
		return
	}
	p := float64(min(max(s.scroll.Y/s.scroll.MaxY, 0), 1))
	if p < resumeFrom {
		p = 0
	}
	if math.Abs(p-s.stored) < saveStep {
		return
	}
	s.stored, s.known[s.id] = p, p
	s.send(write{s.id, p})
}

// send stores w, one write at a time so that the last one is the one the server keeps.
func (s *Screen) send(w write) {
	if s.api == nil {
		return
	}
	if s.writing {
		s.queued = &w
		return
	}
	s.writing = true
	api := s.api
	s.deps.Go(func() func() {
		_, err := api.SetEntry(context.Background(), w.id, client.EntryPatch{ReadProgress: &w.progress})
		return func() {
			s.writing = false
			s.deps.Expired(err)
			if q := s.queued; q != nil {
				s.queued = nil
				s.send(*q)
			}
		}
	})
}

// toasts shows the toast of a restored position.
func (s *Screen) toasts(c *ui.Context) {
	if s.resumed {
		s.resumed = false
		c.AddToast(ui.Toast{ID: resumedToast, Title: s.text.Resumed, Data: theme.ToastLook{Icon: icons.RotateCcw}})
	}
}
