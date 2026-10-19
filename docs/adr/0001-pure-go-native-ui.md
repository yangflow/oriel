# 0001 Pure Go native UI

- Status: accepted
- Date: 2026-10-12
- Related: [docs/product.md](../product.md), "Technical baseline" (技术基线)

## Context

A desktop app built with Electron around a web page is heavy in size and startup. Oriel wants a single language,
reproducible builds and a truly native window. References: [MyGo native UI docs](https://mygo.egoist.dev/docs/ui)
and [the source at the pinned version](https://github.com/egoist/mygo/tree/v0.4.1).

## Decision

Use MyGo native UI and one Go module, with the command-line tool pinned in `go.mod` with `tool`. Updates use the
native updater plugin.

## Cost

Controls are drawn on the GPU, so accessibility and behavior on each platform have to be tested on real devices.
Without a WebView, video and social content cannot embed the official players. MyGo is at an early version; every
upgrade needs a look at API and packaging changes.

## When to revisit

A required platform capability or accessibility requirement cannot be met by native UI.
