# Oriel

[![CI](https://github.com/yangflow/oriel/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yangflow/oriel/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

English | [中文](README.zh-CN.md)

A personal mixed-timeline reader for RSS and RSSHub, written in pure Go with native UI.

Articles, videos, social posts and images arrive from different places, and most readers either flatten them into
one list of links or push you into a cloud account. Oriel keeps one timeline for all four, on your own machine, in
a native app without a browser engine inside. One Go program is both the app and its server: on a desktop it opens
a window and serves only itself; on a home server or a VPS it runs without a window and other Oriel clients connect
to it. Your data stays in a SQLite file you own. There are no accounts, no recommendations and no community
features.

```text
┌──────────────────── oriel ────────────────────┐
│  native window (MyGo)   ──HTTP──▶   server    │
│                                      │        │
│                     fetch loop ──▶ SQLite     │
└───────────────────────────────────────────────┘
          oriel serve: the same server, no window
```

## Features

Working now, on the server side:

- **Scheduled fetching.** Due subscriptions are fetched every 15 seconds with conditional requests (ETag and
  Last-Modified) and backoff after failures. RSS, Atom and JSON Feed are supported, as well as RSSHub routes.
- **Full text for articles.** Article pages are reduced to their main content with Readability, including the
  linked post of a Discourse topic. Links found in feeds cannot reach loopback or private addresses.
- **Retention.** Unstarred entries older than a chosen number of days are pruned hourly; 0 keeps everything.
- **Two run modes.** A desktop app that serves only the loopback interface, and `oriel serve` for a headless host
  with password sign-in and per-device sessions.
- **One storage file.** SQLite with full-text search through a pure Go driver; no cgo in production code. The
  window reads and writes only through the HTTP API, so desktop and headless behave the same.
- **Two languages.** The interface is in English and Simplified Chinese.

Planned, in the order of the [roadmap](docs/roadmap.md) (in Chinese): adding and editing subscriptions, the
timeline with one layout per kind, the article reader, on-demand AI summaries and translation through your own
OpenAI-compatible endpoint, settings and OPML import and export.

Status: early development, version 0.1.0, no release yet. The window still shows a counter example;
subscriptions and settings have no API or screen yet. Targets: macOS 12+, Windows 10/11 and Linux with GTK 3, on
amd64 and arm64. Linux builds need glibc, also when running headless.

## Quick start

Build from source with the Go version in `.go-version` and Python 3:

```bash
git clone https://github.com/yangflow/oriel
cd oriel
go tool mygo dev           # run the desktop app
go tool mygo build         # package it into build/
```

## Usage

Run the server without a window:

```bash
ORIEL_PASSWORD=<password> go run . serve -data ./data -port 7470
```

Without `ORIEL_PASSWORD` the server listens on `127.0.0.1` only. The password is read from the environment only,
so it never appears in the process list. `-data` defaults to the user data directory.

## Development

```bash
scripts/check.sh --cross   # gofmt, go mod tidy, vet, tests, script tests, six cross-compiled targets
go test -race ./...
```

| Topic | Where |
|---|---|
| Behavior, data model, fetching, HTTP and sign-in (in Chinese) | [docs/product.md](docs/product.md) |
| Design language: layout, color, type, components, accessibility | [docs/design.md](docs/design.md) |
| Screen mockups | [design/README.md](design/README.md) |
| Order of work (in Chinese) | [docs/roadmap.md](docs/roadmap.md) |
| Architecture decisions | [docs/adr/](docs/adr/README.md) |
| Repository rules for people and agents | [AGENTS.md](AGENTS.md) |

## Contributing

Issues and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE). Licenses of dependencies are listed in [THIRD_PARTY.md](THIRD_PARTY.md).
