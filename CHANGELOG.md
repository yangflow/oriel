# Changelog

All notable user-facing changes are recorded here, following [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Changed

- The window uses Oriel's own palette, warm neutrals with a teal accent, in light and dark, and Lucide icons. It opens at 960×640 and can shrink to 640×480.
- The interface is available in English and Simplified Chinese only; Chinese systems, Traditional included, show Simplified Chinese.

### Added

- Oriel has its own icon, a mixed timeline on an ivory tile, in the Dock, the taskbar, the installers and the about window.
- Releases are built from a version tag for macOS (universal, signed and notarized), Windows (amd64 and arm64 installers, not signed yet) and Linux (amd64 and arm64), and installed copies update themselves from the GitHub releases of Oriel: checked daily and from "Check for Updates…", installed only after the update window is confirmed, and refused unless signed with Oriel's update key. The update window follows the interface language.
- AI summaries and translations in the app. The reader's sparkles and languages buttons summarize or translate the open article, with a cancel while it runs, the model's name under the result, a localized reason and a retry when it fails, and the stored result shown at once next time; a translation can switch back to the original. Settings has an AI group for the endpoint, model, API key (shown only as configured or not, and removable), output language and the global prompts, with a connection test. The edit dialog adds switches to summarize or translate new entries and the subscription's own prompts. While AI is not set up, these entry points lead to Settings.
- An HTTP API for AI summaries and translations through your own OpenAI-compatible endpoint, such as OpenAI or a local Ollama: settings for the endpoint, model, write-only API key, target language and global prompts, with a connection test; on-demand requests that are queued, stored and answered from the store while the article and settings stay the same, with a source's own prompt taking precedence; and automatic summaries or translations of new entries for sources that ask for them, retried up to three times without holding up fetching. Long translations are sent in parts. The key, prompts and article text are never logged.
- Settings in the app, from the gear beside the server bar or "Settings…" (Cmd/Ctrl+,): the RSSHub instance and how long entries are kept, saved through the server with errors next to the field; OPML import from a file, read and sent in the background, with a summary that lists what could not be added and why, and export to a file that imports back; and the devices signed in to a server with a password, the current one marked, each of the others revocable after a confirmation.
- Subscriptions can be added and edited in the app. The add dialog takes a feed address, an RSSHub route, a full address on the RSSHub instance, or a YouTube, Bilibili, X, GitHub, Telegram or Weibo link, says what it found, and asks for the RSSHub instance when a route needs one. The edit dialog shows whether fetches succeed and changes the title, kind, check interval, pause and mute keywords. The sidebar's + button, Cmd/Ctrl+N and the empty timeline add; a right click on a subscription refreshes, edits, pauses or deletes it, after a confirmation that its entries go too, starred ones included.
- Articles open in a native reader in place of the list: the full text when it was extracted, otherwise what the feed carries with a note saying so, and while extraction is pending the reader checks again every 5 seconds. Headings, paragraphs, lists, quotes, code, links, pictures and captions are drawn natively; links open in the browser. Opening marks the article read; J/K go to the next and previous article, S stars, M toggles read, O opens the original and Escape goes back to the list.
- The timeline is the home screen: every subscription's entries in one list, newest first. Articles show a thumbnail and summary, videos a cover with the duration, social posts up to four pictures, and the Images tab is a masonry wall. Kind tabs, a subscription list, Unread and Starred filters and full-text search narrow the list; J/K, O, S, M, `/` and Cmd/Ctrl+F work from the keyboard. Entries can be starred, marked read or unread, and marked read in bulk after a confirmation. New entries are checked every minute and wait behind a "N new" button. Videos, social posts and pictures open in the browser and are marked read.
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
