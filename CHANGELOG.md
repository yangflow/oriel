# Changelog

All notable user-facing changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Changed

- The window uses Oriel's own palette, warm neutrals with a teal accent, in light and dark, and Lucide icons. It opens at 960×640 and can shrink to 640×480.
- The interface is available in English and Simplified Chinese only; Chinese systems, Traditional included, show Simplified Chinese.

### Added

- The timeline is the home screen: every subscription's entries in one list, newest first. Articles show a thumbnail and summary, videos a cover with the duration, social posts up to four pictures, and the Images tab is a masonry wall. Kind tabs, a subscription list, Unread and Starred filters and full-text search narrow the list; J/K, O, S, M, `/` and Cmd/Ctrl+F work from the keyboard. Entries can be starred, marked read or unread, and marked read in bulk after a confirmation. New entries are checked every minute and wait behind a "N new" button. Until the reader lands, an entry opens in the browser and is marked read.
- `GET /api/image` relays a feed picture to the desktop app, so pictures load the same way from a remote server.
- The desktop app can connect to a remote `oriel serve` from "Connect to Server…" in the app menu, and sign in with its password. The session token is kept in the data directory, readable by the user only; the password is never saved. When the session ends, the app asks for the password again; "This computer" switches back to local data.
- OPML import and export over HTTP. Import walks nested groups and counts duplicates apart from failures. Export keeps each source's kind and RSSHub path, so the file imports back into the same subscriptions.
- An HTTP API for settings: read and change the RSSHub address and the retention period, with validation; secrets are never sent back.
- Mute keywords hide matching entries from the timeline, search, unread counts and mark-all-read; starred entries stay, and nothing is deleted.
- An HTTP API for the timeline: pages by cursor, filters by source, kind, unread and starred, full-text search, and marking entries read or starred.
- An HTTP API for subscriptions: add, list, edit, pause, delete and refresh, with validation and duplicate detection.
- Entries older than the retention period are pruned hourly unless starred, and expired items are not fetched again.
- Article entries get their full text through Readability, retried up to three times; links found in feeds cannot reach private or loopback addresses.
- The server fetches due subscriptions every 15 seconds, with conditional requests and backoff after failures.
- The desktop app keeps its data in a local SQLite database and serves it on the loopback interface.
- `oriel serve` runs the server without a window; set `ORIEL_PASSWORD` to require sign-in and accept remote connections.
- Native window with native update window support.
