# Oriel

[![CI](https://github.com/yangflow/oriel/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yangflow/oriel/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

English | [中文](README.zh-CN.md)

A personal mixed-timeline reader for RSS and RSSHub, written in pure Go with native UI.

Articles, videos, social posts, images and community threads arrive from different places. Oriel puts all five in one
timeline, on your own machine, in a native app with no browser engine inside. One Go program is both the app and its
server: on a desktop it opens a window and serves only itself; on a home server or a VPS it runs without a window and
other Oriel clients connect to it. Your data is one SQLite file you own. No accounts, no recommendations, no social
features of its own.

```text
┌──────────────────── oriel ────────────────────┐
│  native window (MyGo)   ──HTTP──▶   server    │
│                                      │        │
│                     fetch loop ──▶ SQLite     │
└───────────────────────────────────────────────┘
          oriel serve: the same server, no window
```

## Features

### Reading

- **One timeline, five kinds.** Article rows, video covers, social posts with their pictures, a masonry wall for
  images and three panes for community threads; each kind can also show as a grid or compact rows.
- **Narrow it down.** Kinds, subscriptions and folders, Unread, Starred, a time range, and search with phrases,
  exclusions, `from:`, `kind:`, `after:` and `before:`.
- **Keyboard first.** Keys move, open, star and mark entries; N jumps to the next unread one, ? lists every key, and a
  command palette finds actions by name. Marking all read can be undone.
- **Native reader.** Full text when it could be extracted, the feed's content otherwise. Find in the article, export
  it as Markdown, see what changed in an update, read immersive, and choose a sepia or mist page and the line width.
- **Calm timeline.** New entries wait behind an "N new" button, an article several subscriptions carry shows once,
  entries can come back later with "Remind me", and you choose when an entry counts as read.

### Subscriptions and fetching

- **Add from almost anything.** A feed address, an RSSHub route, a website (its feed or route is found), or a YouTube,
  Bilibili, X, GitHub, Telegram or Weibo link.
- **Scheduled fetching.** RSS, Atom, JSON Feed and RSSHub, with conditional requests, backoff, a per-host limit,
  WebSub push and fallback to the next RSSHub instance. The check interval is worked out from how often a feed
  publishes.
- **Per-subscription control.** Check interval, retention, pace, daily cap, mute keywords, full-text rules, request
  headers and proxy. Subscription health points out dead, failing, moved and noisy feeds.
- **Rules.** Star or mark read new entries that match, and hide entries with mute keywords without deleting them.

### AI

- **Summaries and translation** through your own OpenAI-compatible endpoint, such as OpenAI, Ollama or LM Studio. On
  demand in the reader, or automatically for a subscription's new entries, with translated titles for foreign-language
  feeds. The API key stays on the server.

### Your data

- **One SQLite file** with full-text search, through a pure Go driver.
- **Starred copies.** A starred entry keeps its full text and pictures, so it survives the original page going away.
- **Import and export.** OPML in and out; subscriptions, stars and read state from Miniflux and FreshRSS.
- **Backups.** One `.oriel-backup` file from Settings or `oriel backup`, restored from Settings or `oriel restore`,
  and scheduled backups to a folder. Sessions and the AI key stay out of the file.
- **Diagnostics** without secrets, for bug reports.

### Running it

- **Desktop app** serving only the loopback interface, or **`oriel serve`** headless with password sign-in and
  per-device sessions; the desktop app can connect to a remote server.
- **Desktop integration.** Open at login, an unread count in the menu bar or tray and on the Dock, and updating after
  the window is closed.
- **English and Simplified Chinese** interface.

Planned work is in the [milestones](https://github.com/yangflow/oriel/milestones).

## Status

Early development. Releases are on the [releases page](https://github.com/yangflow/oriel/releases) and installed
copies update themselves after you confirm. The macOS app is signed and notarized; the Windows installers are not
signed yet, so SmartScreen warns on first launch.

Targets: macOS 12+, Windows 10/11 and Linux with GTK 3, on amd64 and arm64. Linux builds need glibc, also headless.

## Build from source

You need the Go version in `.go-version` and Python 3:

```bash
git clone https://github.com/yangflow/oriel
cd oriel
go tool mygo dev           # run the desktop app
go tool mygo build         # package it into build/
```

## Run headless

```bash
ORIEL_PASSWORD=<password> go run . serve -data ./data -port 7470
```

- Without `ORIEL_PASSWORD` the server listens on `127.0.0.1` only. The password is read from the environment only,
  so it never appears in the process list.
- `-data` defaults to the user data directory.
- Behind a reverse proxy, pass `-trusted-proxy <ip-or-cidr,...>` so the login throttle reads `X-Forwarded-For` from
  those addresses. List only your proxies; a `/0` range is refused.

### Docker or systemd

The files are in [`deploy/`](deploy/); the details (data directory, port, reverse proxy and HTTPS, health check,
backups, upgrading) are in [docs/product/running.md](docs/product/running.md#部署) ("部署", in Chinese). The image is built from a
checkout, not pulled from a registry, for `linux/amd64` and `linux/arm64`:

```bash
echo 'ORIEL_PASSWORD=<password>' > deploy/.env && chmod 600 deploy/.env
docker compose -f deploy/compose.yaml up -d --build
```

- The example publishes the port on the host's `127.0.0.1` only; edit `ports` to open it, preferably behind an HTTPS
  reverse proxy.
- In a container the password is required: without it the server listens only inside the container.
- The image runs as a non-root user, keeps its data in the volume `/data`, and answers Docker's health check with
  `oriel healthcheck`.
- For systemd, install `deploy/oriel.service` with the password in a `0600` environment file; it runs as a dynamic
  user with its data in `/var/lib/oriel`.

## Development

```bash
scripts/check.sh                  # what a pull request runs: gofmt, go mod tidy, vet, tests, -race on the concurrent packages
scripts/check.sh --cross --race   # everything, as CI runs it on main and daily: -race on all packages, six targets
```

| Topic | Where |
|---|---|
| Behavior, data model, API, fetching and sign-in (in Chinese) | [docs/product.md](docs/product.md) and [docs/product/](docs/product/) |
| Screens canvas, design system and tokens | [design/README.md](design/README.md) |
| Architecture decisions | [docs/adr/](docs/adr/README.md) |
| Releases | [docs/release.md](docs/release.md) |
| Repository rules for people and agents | [AGENTS.md](AGENTS.md) |

## Contributing

Issues and pull requests are welcome in English or Chinese; see [CONTRIBUTING.md](CONTRIBUTING.md). Report
vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE). Licenses of dependencies are listed in [THIRD_PARTY.md](THIRD_PARTY.md).
