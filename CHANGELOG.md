# Changelog

All notable user-facing changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Changed

- The interface is available in English and Simplified Chinese only; Chinese systems, Traditional included, show Simplified Chinese.

### Added

- An HTTP API for subscriptions: add, list, edit, pause, delete and refresh, with validation and duplicate detection.
- Entries older than the retention period are pruned hourly unless starred, and expired items are not fetched again.
- Article entries get their full text through Readability, retried up to three times; links found in feeds cannot reach private or loopback addresses.
- The server fetches due subscriptions every 15 seconds, with conditional requests and backoff after failures.
- The desktop app keeps its data in a local SQLite database and serves it on the loopback interface.
- `oriel serve` runs the server without a window; set `ORIEL_PASSWORD` to require sign-in and accept remote connections.
- Native window with a localized counter example and native update window support.
