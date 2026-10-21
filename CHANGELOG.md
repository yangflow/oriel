# Changelog

release-please writes each release's section from the titles of the pull requests merged since the last one
(docs/release.md); do not edit it by hand.

### Added

- "Mark all as read" acts at once and shows a toast with Undo for 8 seconds; Undo or Cmd/Ctrl+Z marks those entries unread again. `POST /api/entries/unread` marks entries unread by id.
- N goes to the next unread entry below the selected one, loading the next page when needed and saying "No unread entries below" when there is none; in the reader it opens the next unread article.
- A keyboard shortcuts sheet, from `?`, Cmd/Ctrl+/ or "Keyboard Shortcuts" in the new Help menu, lists the keys of the timeline, the reader and the whole window.
- Subscriptions show their site's icon in the sidebar, the entry rows, the reader and the edit dialog, fetched in the background when a feed is checked; the monogram stays where there is none.

### Changed

- "Mark all as read" no longer asks for confirmation, since it can be undone.
- The kinds at the top of the sidebar are a list, each with its icon, name and unread count, instead of a row of tabs.
- "Mark all as read" says what it covers, such as "Mark all in “Video” as read".
- Entry rows are as tall as their text; the thumbnail follows the row, up to 64 points.
- One look per selection: the place you are in the sidebar or Settings is tinted in the accent with its icon and count in the accent; the chosen segment of a control is a raised face, the translation switch in the reader too.
- Chinese titles, summaries and articles get a taller line height, and unread counts line up with tabular figures.
- In dark, the accent is softer and the window a shade deeper, and Settings has more room between groups, rows and their help.
- Disabled buttons keep a readable label on a plain fill instead of a faded accent.
- In Settings, the AI model field sits under its heading across the card, and all fields and text areas share one fill, turning to the background while focused.
- The add and edit dialogs keep their title and buttons in place while the form scrolls, with a divider showing where more is hidden.

## [0.6.0](https://github.com/yangflow/oriel/compare/v0.5.0...v0.6.0) (2026-10-21)


### Added

