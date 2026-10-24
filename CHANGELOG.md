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

## [0.10.1](https://github.com/yangflow/oriel/compare/v0.10.0...v0.10.1) (2026-10-24)


### Fixed

* **fetch:** preserve JSON Feed podcast durations ([#478](https://github.com/yangflow/oriel/issues/478)) ([8876a8b](https://github.com/yangflow/oriel/commit/8876a8b7ce61b61cf0067b0e42e2653c0b3c6cd8))
* ignore late answers from a server the app has left ([#484](https://github.com/yangflow/oriel/issues/484)) ([eddc21e](https://github.com/yangflow/oriel/commit/eddc21e07d7401ce821ecb91ac2e63f7629e1790))
* **settings:** keep focus on a switch when a failure note appears above it ([#486](https://github.com/yangflow/oriel/issues/486)) ([6227cf5](https://github.com/yangflow/oriel/commit/6227cf57f83e2f500abf77925382ae9be7dcb8dd))
* **timeline:** keep checking for new entries after switching servers ([#483](https://github.com/yangflow/oriel/issues/483)) ([faa1841](https://github.com/yangflow/oriel/commit/faa18412fd81691474e31b41a9a0eb3ca04b3eed))

## [0.10.0](https://github.com/yangflow/oriel/compare/v0.9.0...v0.10.0) (2026-10-24)


### Added

* **desktop:** launch at login, menu bar or tray icon and Dock unread badge ([#464](https://github.com/yangflow/oriel/issues/464)) ([f7133d9](https://github.com/yangflow/oriel/commit/f7133d9cf1d43bfc1e2de72e6cf956545a5546d5))
* **import:** drop an OPML file onto the window to import it ([#340](https://github.com/yangflow/oriel/issues/340)) ([#443](https://github.com/yangflow/oriel/issues/443)) ([3348aca](https://github.com/yangflow/oriel/commit/3348acad7b7e429e77cc7b2200a083fd3147e6ee))
* **import:** show OPML import progress and offer View subscriptions when it ends ([#463](https://github.com/yangflow/oriel/issues/463)) ([06b709a](https://github.com/yangflow/oriel/commit/06b709ab4c84224f5596a8a39ef3f63390918417))
* **reader:** choose a sepia or mist page and a narrow, standard or wide line ([#459](https://github.com/yangflow/oriel/issues/459)) ([b338ab1](https://github.com/yangflow/oriel/commit/b338ab1e04d855a975673e439b4c247c4cb3dc03))
* **reader:** find in the article with Cmd/Ctrl+F ([#449](https://github.com/yangflow/oriel/issues/449)) ([c849b85](https://github.com/yangflow/oriel/commit/c849b8572ecfed0405f8ecedf4cf620d8457c113))
* **reader:** show a row for video, audio and embedded content, and open the entry from it ([#466](https://github.com/yangflow/oriel/issues/466)) ([26b0016](https://github.com/yangflow/oriel/commit/26b00169972b8c9eb08c1ce3dc7f0b9d7c4d64f9))
* **settings:** show each RSSHub instance's success rate and latency ([#338](https://github.com/yangflow/oriel/issues/338)) ([#462](https://github.com/yangflow/oriel/issues/462)) ([f74d921](https://github.com/yangflow/oriel/commit/f74d921a6026ee79ff1294a50f39b82f7faabbb3))
* **subscriptions:** let a subscription carry its own User-Agent and Referer ([#467](https://github.com/yangflow/oriel/issues/467)) ([1d24bdc](https://github.com/yangflow/oriel/commit/1d24bdc529d49ae93c5d8a5e2f2e278ce35d8707))
* **subscriptions:** manage several subscriptions at once ([#469](https://github.com/yangflow/oriel/issues/469)) ([4265e00](https://github.com/yangflow/oriel/commit/4265e003a09bb0341f75fedaf2afc01c634f1896))
* **subscriptions:** suggest the new address when a subscription is permanently redirected ([#465](https://github.com/yangflow/oriel/issues/465)) ([c7cfe42](https://github.com/yangflow/oriel/commit/c7cfe427f97fa304526df25b5ca1073ad5fef2f0))
* **timeline:** mark the entries below as read from the entry menu ([#445](https://github.com/yangflow/oriel/issues/445)) ([8f67d9e](https://github.com/yangflow/oriel/commit/8f67d9e7f5c96781da9e65ea3e1910b3564a9de6))
* **timeline:** search with phrases, exclusions and from:, kind:, after:, before: ([#458](https://github.com/yangflow/oriel/issues/458)) ([2ffaf94](https://github.com/yangflow/oriel/commit/2ffaf9442603ec90691bf6ed05d041f518812f38))


### Fixed

* **reader:** find in the article also matches the text in table cells ([#468](https://github.com/yangflow/oriel/issues/468)) ([667307c](https://github.com/yangflow/oriel/commit/667307c19438083637a080c7581bb0e1d3094e63))
* **reader:** keep failed pictures in the text with Try again, and settle pictures without jumps or crops ([#457](https://github.com/yangflow/oriel/issues/457)) ([f6c1094](https://github.com/yangflow/oriel/commit/f6c1094c823d2c79e072f0780b30ac66a9fc8f39))
* **reader:** show tables, footnotes, superscript and subscript as the article wrote them ([#460](https://github.com/yangflow/oriel/issues/460)) ([d7a3864](https://github.com/yangflow/oriel/commit/d7a386439723e4bf1eef2ef97168e3c7509964cd))

## [0.9.0](https://github.com/yangflow/oriel/compare/v0.8.0...v0.9.0) (2026-10-23)


### Added

* **ai:** cap automatic AI work per day and keep finished chunks on a retry ([#353](https://github.com/yangflow/oriel/issues/353)) ([740d746](https://github.com/yangflow/oriel/commit/740d746b10220516aafa1c5903ea16691cf91fbc))
* **diagnostics:** write a rotated log file and record panics with their stack ([#409](https://github.com/yangflow/oriel/issues/409)) ([a8070ad](https://github.com/yangflow/oriel/commit/a8070ad52f57819d641d707a9933d863b9b0c9cf))
* **fetch:** fetch feeds through a global HTTP or SOCKS5 proxy, with a per-source override ([#417](https://github.com/yangflow/oriel/issues/417)) ([65e896b](https://github.com/yangflow/oriel/commit/65e896b5b64b9ccd5ec845a520e7a01f2892f8fd))
* **reader:** immersive reading that fills the window while J and K keep stepping ([#437](https://github.com/yangflow/oriel/issues/437)) ([32cd0db](https://github.com/yangflow/oriel/commit/32cd0db03f32c45398810e73c2954a9afa069f67))
* **rules:** star or mark read new entries by rule, and mute keywords for every source ([#394](https://github.com/yangflow/oriel/issues/394)) ([62a3fdd](https://github.com/yangflow/oriel/commit/62a3fdd5c8c0230d8ccedad89d56de286ba0f333))
* **serve:** run Oriel in Docker or systemd, with compose and unit examples and oriel healthcheck ([#419](https://github.com/yangflow/oriel/issues/419)) ([0b02040](https://github.com/yangflow/oriel/commit/0b02040a360906894749a3cbdbfffb126f4dab05))
* **store:** copy the database before running schema migrations ([#408](https://github.com/yangflow/oriel/issues/408)) ([77a6948](https://github.com/yangflow/oriel/commit/77a69488fadc6f1403ede786a0df8b00ef5eea06))
* **subscriptions:** detect a duplicate subscription across http and https, www and RSSHub routes ([#414](https://github.com/yangflow/oriel/issues/414)) ([d37c8ff](https://github.com/yangflow/oriel/commit/d37c8ffaee6840eaba68b4d67ec5d7d4c4a65577))
* **timeline:** choose when entries are marked as read ([#438](https://github.com/yangflow/oriel/issues/438)) ([e5c7b93](https://github.com/yangflow/oriel/commit/e5c7b93b65df2236668a0d35b5eea2bbf021e377))
* **timeline:** read the full name of a cut-short subscription or toast title in a tooltip ([#442](https://github.com/yangflow/oriel/issues/442)) ([1e03592](https://github.com/yangflow/oriel/commit/1e03592248b846a5de09459e66ba08343763d13c))
* **timeline:** show an article once when several sources carry it ([#381](https://github.com/yangflow/oriel/issues/381)) ([3a36fc2](https://github.com/yangflow/oriel/commit/3a36fc2821f3ae9f69152c68d1bca5287238a39c))
* **ui:** show why a fetch failed as a short phrase, with the original text in the tooltip ([#436](https://github.com/yangflow/oriel/issues/436)) ([04f7805](https://github.com/yangflow/oriel/commit/04f78056d13794a32a7e741ebf4290775dd93c08))


### Fixed

* **ai:** keep summaries and translations from failing or finishing wrongly when the AI endpoint is slow, cut off or down ([#397](https://github.com/yangflow/oriel/issues/397)) ([53c37ba](https://github.com/yangflow/oriel/commit/53c37baca087d1d1a9b1e395369ab9e38784ec2f))
* **ai:** treat an entry of a deleted source as missing in the AI result route, and fill test gaps ([#416](https://github.com/yangflow/oriel/issues/416)) ([6f70de0](https://github.com/yangflow/oriel/commit/6f70de0ec462e294e8546be017c8681cc5d935f6))
* **fetch:** read feeds and OPML files in other encodings, and count extraction waits from the failure ([#398](https://github.com/yangflow/oriel/issues/398)) ([ea2e939](https://github.com/yangflow/oriel/commit/ea2e9393985982a33750a2fb460bf86b49cda7d3))
* **fetch:** retry dead feeds every 6 hours and treat 410 Gone as removed ([#411](https://github.com/yangflow/oriel/issues/411)) ([48ea187](https://github.com/yangflow/oriel/commit/48ea187afbbd60a3fef205b03c54af4dfd9ea28e))
* **reader:** the toolbar lines up with the article column when the sidebar is hidden ([#422](https://github.com/yangflow/oriel/issues/422)) ([9ae6b0a](https://github.com/yangflow/oriel/commit/9ae6b0aca16f9aaa8a5cc959020c5a40489b6a30))
* **search:** find words of one or two characters, such as Chinese two-character words ([#413](https://github.com/yangflow/oriel/issues/413)) ([cf2c7b5](https://github.com/yangflow/oriel/commit/cf2c7b58dce082d0f938169d67fbb5468adecf91))
* **server:** finish cut-off requests before closing the database on quit, and keep the backup name from doubling its extension ([#395](https://github.com/yangflow/oriel/issues/395)) ([cb9ab78](https://github.com/yangflow/oriel/commit/cb9ab78ba8a229414d8ea56ef375e59d2d16dd51))
* **server:** lighter health check and a login limit that holds behind a reverse proxy ([#396](https://github.com/yangflow/oriel/issues/396)) ([8c6f740](https://github.com/yangflow/oriel/commit/8c6f74070a5433b0436e23129b732c529a34bcbd))
* **server:** reject requests from other origins ([#355](https://github.com/yangflow/oriel/issues/355)) ([814c7a8](https://github.com/yangflow/oriel/commit/814c7a8c8693ff7c8f54bd5807a7066addf98c03))
* **settings:** Escape no longer drops typed or unsaved settings ([#421](https://github.com/yangflow/oriel/issues/421)) ([807092d](https://github.com/yangflow/oriel/commit/807092d6f58d26b44c4935eba67e2f2aa736f5fe))
* **sidebar:** the icon buttons at the right of the sidebar sit on one line ([#425](https://github.com/yangflow/oriel/issues/425)) ([be026cf](https://github.com/yangflow/oriel/commit/be026cf0260d77dc76a2f02b835b5fc16038ba2f))
* **sidebar:** unread counts above 999 read 999+ so the count column stays narrow ([#433](https://github.com/yangflow/oriel/issues/433)) ([cea66be](https://github.com/yangflow/oriel/commit/cea66beab9b213921a390067eb57e888e3b733ea))
* **timeline:** mark the entries scrolled past once the list rests, with any font size ([#440](https://github.com/yangflow/oriel/issues/440)) ([6838b23](https://github.com/yangflow/oriel/commit/6838b23a6e6cc360bf64d33b00d61a33689b75d7))
* **timeline:** say in the order menu that new entries come at the end when listing oldest first ([#435](https://github.com/yangflow/oriel/issues/435)) ([c436088](https://github.com/yangflow/oriel/commit/c436088bde5474de60db80ed0c941953cf08fbfa))
* **ui:** make clear, close and other small buttons at least 24×24 to click ([#428](https://github.com/yangflow/oriel/issues/428)) ([42ab615](https://github.com/yangflow/oriel/commit/42ab615b96ed1b08a8adf777dbe191a5f9ee0f8c))
* **ui:** make the interface copy consistent across screens ([#431](https://github.com/yangflow/oriel/issues/431)) ([836cb69](https://github.com/yangflow/oriel/commit/836cb69f21f19643feb40c5c3253f0cf0b76533f))
* **ui:** make the keyboard focus ring visible in the light theme ([#423](https://github.com/yangflow/oriel/issues/423)) ([77813d4](https://github.com/yangflow/oriel/commit/77813d464fcf1c46aea4cc5b0271a47c94f8d637))
* **ui:** make unselected segment labels, the no-key badge and refused result boxes readable ([#424](https://github.com/yangflow/oriel/issues/424)) ([7a7f9a5](https://github.com/yangflow/oriel/commit/7a7f9a569d5da7e359e42ba928519db2c0178e75))
* **ui:** say "1 entry" and "1 subscription" in English instead of the plural ([#420](https://github.com/yangflow/oriel/issues/420)) ([5626097](https://github.com/yangflow/oriel/commit/5626097db44b2496f8bc13e9c21b2be9193b0818))
* **ui:** stop picture downloads when you open another article or switch server, and retry failed pictures ([#426](https://github.com/yangflow/oriel/issues/426)) ([bf1c3fc](https://github.com/yangflow/oriel/commit/bf1c3fc2433f13a25f6bedfdbd1065613cc17c98))
* **ui:** write relative times the same way on every screen and follow the system 12/24-hour clock ([#430](https://github.com/yangflow/oriel/issues/430)) ([45d1c94](https://github.com/yangflow/oriel/commit/45d1c94dfaba02bfa29366962fd52a5f969eb7de))


### Performance

* **ai:** stop the 15-second AI queue from scanning every finished result ([#404](https://github.com/yangflow/oriel/issues/404)) ([c6ef1ec](https://github.com/yangflow/oriel/commit/c6ef1ecc34881da8f3d5114638f58d697a88a781))
* **extract:** stop the 5-second extraction round from scanning every entry ([#401](https://github.com/yangflow/oriel/issues/401)) ([84af943](https://github.com/yangflow/oriel/commit/84af943909c82ea480448cdb0ccc080d8bd4ef73))
* **fetch:** fetching no longer stalls while the due list is read from the database ([#406](https://github.com/yangflow/oriel/issues/406)) ([ad7dd28](https://github.com/yangflow/oriel/commit/ad7dd282f09ad98ee3311e07ff48f7188b44096c))
* **images:** fetch a picture once when several requests ask for it, and cap simultaneous downloads ([#403](https://github.com/yangflow/oriel/issues/403)) ([61fcabe](https://github.com/yangflow/oriel/commit/61fcabe61f8e60106181e9700f4d036f88e02f82))
* **images:** load pictures faster when many are requested at once ([#402](https://github.com/yangflow/oriel/issues/402)) ([fca7ae1](https://github.com/yangflow/oriel/commit/fca7ae1a9b41c1f4dd44849dcebb693a48e1fc20))
* **retention:** prune expired entries in bounded batches so other writes are not blocked ([#399](https://github.com/yangflow/oriel/issues/399)) ([efc6d02](https://github.com/yangflow/oriel/commit/efc6d02b5fde73ec5461fe107779409d837b40f1))
* **sidebar:** the subscription list stays smooth with many subscriptions ([#429](https://github.com/yangflow/oriel/issues/429)) ([f66d0cd](https://github.com/yangflow/oriel/commit/f66d0cd8923fe872d628d02381383abfdbfe9f64))
* **sources:** load the sidebar's unread counts faster ([#407](https://github.com/yangflow/oriel/issues/407)) ([1f59f9b](https://github.com/yangflow/oriel/commit/1f59f9b5cb0ae8bcd8e966fca02b6cf08d403845))
* **store:** save a fetched feed faster so other writes wait less ([#400](https://github.com/yangflow/oriel/issues/400)) ([7306229](https://github.com/yangflow/oriel/commit/730622917b5e4855ce15789cd05573198d2a54c5))
* **store:** shrink the database file after a large prune and optimize it on start and exit ([#405](https://github.com/yangflow/oriel/issues/405)) ([ef02cc5](https://github.com/yangflow/oriel/commit/ef02cc59a305de061a1b3a332ae7c3cb6ec472b6))

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
