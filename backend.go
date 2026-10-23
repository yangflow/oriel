package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/autobackup"
	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/server"
	"github.com/yangflow/oriel/internal/store"
)

//go:embed mygo.json
var packaging []byte

// version is the single version in mygo.json, also for unpackaged builds such as `oriel serve`.
var version = func() string {
	var config struct{ Version string }
	if err := json.Unmarshal(packaging, &config); err != nil {
		panic(err)
	}
	return config.Version
}()

// backend is a running server with its store, the poller that fills it and the AI worker.
type backend struct {
	store      *store.Store
	http       *http.Server
	addr       string
	background *background
	// running counts the requests being handled; close waits for them to return before it closes the store.
	running atomic.Int64
}

// requestGrace is how long close waits for handlers to return once their connections are cut off.
const requestGrace = 5 * time.Second

// track counts the requests in flight in handler.
func (b *backend) track(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.running.Add(1)
		defer b.running.Add(-1)
		handler.ServeHTTP(w, r)
	})
}

// background is the work that runs beside the server: the fetch loop, the AI worker, the automatic backups and the
// reading-time backfill.
// A restore stops it while the data is replaced and starts it again.
type background struct {
	store  *store.Store
	poller *fetch.Poller
	worker *ai.Worker
	copier *fetch.Copier
	auto   *autobackup.Runner

	mu     sync.Mutex
	stop   context.CancelFunc
	group  sync.WaitGroup
	closed bool
}

// run starts the work unless it runs already or the backend is closed.
func (g *background) run() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stop != nil || g.closed {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	g.stop = stop
	g.group.Go(func() { g.poller.Run(ctx) })
	g.group.Go(func() { g.worker.Run(ctx) })
	g.group.Go(func() { g.copier.Run(ctx) })
	g.group.Go(func() { g.auto.Run(ctx) })
	g.group.Go(func() {
		if err := g.store.BackfillReadingTime(ctx); err != nil && ctx.Err() == nil {
			slog.Error("Cannot fill in reading times", "error", err)
		}
	})
}

// halt stops the work and waits until it has.
func (g *background) halt() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stop != nil {
		g.stop()
		g.stop = nil
	}
	g.group.Wait()
}

// pause is the server's hook for a restore: it halts the work and returns the function that starts it again.
func (g *background) pause(context.Context) (func(), error) {
	g.halt()
	return g.run, nil
}

// shutdown halts the work for good.
func (g *background) shutdown() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
	g.halt()
}

// start opens the database in dataDir and serves the API until close. Mode is diag.ModeDesktop or diag.ModeHeadless. Port 0 picks a free port.
func start(ctx context.Context, dataDir, password, mode string, port int, proxies []netip.Prefix) (*backend, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, filepath.Join(dataDir, "oriel.sqlite"))
	if err != nil {
		return nil, err
	}
	worker := ai.New(st, version)
	poller := fetch.New(st, version)
	images := fetch.NewImages(version, filepath.Join(dataDir, "image-cache"))
	auto := autobackup.New(st, version)
	work := &background{store: st, poller: poller, worker: worker, copier: fetch.NewCopier(st, images), auto: auto}
	api, err := server.New(st, server.Config{Password: password, Version: version, Images: images,
		Discover: fetch.NewDiscoverer(version), AI: worker, Fetching: poller.Fetching, Log: logs, Mode: mode, Pause: work.pause, AutoBackup: auto, TrustedProxies: proxies})
	if err != nil {
		st.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp", server.ListenAddr(password, port))
	if err != nil {
		st.Close()
		return nil, err
	}
	b := &backend{
		store: st, http: &http.Server{
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute,
			// Longer than the slowest answer, an AI test call of up to two minutes.
			WriteTimeout: 150 * time.Second, IdleTimeout: 2 * time.Minute,
		}, addr: listener.Addr().String(),
		background: work,
	}
	b.http.Handler = b.track(api)
	go func() {
		if err := b.http.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server stopped", "error", err)
		}
	}()
	work.run()
	return b, nil
}

// close waits for requests in flight, up to the context deadline, stops the poller and the AI worker, then
// closes the database. A request that outlives the deadline is cut off, its context cancelled, and close waits a
// few seconds for its handler to return, so that no handler runs against the closing database or enqueues more work.
func (b *backend) close(ctx context.Context) error {
	err := b.http.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, b.http.Close())
		b.waitForHandlers(requestGrace)
	}
	b.background.shutdown()
	return errors.Join(err, b.store.Close())
}

// waitForHandlers waits until no handler is running, at most grace.
func (b *backend) waitForHandlers(grace time.Duration) {
	deadline := time.Now().Add(grace)
	for b.running.Load() > 0 {
		if time.Now().After(deadline) {
			slog.Warn("Requests still running after the server was closed", "count", b.running.Load())
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
