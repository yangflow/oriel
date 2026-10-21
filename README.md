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

Working now:

- **Scheduled fetching.** A few workers keep fetching due subscriptions, with conditional requests (ETag and
  Last-Modified), backoff after failures and a limit on concurrent requests to one host. RSS, Atom and JSON Feed are supported, as well as RSSHub routes.
- **Full text for articles.** Article pages are reduced to their main content with Readability, including the
  linked post of a Discourse topic. Links found in feeds cannot reach loopback or private addresses.
- **Retention.** Unstarred entries older than a chosen number of days are pruned hourly; 0 keeps everything.
- **Subscription API.** Add, list, edit, pause, delete and refresh subscriptions over HTTP, with validation
  and duplicate detection.
- **Timeline API.** Page through all entries newest first, filter by source, kind, unread or starred, search
  the full text, and mark entries read or starred. Per-source mute keywords hide matching entries without
  deleting them.
- **Timeline screen.** All subscriptions in one list, newest first, each kind in its own layout: article rows
  with a thumbnail, video covers with their duration, social posts with up to four pictures, and a masonry wall
  for images. A list of kinds and a list of subscriptions narrow it; Unread, Starred and search narrow it
  further. Keys move, open, star and mark entries, N jumps to the next unread one, and ? lists every key; marking
  all read can be undone. New entries wait behind a "N new" button instead of moving the list. Each subscription shows its
  site's icon. Videos, social posts and pictures open in the browser.
- **Article reader.** An article opens in place of the list, drawn natively: headings, paragraphs, lists,
  quotes, code, links and pictures. It shows the full text when it was extracted, and otherwise what the feed
  carries, with a note saying so and a link to the original. Keys go to the next article, star, mark read and
  return to the list.
- **Subscription screen.** Add a subscription from a feed address, an RSSHub route, or a YouTube, Bilibili, X,
  GitHub, Telegram or Weibo link, which is turned into its feed or route; the kind and the check interval are
  chosen in the same dialog. Edit, pause, refresh and delete a subscription from the sidebar's right-click menu
  or the pencil in the toolbar; deleting asks first, since its entries go too.
- **Settings.** Change the RSSHub instance and how long entries are kept from the gear beside the server bar or
  "Settings…" (Cmd/Ctrl+,); they are kept on the server, so every device connected to it shares them. Signed in to
  a server with a password, see each device signed in and sign out the ones you no longer use.
- **AI summaries and translation.** Through your own OpenAI-compatible endpoint, such as OpenAI or a local
  Ollama, set up in Settings with a connection test; the API key is kept on the server and never shown again. The
  reader summarizes or translates the open article on demand, cancellable, and keeps the result for next time; a
  subscription can summarize or translate its new entries by itself and use its own prompts.
- **OPML import and export.** Import subscriptions from another reader, nested groups included, and see which
  could not be added and why; export them all to a file that keeps each source's kind and RSSHub path.
- **Two run modes.** A desktop app that serves only the loopback interface, and `oriel serve` for a headless host
  with password sign-in and per-device sessions.
- **Connect to a remote server.** The desktop app can switch to an `oriel serve` on another machine from
  "Connect to Server…" in the app menu. Only the session token is kept, readable by you alone; the password is
  never saved.
- **One storage file.** SQLite with full-text search through a pure Go driver; no cgo in production code. The
  window reads and writes only through the HTTP API, so desktop and headless behave the same.
- **Two languages.** The interface is in English and Simplified Chinese.

Planned, in the order of the [milestones](https://github.com/yangflow/oriel/milestones) (rules in the
[roadmap](docs/roadmap.md), in Chinese): 0.5 polish and fixes, 0.6 reading, 0.7 subscription care, 0.8 backup and
diagnostics, 0.9 priorities and automation.

Status: early development; releases are on the [releases page](https://github.com/yangflow/oriel/releases). The
window has the connect screen, the timeline, the subscription dialogs, the article reader and the settings.
Targets: macOS 12+, Windows 10/11 and Linux with GTK 3, on amd64 and arm64. Linux builds need glibc, also when running headless.

Releases are built and published from a version tag ([docs/release.md](docs/release.md)).
Installed copies will update themselves from them after you confirm in the update window. The macOS app is signed
and notarized. The Windows installers are not signed yet, so SmartScreen warns on first launch.

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
| Roadmap rules and milestones (in Chinese) | [docs/roadmap.md](docs/roadmap.md) |
| Architecture decisions | [docs/adr/](docs/adr/README.md) |
| Repository rules for people and agents | [AGENTS.md](AGENTS.md) |

## Contributing

Issues and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE). Licenses of dependencies are listed in [THIRD_PARTY.md](THIRD_PARTY.md).
