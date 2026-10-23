package timeline

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
)

// The settings "Mark as read" decides when the timeline marks an entry as read (docs/product.md, "何时标为已读"):
// when it opens, after the reader stayed on it for a while, once it scrolled off the top of the list, or never.

const (
	// stayStep is the longest wait between two frames while the stay is counted, and twice it the most one frame
	// adds, so a stretch in which no frame was drawn (the window lost the focus unseen) counts for little.
	stayStep = time.Second
	// passQuiet is how long the list must rest before the entries scrolled past are marked.
	passQuiet = 500 * time.Millisecond
	// passWorkers is the most changes sent at once when the entries scrolled past are marked.
	passWorkers = 4
)

// stay counts how long the reader has had one entry open, for "After a short stay".
type stay struct {
	id    int64         // the entry on show, 0 for none
	last  time.Time     // the frame that was counted last, zero while the window has no focus
	spent time.Duration // the time counted
	done  bool          // the entry is not to be marked by the stay any more: it was, or was read, or the reader marked it
}

// passSig says where the list stands, to find out when it moved.
type passSig struct {
	first, n int
	sum      float32
}

// passing follows the entries scrolled off the top of the list, for "After scrolling past".
type passing struct {
	seen    map[int64]bool // entries that were in view since the list was last put in place
	pending []int64        // entries that went past and are not marked yet
	sig     passSig        // where the list stood in the last frame that was looked at
	moved   time.Time      // when it last moved
	skip    int            // frames to leave out: the rects of the last two frames are of another list or place
}

// rebase forgets what was in view, after the list was put somewhere by the program and not by the reader's scrolling:
// what moved out of view then did not go past.
func (p *passing) rebase() {
	clear(p.seen)
	p.sig = passSig{}
	p.skip = 2 // a list put in place shows there in the rects of the second frame after
}

// markWhen is the mode "Mark as read" has chosen and, for the stay, how long it lasts.
func (s *Screen) markWhen() (string, time.Duration) {
	if s.deps.Reading == nil {
		return prefs.MarkOnOpen, 0
	}
	return s.deps.Reading().MarkRead()
}

// markOpened marks the entry e that was just opened, as the mode wants. inApp tells that it is shown in the app (the
// reader or the lightbox), where a stay can be measured; what the browser opens is marked at once also then.
func (s *Screen) markOpened(e client.Entry, inApp bool) {
	switch when, _ := s.markWhen(); {
	case when == prefs.MarkOnOpen, when == prefs.MarkAfterStay && !inApp:
		s.markRead(e)
	}
}

// markTick runs before each frame is drawn.
func (s *Screen) markTick(c *ui.Context, now time.Time) {
	when, wait := s.markWhen()
	if when == prefs.MarkAfterStay {
		s.stayTick(c, now, wait)
	} else {
		s.stay = stay{}
	}
	if when == prefs.MarkOnScroll {
		s.passTick(c, now)
	} else {
		s.pass = passing{}
	}
}

// stayOn is the entry the stay counts for: the article in the reader once its text shows, or the entry in the lightbox.
func (s *Screen) stayOn() int64 {
	switch {
	case s.reading != 0 && s.reader.Ready():
		return s.reading
	case s.reading == 0 && s.lb.open:
		return s.lb.entry
	}
	return 0
}

// stayTick counts the time the entry on show stays, while the window has the focus, and marks it read when it has
// stayed for wait. Leaving the entry starts over; an entry that is read already, or that the reader marks before, is
// not counted.
func (s *Screen) stayTick(c *ui.Context, now time.Time, wait time.Duration) {
	id := s.stayOn()
	if id != s.stay.id {
		s.stay = stay{id: id}
		if i := s.index(id); id != 0 && (i < 0 || s.entries[i].IsRead) {
			s.stay.done = true
		}
	}
	if id == 0 || s.stay.done {
		return
	}
	i := s.index(id)
	if i < 0 || s.entries[i].IsRead {
		s.stay.done = true
		return
	}
	if s.deps.Active != nil && !s.deps.Active() {
		s.stay.last = time.Time{}
		return
	}
	if !s.stay.last.IsZero() {
		s.stay.spent += min(max(now.Sub(s.stay.last), 0), 2*stayStep)
	}
	s.stay.last = now
	if s.stay.spent >= wait {
		s.stay.done = true
		s.markRead(s.entries[i])
		s.announce = s.text.MarkRead
		return
	}
	c.After(min(wait-s.stay.spent, stayStep))
}