* detect the language of each entry and skip translating entries already in the target language ([#225](https://github.com/yangflow/oriel/issues/225)) ([f9e06d2](https://github.com/yangflow/oriel/commit/f9e06d2abe0e0318d653f1c5aa3af674e22e1a63))
* **reader:** choose a reading font and widen the column with the text size ([#231](https://github.com/yangflow/oriel/issues/231)) ([2de2795](https://github.com/yangflow/oriel/commit/2de2795473e3c5c6387ac63f0e832c6a0b07a393))
* **reader:** copy or save an article as Markdown ([#245](https://github.com/yangflow/oriel/issues/245)) ([9255a87](https://github.com/yangflow/oriel/commit/9255a8742095a4ea6b03b94317981275c0a2946f))
* **reader:** fetch the full text again and switch between full text and feed text ([#238](https://github.com/yangflow/oriel/issues/238)) ([c845775](https://github.com/yangflow/oriel/commit/c845775401cfbcca3e7dd50971529d339f5bcdb2))
* **reader:** reopen a long article where you left off ([#235](https://github.com/yangflow/oriel/issues/235)) ([91002f1](https://github.com/yangflow/oriel/commit/91002f12667628992761ee7571bf0083861bcf91))
* **reader:** space Chinese and Latin text apart and tighten runs of punctuation ([#232](https://github.com/yangflow/oriel/issues/232)) ([17c7661](https://github.com/yangflow/oriel/commit/17c76615363fcefc0778821055058a9f25bdccc0))
* **reader:** translate or explain the selected text ([#239](https://github.com/yangflow/oriel/issues/239)) ([83512b0](https://github.com/yangflow/oriel/commit/83512b098cd2bec19c53e55c0aefa99ca1e1af21))
* **settings:** find an interface font by typing, with each name shown in its own face ([#230](https://github.com/yangflow/oriel/issues/230)) ([59630f9](https://github.com/yangflow/oriel/commit/59630f95be36fab0caf384a54e5a451c1c4a2ece))
* **timeline:** choose how each kind of entry is shown ([#234](https://github.com/yangflow/oriel/issues/234)) ([254d3fe](https://github.com/yangflow/oriel/commit/254d3feed6778cf94586a124d809760728c642e4))
* **timeline:** collapse the sidebar with a button or Cmd/Ctrl+\ ([#242](https://github.com/yangflow/oriel/issues/242)) ([80a7fe2](https://github.com/yangflow/oriel/commit/80a7fe2795eefad62cdc0619ed8d3f40166678c7))
* **timeline:** filter subscriptions by name and sort unread first ([#244](https://github.com/yangflow/oriel/issues/244)) ([a763075](https://github.com/yangflow/oriel/commit/a7630752ce4a2114c200b6ce6e63c58244622cb5))
* **timeline:** jump to any subscription, kind or setting from a command palette ([#240](https://github.com/yangflow/oriel/issues/240)) ([74f4f9f](https://github.com/yangflow/oriel/commit/74f4f9f7757596b7fb24c0b4796907c07ff14d14))
* **timeline:** keep the list, scroll position and selection when switching back to a recent scope or filter ([#227](https://github.com/yangflow/oriel/issues/227)) ([8dadf66](https://github.com/yangflow/oriel/commit/8dadf668ce2275b14fbb3d692d4ca8f77747a4e4))
* **timeline:** mark entries older than a day, a week or a month as read ([#241](https://github.com/yangflow/oriel/issues/241)) ([4296656](https://github.com/yangflow/oriel/commit/42966560b178af01f40eca6fedc7fd00c0afe74c))
* **timeline:** mark where you last looked with a divider and a count of new entries ([#246](https://github.com/yangflow/oriel/issues/246)) ([3f92a0c](https://github.com/yangflow/oriel/commit/3f92a0c9ff41079b9829781ce404174489141758))
* **timeline:** read oldest first and choose a compact list density ([#248](https://github.com/yangflow/oriel/issues/248)) ([61ba1d8](https://github.com/yangflow/oriel/commit/61ba1d872d0ae96abf9861044258ff303cd1d29b))
* **timeline:** show how long each entry takes to read and filter by a time budget ([#233](https://github.com/yangflow/oriel/issues/233)) ([e5abe62](https://github.com/yangflow/oriel/commit/e5abe620760f497e71c0acce4cd7c07fbb7f17a4))
* **timeline:** show the AI summary in an article's row when one exists ([#237](https://github.com/yangflow/oriel/issues/237)) ([063f155](https://github.com/yangflow/oriel/commit/063f1558c002a2252073a40f9dfaa1a4cc2a9663))
* **timeline:** switch videos and image sources between a list and a grid ([#236](https://github.com/yangflow/oriel/issues/236)) ([6646a8b](https://github.com/yangflow/oriel/commit/6646a8b07fa60f5afb5ca54e9722a5f28426e5a8))
* **timeline:** view the pictures of a social or image entry inside the app ([#247](https://github.com/yangflow/oriel/issues/247)) ([d322f93](https://github.com/yangflow/oriel/commit/d322f93658924361948e4462f221e202cc36f44a))


### Fixed

* **reader:** skip hidden pictures and known tracking pixels in entries ([#163](https://github.com/yangflow/oriel/issues/163)) ([f3711e2](https://github.com/yangflow/oriel/commit/f3711e28b6a722713674288768880ab1582b7744))
* **ui:** stop long titles at two lines and show full subscription names on hover ([#121](https://github.com/yangflow/oriel/issues/121)) ([abeba62](https://github.com/yangflow/oriel/commit/abeba621caad81bf0e663817fba0375fabe56772))


### Performance

* **reader:** prefetch the previous and next entries so J and K open at once ([#228](https://github.com/yangflow/oriel/issues/228)) ([8ea326c](https://github.com/yangflow/oriel/commit/8ea326c0598a101db4263d5617843ae14d210a3d))
* **server:** keep proxied images on disk so scrolling back does not fetch them again ([#229](https://github.com/yangflow/oriel/issues/229)) ([c87926f](https://github.com/yangflow/oriel/commit/c87926f0496e8b37cd53d8dfc5dda6d330e0e0d7))
* **store:** read about one page for the unread and kind timelines ([#226](https://github.com/yangflow/oriel/issues/226)) ([6f4deaf](https://github.com/yangflow/oriel/commit/6f4deaf2baa2a66ae0894aedc0bc1332eb0f75d1))

## [0.5.0](https://github.com/yangflow/oriel/compare/v0.4.0...v0.5.0) (2026-10-20)


### Added

* **ui:** mark the entries above, or a whole subscription, as read from the context menu ([#112](https://github.com/yangflow/oriel/issues/112)) ([34b1d5e](https://github.com/yangflow/oriel/commit/34b1d5eca50b715899d733ea1e7db30d44da02be))
* **ui:** offer OPML import and name the links that work on first run ([#109](https://github.com/yangflow/oriel/issues/109)) ([d5bfaa1](https://github.com/yangflow/oriel/commit/d5bfaa1a15ce58be991e9be56565e1736c5138f8))
* **ui:** press V to mark the visible entries as read and move past them ([#114](https://github.com/yangflow/oriel/issues/114)) ([7a4afe4](https://github.com/yangflow/oriel/commit/7a4afe4b218b452e5eca3d4dea0d00d14d669cc7))
* **ui:** say when the server can't be reached and the list is stale ([#108](https://github.com/yangflow/oriel/issues/108)) ([cdc0729](https://github.com/yangflow/oriel/commit/cdc0729801f05b32ba8cd249074ffae5484695a5))
* **ui:** switch scope and reach the sidebar from the keyboard ([#113](https://github.com/yangflow/oriel/issues/113)) ([97abaf1](https://github.com/yangflow/oriel/commit/97abaf17089117a4d4ea5f7d6fc0b985638d0251))
* **ui:** use the room of a wide window in Settings ([#117](https://github.com/yangflow/oriel/issues/117)) ([c89fa18](https://github.com/yangflow/oriel/commit/c89fa18ef13794a36c0e1764076525ac02dce2d5))


### Fixed

* **fetch:** wait as long as a rate-limited feed asks before retrying ([#110](https://github.com/yangflow/oriel/issues/110)) ([060dc47](https://github.com/yangflow/oriel/commit/060dc47406a7d7bf7250e7e4f1163dfa4f278ac1))
* **ui:** keep the list keys working after clicking a scope in the sidebar ([#116](https://github.com/yangflow/oriel/issues/116)) ([f6c5dfc](https://github.com/yangflow/oriel/commit/f6c5dfce672e543e6ab91ce586877ff1dc7bcf20))
* **ui:** keep your place when the interface language changes ([#105](https://github.com/yangflow/oriel/issues/105)) ([0c54e13](https://github.com/yangflow/oriel/commit/0c54e1336f094594a8e79cfce761105e903bb29a))
* **ui:** show why a subscription is failing and list the failing ones ([#107](https://github.com/yangflow/oriel/issues/107)) ([8954037](https://github.com/yangflow/oriel/commit/8954037e0194c5e4e8b0cf018ef1f2f40f8560e2))
* **ui:** use one set of Chinese terms and quotes across the screens ([#118](https://github.com/yangflow/oriel/issues/118)) ([e4fbcec](https://github.com/yangflow/oriel/commit/e4fbcec0f502a4d95901202d1d989404ba903b1a))

## [0.4.0](https://github.com/yangflow/oriel/compare/v0.3.0...v0.4.0) (2026-10-20)


### Added

* **settings:** appearance options for theme, language, font and reading size ([#63](https://github.com/yangflow/oriel/issues/63)) ([716eba8](https://github.com/yangflow/oriel/commit/716eba846423edfb90160b4482eb20ae898f57b8))
* **ui:** ink pane, graphite dark and sans reading across every screen ([#103](https://github.com/yangflow/oriel/issues/103)) ([78a6b75](https://github.com/yangflow/oriel/commit/78a6b75a2625d78fbaa694e1dfa7b886e11beff5))

## [0.3.0](https://github.com/yangflow/oriel/compare/v0.2.0...v0.3.0) (2026-10-19)


### Added

* **ui:** paper-and-ink palette with fading row fills ([#60](https://github.com/yangflow/oriel/issues/60)) ([b52cdb5](https://github.com/yangflow/oriel/commit/b52cdb53a38e4888ee9c8d9c963d654393be1985))


### Fixed

* **ui:** hide the title bar and draw the chrome in Liquid Glass ([#59](https://github.com/yangflow/oriel/issues/59)) ([597a046](https://github.com/yangflow/oriel/commit/597a0462b4886c3312cb6920063cf9761d40e30b))

## [0.2.0](https://github.com/yangflow/oriel/compare/v0.1.0...v0.2.0) (2026-10-19)


### Added

* **ui:** kinds list, scoped mark all, one selection look and calmer spacing ([#51](https://github.com/yangflow/oriel/issues/51)) ([25690b7](https://github.com/yangflow/oriel/commit/25690b783c7b9b99a389583c992609951fee9817))
* **ui:** undo mark all read, key sheet, next unread and site icons ([#42](https://github.com/yangflow/oriel/issues/42)) ([1182668](https://github.com/yangflow/oriel/commit/1182668cf5c6c1e043acd782ffd3446ca6a85829))

## [0.1.0] - 2026-10-19

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
