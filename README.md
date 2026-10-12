<h1 align="center">Oriel</h1>

<p align="center">
  A personal mixed-timeline reader for RSS and RSSHub, written in pure Go with native UI.
</p>

<p align="center">
  <a href="https://github.com/yangflow/oriel/actions/workflows/ci.yml"><img src="https://github.com/yangflow/oriel/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/yangflow/oriel" alt="License: MIT"></a>
</p>

<p align="center">
  <b>English</b> · <a href="README.zh-CN.md">简体中文</a>
</p>

## Why Oriel

Articles, videos, social posts and images arrive from different places, and most readers either flatten them into
one list of links or push you into a cloud account. Oriel keeps one timeline for all four, on your own machine, in
a native app without a browser engine inside.

One Go program is both the app and its server. On a desktop it opens a window and serves only itself. On a home
server or a VPS it runs without a window, and other Oriel clients connect to it. Your data stays in a SQLite file
you own. There are no accounts, no recommendations and no community features.

## Features

Working now, on the server side:

- **Scheduled fetching.** Due subscriptions are fetched every 15 seconds with conditional requests (ETag and
  Last-Modified) and backoff after failures. RSS, Atom and JSON Feed are supported, as well as RSSHub routes.
- **Full text for articles.** Article pages are reduced to their main content with Readability, including the
  linked post of a Discourse topic. Links found in feeds cannot reach loopback or private addresses.
- **Retention.** Unstarred entries older than a chosen number of days are pruned hourly; 0 keeps everything.
- **Two run modes.** A desktop app that serves only the loopback interface, and `oriel serve` for a headless host
  with password sign-in and per-device sessions.

Planned, in the order of the [roadmap](docs/roadmap.md) (in Chinese): adding and editing subscriptions, the
timeline with one layout per kind, the article reader, on-demand AI summaries and translation through your own
OpenAI-compatible endpoint, settings and OPML import and export.

## How it works

```text
┌──────────────────── oriel ────────────────────┐
│  native window (MyGo)   ──HTTP──▶   server    │
│                                      │        │
│                     fetch loop ──▶ SQLite     │
└───────────────────────────────────────────────┘
          oriel serve: the same server, no window
```

- The window reads and writes only through the HTTP API, so a desktop app and a headless server behave the same.
- Storage is one SQLite file with full-text search, through a pure Go driver; there is no cgo in production code.
- The interface is in English and Simplified Chinese.

## Status

Early development, version 0.1.0, with no release yet. The server parts above work and are tested; the window
still shows the template's counter example. Subscriptions and settings have no API or screen yet.

Targets: macOS 12+, Windows 10/11 and Linux with GTK 3, on amd64 and arm64. Linux builds need glibc, also when
running headless.

## Quick start

Build from source with Go 1.27.1 (see `.go-version`) and Python 3:

```bash
git clone https://github.com/yangflow/oriel
cd oriel
scripts/check.sh --cross   # format, vet, tests and six cross-compiled targets
go tool mygo dev           # run the desktop app
go tool mygo build         # package it into build/
```

Run it headless:

```bash
ORIEL_PASSWORD=<password> go run . serve -data ./data -port 7470
```

Without `ORIEL_PASSWORD` the server listens on `127.0.0.1` only. The password is read from the environment only,
so it never appears in the process list.

## Security

Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md). Do not open public issues for them.

## Documentation

| Topic | Where (in Chinese) |
|---|---|
| Behavior, data model, fetching, HTTP and sign-in | [docs/product.md](docs/product.md) |
| Colors, type, spacing and components | [docs/design-system.md](docs/design-system.md) |
| Screens and their states | [design/README.md](design/README.md) |
| Order of work | [docs/roadmap.md](docs/roadmap.md) |
| Architecture decisions | [docs/adr/](docs/adr/README.md) |
| Rules for changing the repository | [AGENTS.md](AGENTS.md) |

## Contributing

Issues and pull requests are welcome; read [CONTRIBUTING.md](CONTRIBUTING.md) first. Every commit is signed off
with `git commit -s` for the Developer Certificate of Origin.

## Support

See [SUPPORT.md](SUPPORT.md). Questions go to issues; there are no Discussions and no paid support.

## License

[MIT](LICENSE). Licenses of dependencies are listed in [THIRD_PARTY.md](THIRD_PARTY.md).
