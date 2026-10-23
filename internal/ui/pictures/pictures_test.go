package pictures

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// source answers every request from fails, then with data, and records the order and contexts of the requests.
type source struct {
	asked []string
	ctxs  []context.Context
	fails map[string]int
}

func (f *source) Image(ctx context.Context, address string) ([]byte, error) {
	f.asked = append(f.asked, address)
	f.ctxs = append(f.ctxs, ctx)
	if f.fails[address] > 0 {
		f.fails[address]--
		return nil, errors.New("down")
	}
	return []byte(address), nil
}

// run holds the work as the screen's background does: it waits in a queue until the test runs it.
type run struct{ queue []func() func() }

func (r *run) Go(work func() func()) { r.queue = append(r.queue, work) }

// settle does the work waiting, applies its results and starts what they free.
func (r *run) settle() {
	work := r.queue
	r.queue = nil
	for _, w := range work {
		w()()
	}
}

func decode([]byte) (*ui.Bitmap, error) { return &ui.Bitmap{}, nil }

func start(q *Queue, f *source, r *run, at time.Time, alive func() bool) {
	q.Download(f, decode, r.Go, func() time.Time { return at }, alive)
}

func yes() bool { return true }

func TestOrder(t *testing.T) {
	for _, tc := range []struct {
		latestFirst bool
		want        []string
	}{
		{false, []string{"0", "1", "2", "3"}},
		{true, []string{"5", "4", "3", "2"}},
	} {
		q, f, r := New(tc.latestFirst, 0), &source{}, &run{}
		for i := range 6 {
			q.Get(strconv.Itoa(i), now)
		}
		start(q, f, r, now, yes)
		r.settle()
		if !slices.Equal(f.asked, tc.want) {
			t.Fatalf("latest first %v: asked %v, want %v", tc.latestFirst, f.asked, tc.want)
		}
		if q.Active() != 0 && len(r.queue) != 0 {
			t.Fatalf("downloads left: %d", q.Active())
		}
	}
}

func TestMaxDownloads(t *testing.T) {
	q, f, r := New(false, 0), &source{}, &run{}
	for i := range 6 {
		q.Get(strconv.Itoa(i), now)
	}
	start(q, f, r, now, yes)
	if q.Active() != MaxDownloads || len(r.queue) != MaxDownloads {
		t.Fatalf("%d downloads, %d queued, want %d", q.Active(), len(r.queue), MaxDownloads)
	}
	r.settle()
	start(q, f, r, now, yes)
	r.settle()
	if len(f.asked) != 6 || q.Active() != 0 {
		t.Fatalf("asked %v, %d active", f.asked, q.Active())
	}
	if p := q.Peek("5"); p == nil || p.Bitmap == nil || p.Failed {
		t.Fatalf("picture not got: %+v", p)
	}
}

func TestStopCancelsDownloads(t *testing.T) {
	q, f, r := New(false, 0), &source{}, &run{}
	q.Get("a", now)
	start(q, f, r, now, yes)
	q.Stop()
	r.settle()
	if len(f.ctxs) != 1 || f.ctxs[0].Err() == nil {
		t.Fatal("the download was not cancelled")
	}
	q.Get("b", now)
	start(q, f, r, now, yes)
	r.settle()
	if len(f.ctxs) != 2 || f.ctxs[1].Err() != nil {
		t.Fatal("a download after the stop shares the cancelled context")
	}
}

func TestStaleResultDropped(t *testing.T) {
	q, f, r := New(false, 0), &source{}, &run{}
	q.Get("a", now)
	live := true
	start(q, f, r, now, func() bool { return live })
	live = false
	q.Reset()
	r.settle()
	if q.Len() != 0 || q.Active() != 0 {
		t.Fatalf("a stale result was applied: %d pictures, %d active", q.Len(), q.Active())
	}
	q.Get("b", now)
	start(q, f, r, now, func() bool { return false })
	r.settle()
	if p := q.Peek("b"); p == nil || p.Bitmap != nil || p.Failed || q.Active() != 1 {
		t.Fatalf("a dropped result changed the picture: %+v, %d active", p, q.Active())
	}
}

func TestRetry(t *testing.T) {
	q, f, r := New(false, 0), &source{fails: map[string]int{"a": 5}}, &run{}
	at := now
	fetch := func() {
		q.Get("a", at)
		start(q, f, r, at, yes)
		r.settle()
	}
	fetch()
	if p := q.Peek("a"); !p.Failed || len(f.asked) != 1 {
		t.Fatalf("first try: %+v, asked %d", p, len(f.asked))
	}
	fetch()
	if len(f.asked) != 1 {
		t.Fatalf("asked again at once: %d", len(f.asked))
	}
	for want := 2; want <= Tries; want++ {
		at = at.Add(Retry)
		fetch()
		if len(f.asked) != want {
			t.Fatalf("asked %d times, want %d", len(f.asked), want)
		}
	}
	at = at.Add(Retry)
	fetch()
	if p := q.Peek("a"); len(f.asked) != Tries || !p.Failed {
		t.Fatalf("asked %d times past the limit, failed %v", len(f.asked), p.Failed)
	}
}

func TestRetrySucceeds(t *testing.T) {
	q, f, r := New(false, 0), &source{fails: map[string]int{"a": 1}}, &run{}
	q.Get("a", now)
	start(q, f, r, now, yes)
	r.settle()
	later := now.Add(Retry)
	q.Get("a", later)
	start(q, f, r, later, yes)
	r.settle()
	if p := q.Peek("a"); p.Failed || p.Bitmap == nil {
		t.Fatalf("the retry did not get the picture: %+v", p)
	}
}

func TestForgetFailed(t *testing.T) {
	q, f, r := New(false, 0), &source{fails: map[string]int{"bad": 1}}, &run{}
	q.Get("bad", now)
	q.Get("good", now)
	start(q, f, r, now, yes)
	r.settle()
	q.ForgetFailed()
	if q.Peek("bad") != nil || q.Peek("good") == nil {
		t.Fatalf("forgot the wrong pictures: %d held", q.Len())
	}
}

func TestKeep(t *testing.T) {
	q, f, r := New(false, 2), &source{}, &run{}
	for _, a := range []string{"a", "b", "c"} {
		q.Get(a, now)
		start(q, f, r, now, yes)
		r.settle()
	}
	q.Get("b", now)
	q.Get("c", now)
	q.Get("d", now)
	start(q, f, r, now, yes)
	r.settle()
	if q.Len() != 2 || q.Peek("c") == nil || q.Peek("d") == nil {
		t.Fatalf("kept %d pictures, want the two drawn last, c and d", q.Len())
	}
}
