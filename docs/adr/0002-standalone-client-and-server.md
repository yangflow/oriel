# 0002 A standalone app: one program is both the client and the server

- Status: accepted
- Date: 2026-10-12
- Related: [docs/product.md](../product.md), "Architecture" (架构)

## Context

There were two paths: build a native client for an existing server, or build a standalone app with its own fetching,
storage and AI calls. The first cannot work without another service; the second works as soon as it is installed.

## Decision

Oriel is a standalone app and depends on no other service.

Oriel is one Go module and one program, in three layers:

- Server: fetches subscriptions, extracts full text, stores data, calls the AI endpoint, prunes, and serves the data
  through an HTTP API.
- Client: the MyGo native UI. It reads and writes data only through the HTTP API and never touches storage directly.
- Startup: desktop mode starts the server on this machine, listening only on `127.0.0.1`, then opens the window;
  headless mode runs only the server, for Oriel clients on other devices to connect to. A client can connect to the
  local server or to a remote one.

## Cost

The client also goes through HTTP, which adds a layer of serialization for local use. The HTTP API has to be
defined, kept compatible across versions and authenticated by us. Without cgo, storage can only use a pure Go
driver.

## When to revisit

The overhead of local HTTP is measured on real devices to affect interaction, or headless mode goes unused for a long
time.
