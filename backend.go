package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

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

// backend is a running server with its store and the poller that fills it.
type backend struct {
	store      *store.Store
	http       *http.Server
	addr       string
	stopPoller context.CancelFunc
	pollerDone chan struct{}
}

// start opens the database in dataDir and serves the API until close. Port 0 picks a free port.
func start(ctx context.Context, dataDir, password string, port int) (*backend, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, filepath.Join(dataDir, "oriel.sqlite"))
	if err != nil {
		return nil, err
	}
	api, err := server.New(st, server.Config{Password: password, Version: version})
	if err != nil {
		st.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp", server.ListenAddr(password, port))
	if err != nil {
		st.Close()
		return nil, err
	}
	pollCtx, stopPoller := context.WithCancel(context.Background())
	b := &backend{
		store: st, http: &http.Server{Handler: api, ReadHeaderTimeout: 10 * time.Second}, addr: listener.Addr().String(),
		stopPoller: stopPoller, pollerDone: make(chan struct{}),
	}
	go func() {
		if err := b.http.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server stopped", "error", err)
		}
	}()
	go func() {
		fetch.New(st, version).Run(pollCtx)
		close(b.pollerDone)
	}()
	return b, nil
}

// close waits for requests in flight, up to the context deadline, stops the poller, then closes the database.
func (b *backend) close(ctx context.Context) error {
	err := b.http.Shutdown(ctx)
	b.stopPoller()
	<-b.pollerDone
	return errors.Join(err, b.store.Close())
}
