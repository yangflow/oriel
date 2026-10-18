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
	"sync"
	"time"

	"github.com/yangflow/oriel/internal/ai"
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
	store          *store.Store
	http           *http.Server
	addr           string
	stopBackground context.CancelFunc
	background     sync.WaitGroup
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
	worker := ai.New(st, version)
	api, err := server.New(st, server.Config{Password: password, Version: version, Images: fetch.NewImages(version), AI: worker})
	if err != nil {
		st.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp", server.ListenAddr(password, port))
	if err != nil {
		st.Close()
		return nil, err
	}
	backgroundCtx, stopBackground := context.WithCancel(context.Background())
	b := &backend{
		store: st, http: &http.Server{Handler: api, ReadHeaderTimeout: 10 * time.Second}, addr: listener.Addr().String(),
		stopBackground: stopBackground,
	}
	go func() {
		if err := b.http.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server stopped", "error", err)
		}
	}()
	b.background.Go(func() { fetch.New(st, version).Run(backgroundCtx) })
	b.background.Go(func() { worker.Run(backgroundCtx) })
	return b, nil
}

// close waits for requests in flight, up to the context deadline, stops the poller and the AI worker, then
// closes the database.
func (b *backend) close(ctx context.Context) error {
	err := b.http.Shutdown(ctx)
	b.stopBackground()
	b.background.Wait()
	return errors.Join(err, b.store.Close())
}