// passTick finds the entries that went off the top of the list since the last frame, from the places the entries were
// drawn at in that frame, and marks them read once the list has rested.
func (s *Screen) passTick(c *ui.Context, now time.Time) {
	p := &s.pass
	if s.reading == 0 && !s.loading && len(s.entries) > 0 && len(s.rects) > 0 {
		s.passLook(now)
	}
	if len(p.pending) == 0 {
		return
	}
	if wait := passQuiet - now.Sub(p.moved); wait > 0 {
		c.After(wait)
		return
	}
	s.flushPass()
}

// passLook notes the entries in view in the last frame and those that went past the top.
func (s *Screen) passLook(now time.Time) {
	p := &s.pass
	if p.skip > 0 {
		p.skip--
		return
	}
	top := s.top()
	sig := passSig{first: -1}
	for i, r := range s.rects {
		if r.H > 0 && r.Y+r.H > top && r.Y < s.height {
			if sig.first < 0 || i < sig.first {
				sig.first = i
			}
			sig.n++
			sig.sum += r.Y
		}
	}
	if sig.first < 0 || sig == p.sig {
		return
	}
	p.sig, p.moved = sig, now
	if p.seen == nil {
		p.seen = map[int64]bool{}
	}
	for i, r := range s.rects {
		if i < len(s.entries) && r.H > 0 && r.Y+r.H > top && r.Y < s.height {
			p.seen[s.entries[i].ID] = true
		}
	}
	for i, e := range s.entries {
		if !p.seen[e.ID] {
			continue
		}
		r, drawn := s.rects[i]
		if drawn && r.H > 0 && r.Y+r.H > top || !drawn && i >= sig.first {
			continue
		}
		delete(p.seen, e.ID)
		if !e.IsRead {
			p.pending = append(p.pending, e.ID)
		}
	}
}

// flushPass marks the entries that went past read, at once.
func (s *Screen) flushPass() {
	ids := s.pass.pending
	s.pass.pending = nil
	s.markPast(ids)
}

// markPast marks the entries ids read: the rows change at once, the server gets one change for each, at most
// passWorkers at a time, and a row it refuses goes back. The ones it took are offered together to be undone, with the
// toast that "Mark all as read" shares.
func (s *Screen) markPast(ids []int64) {
	if s.api == nil {
		return
	}
	on := true
	patch := client.EntryPatch{IsRead: &on}
	var sent []client.Entry
	for _, id := range ids {
		i := s.index(id)
		if i < 0 || s.entries[i].IsRead {
			continue
		}
		before := s.entries[i]
		s.set(i, patch)
		s.patchLists([]int64{id}, patch)
		if s.offline || s.waitingFor(id) >= 0 {
			wasRead := before.IsRead
			s.enqueue(id, patch, client.EntryPatch{IsRead: &wasRead}, before.Group)
			continue
		}
		sent = append(sent, before)
	}
	if len(sent) == 0 {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		errs := make([]error, len(sent))
		work := make(chan int)
		var wg sync.WaitGroup
		for range min(passWorkers, len(sent)) {
			wg.Go(func() {
				for k := range work {
					_, errs[k] = api.SetEntry(context.Background(), sent[k].ID, patch)
				}
			})
		}
		for k := range sent {
			work <- k
		}
		close(work)
		wg.Wait()
		return func() {
			if gen != s.gen {
				return
			}
			var undo []int64
			marked := 0
			for k, err := range errs {
				before := sent[k]
				switch {
				case s.expired(err):
					return
				case err == nil:
					marked++
					undo = append(undo, before.ID)
					if before.Group != nil {
						for _, m := range before.Group.Entries {
							if !m.IsRead && m.ID != before.ID {
								undo = append(undo, m.ID)
							}
						}
					}
				case unreached(err):
					wasRead := before.IsRead
					s.enqueue(before.ID, patch, client.EntryPatch{IsRead: &wasRead}, before.Group)
					s.drop()
				default:
					wasRead := before.IsRead
					s.revert(before.ID, client.EntryPatch{IsRead: &wasRead}, before.Group)
				}
			}
			if marked == 0 {
				return
			}
			s.loadSources()
			s.undoable = nil
			if len(undo) <= maxUndo {
				s.undoable = &marking{Marked: client.Marked{IDs: undo},
					text: s.format(s.text.MarkedScrolled, marked)}
			}
		}
	})
}
