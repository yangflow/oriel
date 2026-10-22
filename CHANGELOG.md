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

## [0.8.0](https://github.com/yangflow/oriel/compare/v0.7.0...v0.8.0) (2026-10-22)


### Added

* **backup:** back up and restore all data from Settings or the command line ([#298](https://github.com/yangflow/oriel/issues/298)) ([9ad2eb4](https://github.com/yangflow/oriel/commit/9ad2eb44970ac822df40352c5f6af328a7fd1435))
* **backup:** back up automatically to a folder every day or week ([#308](https://github.com/yangflow/oriel/issues/308)) ([fe2ca44](https://github.com/yangflow/oriel/commit/fe2ca4434e39f5074c5c4bc67eb5cc0f59dc01d9))
* **reader:** keep a local copy of starred entries' pictures and text ([#306](https://github.com/yangflow/oriel/issues/306)) ([990f68f](https://github.com/yangflow/oriel/commit/990f68feb45094a39221031cae4f6032b087b7b6))
* **reader:** one toolbar button style, a fixed reader toolbar and a solid bar once scrolled ([#310](https://github.com/yangflow/oriel/issues/310)) ([5031863](https://github.com/yangflow/oriel/commit/503186392a44ce6b16ec218de616eba3c8233509))
* **settings:** export a diagnostics file without secrets ([#294](https://github.com/yangflow/oriel/issues/294)) ([98bebe9](https://github.com/yangflow/oriel/commit/98bebe9fb06a8909940a6e002d082e1e8965ed28))
* **subscriptions:** explain when the public rsshub.app blocks a fetch and point to your own instance ([#307](https://github.com/yangflow/oriel/issues/307)) ([a2c109d](https://github.com/yangflow/oriel/commit/a2c109dfea5cbcf994e7c77342209f4339cdfa79))
* **subscriptions:** undo deleting a subscription ([#295](https://github.com/yangflow/oriel/issues/295)) ([fbdb2d2](https://github.com/yangflow/oriel/commit/fbdb2d231ad24d53d8e6fdd8c92cd39ad02045cd))
* **timeline:** drop the topic starter packs from the empty timeline ([#305](https://github.com/yangflow/oriel/issues/305)) ([7bafe40](https://github.com/yangflow/oriel/commit/7bafe404740950f4f2754a35ce594e23af693f39))
* **timeline:** undo a bulk mark as read only for the entries it marked ([#296](https://github.com/yangflow/oriel/issues/296)) ([56d227c](https://github.com/yangflow/oriel/commit/56d227c9c41d980e9ec0196b95333220ff0d5596))


### Fixed

* **ui:** drop tooltips that repeat a button's visible label ([#309](https://github.com/yangflow/oriel/issues/309)) ([95a4638](https://github.com/yangflow/oriel/commit/95a4638da3903260c7bc3b02839725b87573fd43))

## [0.7.0](https://github.com/yangflow/oriel/compare/v0.6.0...v0.7.0) (2026-10-22)


### Added

* **fetch:** check each subscription as often as it publishes, and suggest a kind after the first fetch ([#265](https://github.com/yangflow/oriel/issues/265)) ([593d9c9](https://github.com/yangflow/oriel/commit/593d9c957b9dcbda9a8cbaefc28ac003b7e1433b))
* **fetch:** fall back to another RSSHub instance when one fails ([#266](https://github.com/yangflow/oriel/issues/266)) ([f4f05c5](https://github.com/yangflow/oriel/commit/f4f05c577bf814df03f587c46cf336bc853d6016))
* **fetch:** get new entries within seconds from sources that support WebSub ([#281](https://github.com/yangflow/oriel/issues/281)) ([6b66bf2](https://github.com/yangflow/oriel/commit/6b66bf2a59937d62f0d859c369b63178fa4475cc))
* **reader:** copy an entry's link from its menu or the reader toolbar ([#270](https://github.com/yangflow/oriel/issues/270)) ([7f275d3](https://github.com/yangflow/oriel/commit/7f275d3b52cae1a6889ccd629b79738528978c71))
* **settings:** add presets for Ollama and LM Studio and a connection check for the AI endpoint ([#276](https://github.com/yangflow/oriel/issues/276)) ([cce6ed5](https://github.com/yangflow/oriel/commit/cce6ed520303329362eadd809d32180693e6d2bc))
* **settings:** import subscriptions, stars and read state from Miniflux and FreshRSS ([#291](https://github.com/yangflow/oriel/issues/291)) ([3d7c3b5](https://github.com/yangflow/oriel/commit/3d7c3b5d48ad415293053c958fbf782c4f2ecbe1))
* **settings:** see recent fetches and database size, and compact the database ([#282](https://github.com/yangflow/oriel/issues/282)) ([c0bc530](https://github.com/yangflow/oriel/commit/c0bc5306a083aa573c40da9fc2b5a9b6c18609df))
* **settings:** undo Restore default on an edited prompt ([#272](https://github.com/yangflow/oriel/issues/272)) ([057e774](https://github.com/yangflow/oriel/commit/057e7745cfa03da76fd9c5e4c35bbb4b6a185daf))
* **sidebar:** say when a new or refreshed subscription has finished its first fetch ([#275](https://github.com/yangflow/oriel/issues/275)) ([e822412](https://github.com/yangflow/oriel/commit/e822412964f78c448452e35737c83b0cdacd00dc))
* **subscriptions:** add several subscriptions without reopening the dialog ([#279](https://github.com/yangflow/oriel/issues/279)) ([45f7ae4](https://github.com/yangflow/oriel/commit/45f7ae4515150ddd17c930b5f9dff3929bc4883f))
* **subscriptions:** find the feed of any pasted website address ([#286](https://github.com/yangflow/oriel/issues/286)) ([8f6b260](https://github.com/yangflow/oriel/commit/8f6b2609f1fd78ca97093db8a2e33869f3653824))
* **subscriptions:** find the RSSHub route for a site and preview it before subscribing ([#289](https://github.com/yangflow/oriel/issues/289)) ([86e2a11](https://github.com/yangflow/oriel/commit/86e2a111dd0412d769400ef33a86d744ce7eb4ba))
* **subscriptions:** let a subscription set its own content selector and link rewrites for full text ([#290](https://github.com/yangflow/oriel/issues/290)) ([b3019d9](https://github.com/yangflow/oriel/commit/b3019d96694933e49c52dd0a561db249b669db6e))
* **subscriptions:** let each source set how long its entries stay in the timeline ([#288](https://github.com/yangflow/oriel/issues/288)) ([1a78d97](https://github.com/yangflow/oriel/commit/1a78d9767db68b65ce113806acdc4dde8ee94f2f))
* **subscriptions:** set how long each subscription keeps its entries ([#284](https://github.com/yangflow/oriel/issues/284)) ([a05a01c](https://github.com/yangflow/oriel/commit/a05a01c23f8380fe97622b8b07945eeaf6c646a5))
* **subscriptions:** show how many entries the mute keywords hide ([#280](https://github.com/yangflow/oriel/issues/280)) ([77e49ca](https://github.com/yangflow/oriel/commit/77e49ca195080fff64ea224de3ccc4954adcd64a))
* **subscriptions:** show which subscriptions are dead, failing or rarely read ([#277](https://github.com/yangflow/oriel/issues/277)) ([1840659](https://github.com/yangflow/oriel/commit/184065972c55d6f2857e8343be25bf682e0e7710))
* **timeline:** add keys for the filters, mark all read and loading new entries ([#273](https://github.com/yangflow/oriel/issues/273)) ([e109bd2](https://github.com/yangflow/oriel/commit/e109bd24b6317e434d5024655efdd3dcdb227dd9))
* **timeline:** move the sidebar button to the pane's corner and rearrange the toolbar ([#283](https://github.com/yangflow/oriel/issues/283)) ([8dfaacf](https://github.com/yangflow/oriel/commit/8dfaacf018d20d9a364c61cd510f616375cf9c53))
* **timeline:** offer topic starter packs on the empty first-run screen ([#287](https://github.com/yangflow/oriel/issues/287)) ([80512b9](https://github.com/yangflow/oriel/commit/80512b9ff5b998cd5d1505b882d4b5e096468725))
* **timeline:** recover faster from a dropped server and keep stars and reads made offline ([#285](https://github.com/yangflow/oriel/issues/285)) ([d0e21a0](https://github.com/yangflow/oriel/commit/d0e21a059a84b4e81e97f9ddf716416031f70ed2))
* **timeline:** refresh all subscriptions at once ([#274](https://github.com/yangflow/oriel/issues/274)) ([5cac8ff](https://github.com/yangflow/oriel/commit/5cac8ffd6a5c24d8a90a8618b0ea04eb4df036cd))
* **timeline:** reopen in the scope and filter you left on each server ([#257](https://github.com/yangflow/oriel/issues/257)) ([26517c2](https://github.com/yangflow/oriel/commit/26517c21cb7e77297d5369309bbe2b8ce979185d))
* **window:** reopen the window at the size and position you left it ([#268](https://github.com/yangflow/oriel/issues/268)) ([79a1b37](https://github.com/yangflow/oriel/commit/79a1b375e06089f1c86e49457999f8d34696e1c1))


### Fixed

* **connect:** announce the show-password button as a toggle with a pressed state ([#258](https://github.com/yangflow/oriel/issues/258)) ([7384286](https://github.com/yangflow/oriel/commit/73842860de6546918af0b09b3cea049491a26749))
* **fetch:** retry full-text extraction later when a site is briefly down ([#252](https://github.com/yangflow/oriel/issues/252)) ([8ed2df3](https://github.com/yangflow/oriel/commit/8ed2df3f702ce4294c0516a35edbf357b62a966a))
* **server:** slow down and limit repeated login attempts ([#251](https://github.com/yangflow/oriel/issues/251)) ([5e4fa7c](https://github.com/yangflow/oriel/commit/5e4fa7cc84b9bde9351876e2ccf7ed0543385cc1))
* **sidebar:** line up the add button in the Subscriptions header with the rows below it ([#269](https://github.com/yangflow/oriel/issues/269)) ([b86838e](https://github.com/yangflow/oriel/commit/b86838ea30d9af77e1de978fd7dfa6718c16b6f1))
* **store:** keep an article once when its feed changes the GUID, and refuse a duplicate subscription with a trailing slash ([#255](https://github.com/yangflow/oriel/issues/255)) ([ad99ab3](https://github.com/yangflow/oriel/commit/ad99ab319849939ef52e017d5bb1324f08082e82))
* **store:** wait for concurrent writes instead of failing with database is locked ([#250](https://github.com/yangflow/oriel/issues/250)) ([22985e9](https://github.com/yangflow/oriel/commit/22985e94544077f9f0888f621ce85d1c1cfcdddd))
* **timeline:** stop the skeleton flashing when you switch the filter or kind ([#278](https://github.com/yangflow/oriel/issues/278)) ([14a90b7](https://github.com/yangflow/oriel/commit/14a90b7c28b1c134aa318ba5dbe8115de5597352))


### Performance

* **fetch:** keep many subscriptions on schedule and space out requests to one host ([#256](https://github.com/yangflow/oriel/issues/256)) ([fcd7fc6](https://github.com/yangflow/oriel/commit/fcd7fc6f503a1d8fa3a99b3b5d4bca4203e0f072))
* **store:** count unread entries per subscription with an index ([#254](https://github.com/yangflow/oriel/issues/254)) ([f87d112](https://github.com/yangflow/oriel/commit/f87d11215f9102fdf2351692f234dfd145c4cce6))

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
