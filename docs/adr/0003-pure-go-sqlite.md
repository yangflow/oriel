# 0003 Pure Go SQLite

- Status: accepted
- Date: 2026-10-12
- Related: [docs/product.md](../product.md), "Storage" (存储)

## Context

The server needs a single-file embedded database with full-text search; for a single user SQLite is enough. The
technical baseline does not allow cgo in production code; otherwise cross-compiling the six targets would need six C
toolchains.

## Decision

Use `modernc.org/sqlite`. It is SQLite's C source machine-translated to Go, includes FTS5 and the trigram tokenizer,
and compiles for all six targets with `CGO_ENABLED=0`. It is used through `database/sql` with the driver name
`sqlite`.

## Cost

It is slower than the cgo version, and the program is larger. It brings a chain of indirect dependencies
(`modernc.org/libc` and others), so every upgrade has to sync `THIRD_PARTY.md` and the license files. The SQLite
version follows the driver and cannot be upgraded on its own.

## When to revisit

Query or write speed is measured on real devices to affect interaction, or the driver stops being maintained.
