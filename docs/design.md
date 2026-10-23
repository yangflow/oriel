# Oriel design language

This is the design language of Oriel: the rules every screen follows, and the place to record where a screen
departs from them. Oriel is a single-user reader that mixes RSS and RSSHub subscriptions into one timeline of
articles, videos, social posts and images (see [product.md](product.md), in Chinese). The same program is the
desktop app and, with `oriel serve`, a headless server; only the desktop app has a UI, and that UI reads and writes
through the HTTP API.

The UI is built on the native toolkit of [MyGo](https://github.com/egoist/mygo). This document summarizes the parts
of its [UI guides](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/README.md) that matter for design; the guides
stay the authority on API details. Links point to MyGo v0.5.5, the version pinned in `go.mod`. When the pin moves,
check the pages again and update the links and this document together.

Five screens are built: the connect screen in `internal/ui/connect/` (mockup `design/screens/connect.html`), which
connects the desktop app to a remote server, the timeline in `internal/ui/timeline/` (mockup
`design/screens/timeline.html`), which is home, the subscription dialogs over it in `internal/ui/subscriptions/`
(mockup `design/screens/subscriptions.html`), the reader in `internal/ui/reader/` (mockup
`design/screens/reader.html`), which takes the list's place for an article, and the settings in
`internal/ui/settings/` (mockup `design/screens/settings.html`), which take the whole window.

Related files: [design/README.md](../design/README.md) (mockups and how a UI change is made) and
[design/tokens.md](../design/tokens.md) (spacing, size and color values in one table).

## Contents

1. [Principles](#principles)
2. [Layout and spacing](#layout-and-spacing)
3. [Color and dark mode](#color-and-dark-mode)
4. [Typography](#typography)
5. [Chinese typesetting](#chinese-typesetting)
6. [Icons and images](#icons-and-images)
7. [Component guide](#component-guide)
8. [Windows, menus and navigation](#windows-menus-and-navigation)
9. [Accessibility checklist](#accessibility-checklist)
10. [Motion](#motion)
11. [Localization](#localization)
12. [Design acceptance checklist](#design-acceptance-checklist)
13. [Polish checklist](#polish-checklist)
14. [References](#references)

## Principles

- **Native first.** Use MyGo's native widgets and the theme defaults as they are, and follow the system's light and
  dark appearance (unless the user chose one in Settings, Appearance) and its text size. A control that looks and behaves like the platform's is learned already. Style
  an element yourself only when no widget does the job, and build it on the
  [custom widget](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/custom-widgets.md) bases so focus, keyboard and
  accessibility stay intact. No WebView and no HTML frontend.
- **Consistent across platforms, respectful of each.** One view serves macOS, Windows and Linux. Follow each
  platform's conventions where MyGo exposes them: menu roles, the accent color, the system font, shortcuts that use
  `CmdOrCtrl`. Do not add chrome that imitates one platform on the others.
- **Content first.** Oriel is for reading. The timeline and the reader give the room to titles, text and images;
  controls stay quiet and secondary until they are needed. An ink pane for the sidebar, paper for the content, one
  accent, and fewer lines, fills and sizes than the toolkit's defaults.
- **Accessible by default.** Every element can be reached with the keyboard, named for a screen reader, read in both
  appearances, and still works at larger text sizes. See the [checklist](#accessibility-checklist).
- **Almost no custom motion.** State changes take effect at once; only state fills, text color and the row actions
  fade, in color and opacity, never longer than 200 ms. See [Motion](#motion).
- **Meaning is never carried by color alone.** Say it in words; pair color with text, a symbol or a shape.
- **The UI thread draws.** A frame is built from in-memory state. No disk, network or other blocking work in a view,
  including calls to Oriel's own HTTP API and decoding images; start that work in the background and change the state
  back on the UI thread (see [Views](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/views.md)).
- **Spec before code.** Every screen is specified in its mockup in `design/` and in this document before it is
  written. Only `design/` and this document count as the design reference for Oriel.

## Layout and spacing

Layout uses flexbox in device-independent pixels (DIPs); see
[Layout](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/layout.md).

- **One spacing unit.** The theme's `Spacing` (4 DIPs by default; 3 is compact, 5 roomy) is the unit of every widget's
  padding and gap. Size your own elements with `t.Space(n)` so they follow it. Do not write raw pixel numbers for
  spacing.
- **Scale.** Use whole multiples of the unit: 1 (4) between a label and its control, 2 (8) between related controls
  and between buttons, 4 (16) between groups, 6 (24) for the window's outer padding. Larger gaps need a reason, for
  example a section break.
- **Structure from containers.** `Column` and `Row` with `Gap` and `Padding`; `Grow(1)` and `Spacer` for free space;
  `Grid` for forms and aligned tables of controls. Prefer `Gap` to margins on individual children.
- **Sizes are bounds.** Give windows a minimum size and let content reflow; do not fix a width a translation can
  overflow. Use `MinWidth`, `MaxWidth` and `Grow` instead of fixed widths, except for square symbol buttons.
- **Alignment.** Left-align text and labels; center only isolated focal content, as an empty state. Reading
  text in the article reader gets a comfortable maximum line length (`MaxWidth`) instead of spanning a wide window.
- **Scrolling.** Content that can exceed the window goes in a
  [scroll view](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/scroll.md). The timeline is a
  [list](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/list.md), which builds only the rows in view and lets rows
  of different kinds have different heights.
- **The connect screen.** A sheet at most `Space(80)` (320) wide, centered in a scroll view with outer padding
  `Space(6)`; `Space(6)` between the header, the form and the buttons, `Space(2)` between fields, `Space(1.5)` (6)
  between a field and its error. Fields and buttons are `Space(8)` (32) high; the two buttons share the width
  equally (both `theme.Button`, the primary one with its arrow after the text or a spinner while it connects). The password field's eye button is a toggle button: its name stays "Show password" and its pressed
  state says whether the password shows, while its icon flips between eye and eye-off.
- **The timeline.** An ink pane and the main column. The pane is 216 wide (184 below a 760 wide window), an opaque
  pane of ink (warm charcoal in light, deep teal in dark) floating 8 from the window's top, bottom and left edges, radius 14, with no divider; it is drawn with its
  own light-on-dark theme (see [Glass and ink](#windows-menus-and-navigation)); its first 40 are left to the window
  buttons. Then the five kinds (All, Articles, Videos, Social, Images) as 30 high rows, each an icon, its name and its
  unread count (11 type, tabular; above 999 it reads "999+", so the column never grows past that text and is not fixed, and the full number stays in the row's accessible description; `many-feeds` shows one); then the subscriptions of the chosen kind, 30 high rows, their counts capped the same way; at the bottom the server bar, 40 high. The main
  column is the paper: `Background`. It has a 52 high toolbar row on a solid `Background` bar (see Glass and ink), with no divider at the top and a hairline once the list has scrolled under it, in three parts: the title and its count at the leading end,
  one capsule with the Everything/Unread/Starred segments in the middle, and at the trailing end an evenly spaced
  group of bare icon buttons (the reading-time button, the grid switch (only where it applies), mark all as read, whose name and
  tooltip say its scope: "Mark all in “Videos” as read"; right click opens its menu, see Components), 4 apart, with search
  last, at the far right. The segments are one 32 high capsule of glass on the bar; the entries scroll under the bar,
  not through it. The title's part and the trailing part take equal room, so the segments are centered in the main
  column, whatever the title or the buttons are. Search is a flat ink field 200 wide; it gives up width before anything else,
  down to 48 (the magnifier and a letter), when the column is tight. Under a 736 wide main column (the column, not the window, so
  hiding the sidebar gives room back) search is a 32 square magnifier button after the other buttons; clicked, it opens the field
  over the buttons and takes the focus, and the field closes again when it loses the focus with no text in it. Under 580 the gap
  between the toolbar items is 4, the segments are padded 7 and the unread count is left out; the title is then cut with an
  ellipsis, 40 wide at least; the buttons never shrink, so mark all as read is always inside the window. The Tab order is the visual one:
  the sidebar button, the segments, the buttons, search. The reading-time button is a bare 32 square clock icon right of the capsule, named "Reading time: any
  length"; its menu lists Any length (the default), Up to 5, 10, 15 and 30 minutes, each a 28 high menu row with a
  check on the current one. With a budget chosen it becomes a 32 high button (radius 8) with the `Soft(Accent)` fill,
  "≤ 10 min" in `Accent` at 12 medium beside the icon, tabular numerals, named "Reading time: up to 10 minutes"; below
  a 760 wide window it keeps only the icon, with a 7 `Accent` dot at its top right. It is a view filter: it does not
  mark or move anything, the title's unread count is hidden while it is on, and mark all as read, mark above and V are
  disabled as under Starred. The list order has no button of its own, at any width: "Newest first" and "Oldest first"
  are the last two rows of the reading-time menu, under a hairline, with a check on the current one; choosing one
  announces its name. While it is oldest first the clock gets the same `Soft(Accent)` fill and `Accent` icon as with a
  budget (a bare 32 circle, no text beside it unless a budget is chosen too, and the dot below 760), and its name
  adds ". Oldest first: new entries appear at the end of the list". The menu is native and its rows have one line, so
  under the two orders sits a disabled row with that same sentence (muted by the system, wrapping to two lines in a narrow menu), and the
  sentence is also the clock's tooltip and screen-reader name; the "N new" pill never shows in this order and the note
  is why. The list goes back to its top, the oldest entry, and the "N new" pill is not shown while it
  is on (new entries appear when paging reaches the end). The order is kept per device. Drawn in `design/screens/timeline.html`, `order-oldest`. Compact list density (Settings,
  Appearance, Timeline) tightens only the spacing of rows, grid cells and wall tiles, by the values in
  `design/tokens.md`; no content is dropped. Drawn in `design/screens/timeline.html`, `dense`. When a background check fails after the list loaded, a quiet 28 high status line sits under the toolbar,
  lined up with the list: a 14 `Warning` wifi-off icon and 12 `TextMuted` text, "Can't reach the server · last updated
  10:42 AM", with no fill; it is a polite status, announced once, and goes away on the next successful check. At its end sits a quiet text button, "Try again" (12 `Accent`, 22 high, 8 padding, radius 6, hover fill like other quiet buttons); while a check runs it reads "Retrying…" in `TextMuted` and is disabled. A star or read change made while the server is out of reach stays on the row and is not rolled back; until it is sent, the row's meta line carries a 13 `TextMuted` upload icon after the star, named "Waiting to save". The queue lives in memory only and is lost on quit. Drawn in `design/screens/timeline.html`, `offline`, `offline-retrying`, `offline-pending`. Loading the first page of a list (a switch of scope, filter or kind, a reload) draws the skeleton only after 300 ms; an answer that comes sooner shows no loading state, so switching never flashes bars. With no subscriptions the list is an empty state: a 48 tile, a
  15 semibold heading, a hint at most 320 wide that names the links Oriel recognises, then a row of two buttons 8 apart,
  wrapping when narrow: the primary "Add your first subscription" and the secondary "Import OPML…" and a second secondary "Restore from backup…", both of which open
  Settings on Import and export. Drawn in `design/screens/timeline.html`, `no-subscriptions`. When a reading-time budget leaves nothing, the list shows the same pattern: a 48
  tile with a clock, "Nothing fits in 10 minutes", the hint "Entries with no estimate, like pictures, are left out.",
  and one secondary button, "Show any length", which clears the budget. Article, social and mixed rows sit in one column at most 680 wide (720 for Social), centered, padded 24
  at the sides; a row is as high as its text, padded 16 with 14 between its parts, radius 12 when it has a fill, and
  an article's thumbnail, at most 96 by 64, radius 8, starts under the meta line and never makes the row taller. A
  row reads, top to bottom: the meta line (20 high: the site icon, the source name, a dot, the time, the reading
  time "6 min read" in `TextMuted` (after a dot, only when the estimate is a minute or more and the entry is read, not
  watched), the kind mark; the row actions at its right), the title, the summary or the text, the pictures. An article that has an
  AI summary shows it in place of the feed summary, in the same size, color and two-line clamp, led by the `sparkles` icon
  (13, `TextMuted`, labelled "AI summary") so it reads as generated; Social, Videos, Images and the Compact viewer, which
  show no feed summary, are unchanged. Rows are separated by a
  hairline only, which hover and selection hide. The toolbar keeps its 12 inset in a wide window, so the segments stay centered in the main column. Pictures in a social post share a grid at
  most 440 wide (520 in Social): one picture keeps its aspect ratio, at most 384 wide and 240 high; two sit side by
  side at 4:3; three or more show the first three as squares in one row, the third covered by "+N" for the rest
  (labelled "N more pictures"), so a post is never taller than one row of pictures. Videos are a grid of cards at least
  200 wide, 16:9 covers; Images are a masonry wall of up to three columns at least 170 wide, each picture at its own
  aspect ratio. These are the defaults; the View as preference (the next bullet) changes them. Below a 760 wide window the segments show icons only. Sizes taken from the mockup are written
  `px(t, n)`, which is `t.Rem(n / 13)`, so they follow the text size. The pane's section header, 22 high, ends in a 22
  square + button that adds a subscription; it shows even when there are none. The label starts where the row icons
  do (10 in) and the button's 14 icon ends where the row counts do (10 in), so the button is centered on the pane line (`pane-line` and `pane-add` in the tokens), 6 in from the row
  highlight's edge and level with the label's middle; drawn in `design/screens/timeline.html`. A paused subscription has its name in
  `TextMuted`, a pause mark instead of the failure mark, and no count. With one subscription chosen, a pencil button
  after the count edits it. A row is chosen with a fill (ink 6.5%) and the selection marker; see
  [States](#color-and-dark-mode).
- **"Last looked" divider.** In the column of rows only, a 28 high row of its own between two entries, in the row
  column (at most 680, the same side padding): a 1 high `Border` hairline across it with the label centered on the
  hairline, 12 medium `Accent` on the `Background` with 10 of padding at each side, tabular numerals: "12 new since you
  last looked" / "上次看到这里 · 新增 12 条" ("50+" over 50). It is a button, named by its label (the accessible
  name is the label); hover steps the label to `AccentHover`, the keyboard ring is the row's, and a click closes it with
  no animation, the list below keeping its place. It has no fill and no actions, and the hairline stands in for the
  separator of the row below. Drawn in `design/screens/timeline.html`, `last-looked`.
- **Viewers (View as).** Each kind has a viewer the reader chooses in Settings, Appearance (the settings row is
  described under Settings below; the rule for which entries use it is in `docs/product.md`, Viewers). The default of
  each kind is the look described in the timeline bullet above (Articles List, Videos Grid, Social Card, Images Wall); the others are:
  **Article, Compact.** A row 36 high, in the column (680 wide), padded 0 16 0 28 like a row, radius 10, parted by the
  same hairline; the unread dot sits at its left, centered. Left to right: the site icon (16), the source name in a
  128 wide field (96 below a 760 wide window; 12 medium `Text2`, one line, ellipsis), a 12 gap, the title (13
  semibold `Text`, one line, ellipsis, taking the rest; read: 500 `TextMuted`), the star mark when starred, and the
  time (12 `TextMuted`, tabular figures, right-aligned, 56 wide at least). The reading time is left out, the summary
  and the thumbnail too. The row actions replace the time while the pointer rests on the row or it is chosen or
  focused (28 by 24, centered vertically, no longer in the meta band because there is none).
  **Social, Compact.** A row padded 12 16 12 28 with 4 between its parts: the meta line (author, handle, time), the
  text at 13 `Text` clamped to 2 lines (6 in the default viewer), then the pictures shrunk to thumbnails. One picture
  is a 56 square, radius 8, at the right of the text, starting under the meta line (as an article's thumbnail does);
  two or more are a strip under the text, 8 below it, of at most four 40 squares, radius 6, 4 apart, the fourth
  covered by "+N" (11 semibold white on ink 50%) when more follow; the strip never wraps. Failed thumbnails show the
  `Track` fill and a 16 image mark.
  **Video, Large cover.** A row of the column's width (the 28 gutter stays for the unread dot): the cover fills the
  row's content width at 16:9, radius 12 with the 0.5 DIP outline, the play mark (a 44 disc of ink 40% with a 16
  white triangle) always shown, the duration badge at the bottom right, then 8 below the cover the title (15
  semibold, two lines at most, ellipsis), then the meta line (source, time, the platform with the video mark). The row
  actions sit at the right of the meta line. The cover is at most 640 wide (360 high) and stays left-aligned with the text; in a narrower column it is just the column's width. In the 680 column it is about 620 by 350, so a screen shows two
  to three entries; it is for people who want the picture first.
  **Video, List** and **Image, Card** are the rows the other scopes draw today, now also in Videos and Images: the
  video row (a 16:9 thumbnail with the duration, the meta line and the title beside it) and the image card (the
  3:2 picture); the grid and the wall are not used for that kind.
  **Image, Grid.** Used by default only in the Images scope without a search, as the wall is. It is the Videos grid with square
  pictures, so it follows the same layout: the grid fills the column, padded 16 at the top, 24 at the sides and 32 at the
  bottom, cells at least 170 wide (the wall's column width) sharing the width, 16 across and 24 down, aligned
  at the top. A cell: a square picture (radius 10, 0.5 outline, centered and cropped), 4 below it the title in 13
  semibold on one line with an ellipsis, then the meta line with the inline unread dot. The cell is square before the
  picture arrives, so a late picture moves nothing; the stored width and height are not used. An entry with more than
  one picture shows the count in a badge at the top right, a dark pill (11 medium white on ink 62%, radius 5, 6 in
  from the corner) with an 11 images mark and the number ("3 pictures" as the picture's name). Like a video card, the
  cell has no fill, chosen ring or focus ring of its own; hover, the actions and the keyboard are as the Videos grid
  has them. Keyboard and the V rule treat a row of cells whole, as the Videos grid does.
  **Hover, in the grid and the wall.** As today's cards: the video card's play mark appears, the row actions appear
  at the cell's bottom right (`bottom -2, right -4`), and the meta line gives way 84 on its right so the actions never
  cover it. Nothing else changes: the time and the star stay.
  **Unread dot.** A grid card, a wall tile and the square image cell carry the 7 `Accent` dot inline at the start of
  the meta line. A row, including the Large cover and the video row, carries it in the 28 left gutter, level with the top of
  the row's first line (on the Large cover, with the top of the cover).
  **Where each applies.** Grid, Wall and the square Grid by default only in the Videos or Images scope with no search; in All,
  Articles, Social, a single subscription and a search the kind draws its row or card, so a subscription starts as
  a column. The grid switch (below) can turn the grid on for one video or image subscription. Compact and Large cover apply wherever the kind appears, so a Compact
  article row, a Large cover video and a Compact social post can sit one under another in All. Drawn in
  `design/screens/timeline.html`: `article-compact`, `social-compact`, `video-cover`, `video-list`, `image-grid`,
  `image-card` and `mixed-viewers`; with no state, `scope=video` and `scope=image` are the defaults, the grid and the
  wall.
  **Grid switch.** A bare 32 square icon button right of the reading-time button, shown only in the Videos and Images
  scopes and in a single video or image subscription, never while searching. It is one toggle: while the list shows it
  draws the layout-grid icon, named and tooltipped "Show as grid"; while the grid shows it draws the list icon, named
  "Show as list". Same states as the other bare tool buttons (hover fill, focus ring), 16 icon in `TextMuted`. It does
  not show a pressed state, since its icon already says which look comes next. At the minimum width it stays, as the
  reading-time icon does. The grid it asks for is the kind's: the cover grid for videos, the wall for images, or the
  square grid when the Images viewer is Grid; the list is the kind's other look. The choice belongs to the scope it was
  made in, overrides the viewer of Settings there, and is kept on this device. The app never suggests the grid. Drawn in
  `design/screens/timeline.html`: `image-switch` (Images switched to the list, so the switch shows the grid icon) and
  `source-grid` (one image subscription as a grid, so the switch shows the list icon); every other state of Images and
  Videos shows the list icon.
- **The subscription dialogs.** Dialogs over the timeline on a `RGBA(28,24,18,.32)` backdrop: 440 wide to add, 480 to
  edit, radius 16, `theme.RaisedFill` and a soft shadow, at most the window's height less `Space(8)`. A header (a 32 square
  tile or monogram, the title 15 semibold, a 28 square bare close button), a form that scrolls when the window is too low while the header and the buttons stay (a `Border` line appears under
  the header once the form is scrolled and above the buttons while more is below),
  with `Space(3.5)` between fields and 20 to the sides, and a footer with the buttons at the right; in the edit
  dialog "Delete…" sits alone at the left, in `Danger` without a fill. Fields are 32 high; title and interval share a
  row. Under the address, one line at a time: the error, the help, the RSSHub instance card (`Warning` soft fill) or what
  the address was recognized as (`Accent` soft fill, the address in monospace). An address that is a web page is looked up
  half a second after typing stops: the line reads "Looking for feeds…" with a spinning loader at 14 and Add is held. Every feed
  found is a row of the same recognized-as style (RSS icon, title or site and what it carries, the address in monospace, a check
  at the right), rows 6 apart under a 12 muted line "Found n feeds. Choose one."; the chosen row has the `Accent` soft fill and its
  check, the others have no fill, a 1 `Border` outline and no check. The first row is chosen, a click chooses another, and a single
  result is the one card as for a recognized link. When nothing was found a 12 muted line says so and Add tries the address
  as typed. Mockup states `add-searching`, `add-found-list` and `add-none-found`. A name, or a link that has no feed, is
  looked up in the RSSHub instance's route catalog the same way: the line reads "Looking for RSSHub routes…", and every
  matching route is a row like the feeds above (RSS icon, route name, the route template in monospace), at most 10. Under the
  chosen row sit its parameters, one 32 high field each with the name as a 12 medium label ("(optional)" after it for a
  parameter that may be left out) and the description as placeholder; the fields filled from a link look like typed ones.
  A secondary "Preview" button with a 14 eye icon follows the fields and is held until the route is complete; its result is a
  card with a 1 `Border` outline listing the first 5 entries, the title on one line and the date at the right in 12 muted,
  under a 12 muted line with the feed's title, and a failure is the error line below the button. Without a catalog a 12
  muted line says to type the route, as for a route typed with a slash, and there is no error. Mockup states
  `add-routes`, `add-route-params`, `add-route-preview` and `add-no-catalog`. A full address of the public rsshub.app
  gets a warning card (`Warning` soft fill, 8 radius, as the RSSHub-address card) under the field in place of the lookup:
  a 14 triangle with one sentence in `Warning` medium and a small secondary button under it, "Use my RSSHub instance"
  (an instance is set; it rewrites the field to the route only when pressed) or "Open settings" (none is set). Mockup
  states `add-public` and `add-public-no-instance`. When the site's bot protection turned a fetch away, the failure
  text (health line, sidebar tooltip, toast) is one localized sentence in place of the raw HTTP text (`edit-blocked`). Other known failures (timeout, name not found, certificate, no connection, 401/403, 404/410, 429, 5xx, not a feed) read as a short phrase in the same places in place of the English text the server stores (`edit-failing`); the raw text is in the tooltip of the health line, the sidebar mark and the health list ("Original: …") and in the description a screen reader reads. Text it does not know is shown as it is. No new element: the line is the same, only its words and its tooltip change. The edit dialog starts with a health line
  (`Surface`, or the `Warning` soft fill while fetches fail) with a small Refresh button, Try again while fetches fail.
  For a route that has been fetched, a muted 12 line under it names the RSSHub instance that served the last fetch
  (host only, "Served by fallback address {host}" with a second muted line saying the first address did not answer, when it is not the first of the list). The interval offers Auto
  first, and an automatic subscription reads "Auto · 6 h" with the interval in use. When the first fetch found the feed to look like
  another kind, a line in the style of the health line sits under the kind picker with the kind's icon, one sentence and a small
  button "Show as Videos"; it only chooses the kind in the dialog. "Keep entries" follows, a full-width select in the
  style of the interval, above the pause switch: "Follow settings · 1 month" (the global period after the dot) is
  the default, then 1 week to 1 year and Forever, and an "N d" row when the stored number is none of those; a muted
  12 help line under it says starred entries are always kept. "Pace" follows in the same style: Off is the
  default, then Breaking (3 h), Daily (2 days) and Weekly (7 days), and an "N h" or "N d" row when the stored number is
  none of those; a muted 12 help line says unread entries fade and leave All and Unread when the time is up, and
  stay in search, Starred and the source. "Extraction" follows, a disclosure row (12 medium, a chevron that turns
  from right to down) with a muted 12 help line, collapsed unless one of its three fields is set (`edit-extract`
  in the mockup). Opened, it holds a "Content selector" box, a "Referer" box and a "Link rewrites" box (monospace,
  three lines that grow to eight, one `from => to` per line), each with a muted 12 help line that says what an empty
  field does. Saving checks the three on the server, and a refusal shows under the field it concerns, in the
  field's error style. The dialog ends with "Summaries and
  translations", a 12 medium muted group title with the sparkles icon: two switches that queue new entries, and two
  monospace prompt boxes, three lines that grow to eight, whose placeholder says the empty box uses the prompt in
  Settings. While AI is not set up a line on `Surface` in `TextMuted` says so, with a small "Open settings".
  Under the mute keywords, after their help, a second 12 `TextMuted` line (tabular figures, a status text) says how many
  entries the keywords hide now: "{n} hidden now." (「现在隐藏了 {n} 条。」) or "Nothing hidden now." (「现在没有隐藏的条目。」).
  It follows the box as keywords are typed, after a short pause; until the new number arrives the old one stays, with
  no spinner. With no keywords, or if the count cannot be read, the line is not drawn. Drawn in
  `design/screens/subscriptions.html`, `edit` and `edit-mute-none`.
- **Subscription health.** A dialog 480 wide like the edit dialog opened from the command palette action
  "Subscription health…"; the sidebar has no entry for it. Header: the `heart-pulse` icon, the title and a muted line,
  "Sources that need a decision. Nothing changes until you choose." Body, one state at a time: loading is a muted line
  with a spinning `loader-circle`, "Checking…"; an error is an `errorLine` with a small "Try again"; empty is a centered
  `circle-check-big` in `Success` over "All your subscriptions look healthy"; otherwise the list, scrolling inside the
  dialog. A row has the source's glyph (16, as in the sidebar) and name (13 medium, one line) at the left, under it the reason in 12 on
  `Warning` (`dead` and `failing`) or `TextMuted` (`rarelyRead`) with a 14 icon (`circle-alert`, `triangle-alert`,
  `eye`), and a second muted 12 tabular line with the last 30 days' numbers; at the right two small buttons, "Pause"
  (`pause`, quiet) and "Unsubscribe…" (`trash2`, `Danger` text, no fill). Rows are separated by a hairline, 12 high
  paddings, no card around them. A failing reason shows the last error on a second line, one line truncated. A row
  leaves the list as soon as its action succeeds; the footer has one "Close" button. Unsubscribe opens the same
  confirmation as Delete. Drawn in `design/screens/subscriptions.html`, `health`, `health-loading`, `health-error`,
  `health-empty`. Accessibility: the dialog is labeled by its title, each row is a list item named by the source and
  its reason, the buttons carry the source's name ("Pause {name}", "Unsubscribe {name}"), and the loaded count is
  announced with `Announce`.
- **Hiding the sidebar.** The button sits in the pane's top 40, at its right end, centered on the pane line (25 from the pane's edge, like the add, Unread first and settings buttons) and on the toolbar row's middle line,
  level with the window buttons at the left; it is a bare 32 square icon button with
  the Lucide `panel-left` icon (16, `toolFill` colors, radius 8), named "Hide sidebar" while the pane shows and "Show
  sidebar" while it is hidden (that name is also its tooltip; no shortcut in the tooltip, as no other tooltip carries
  one). Cmd+\ on macOS and Ctrl+\ elsewhere does the same, from the timeline and the reader, which share one key
  handler, and it is in the key sheet under "Anywhere". Hidden means the pane is gone, with no icon rail, and the main
  column takes the whole window width, so the reading area keeps its width at the 640 minimum; the failing-sources hint and
  the add button go with the pane (1 to 5 still switch kinds; H or Left expand the pane first, then focus it). The
  window buttons sat over the pane, so with it hidden the timeline's toolbar row's left padding grows by `TitleBar().Left`, and the
  reader's row gives way only as far as the column's edge leaves them too little room (below), and the sidebar button moves to the toolbar's leading edge, right after them,
  before the title, on the same line as before; it stays at that fixed position whatever the title is. The pane does not animate in or out. The choice is a device
  preference and is remembered.
- **The reader.** It takes the main column's place; the pane stays. A 52 high toolbar row over the article, as in the
  timeline: "Back to {scope}" (a chevron and the scope's name) at the left as a bare button; at the right
  [previous, next] and [star (`Star` when starred), read toggle, open in browser, copy link (link icon), more] as bare icon buttons (more is the ellipsis, a menu button whose menu, the platform's own with text only, holds Copy as Markdown and Save as Markdown…; it is dimmed with the other article buttons). The order is fixed, the same in every article: back, previous, next, summary, translate, star, read/unread, open in
  browser, copy link, more. The one glass capsule, [Summarize (sparkles), Translate (languages)], takes the summary and translate places,
  carrying their names beside the icon (12
  medium, icon only below a 760 wide window). The read toggle is an envelope: closed (mail) to mark unread, open
  (mail-open) to mark read, the same icons as the timeline's row action. Summarize and Translate are toggles: pressed,
  they sit on `Soft(Accent)` in `Accent` and their names become "Hide the summary" and "Show the original". In a wide
  window the toolbar lines up with the article column, centered in the whole main column as the article is: the back
  button's icon starts at the text's left edge and the last button ends at its right edge. The window buttons over a
  hidden pane (above) move the row only where the column's edge leaves them too little room: the row then pads that
  side more and keeps its width, so in a wide window it does not move at all. Under the toolbar the article scrolls, a column at most 680 wide at Medium (680 times the Reading text size scale: 635, 680, 771, 861, tokens.md; the window can limit it further), centered,
  padded 28/32/48 (20 below a 760 wide window): the source's monogram, name, full date and, after a dot, the reading time ("6 min read") at 12; the title at 24 (20
  when narrow) weight 600 in `Text`; the note on where the text comes from (`Warning` soft fill when it is the feed's,
  `Surface` with a spinner while the full text is being got), 12, its "Open the original" link underlined and, when the
  full text failed, a "Try again" text button at its end; when both the full text and the feed's text exist, the
  Full text | Feed switch, the segmented control of the translation note (24 high segments in a 2 padded `Track` of ink 7%, the chosen one
  `Raised`) on its own row, left aligned, 18 below the title and the notes; then the text 22 below. The body is set in the Reading font (the interface font unless the reader chose a serif or a family, tokens.md) at 16 and line height 1.7 (1.85 in
  Chinese) by default, in `theme.Prose`; the Reading font, text size and Line spacing preferences change it, and the text size changes the column's width too, and h2, h3, the
  gaps between paragraphs and list items and code scale in step with the size. The Reading font sets the body only: h2 and h3 use the interface font,
  19 and 16 semibold, 1.9em and 1.3em above, 0.6em below; 1.05em between paragraphs, 0.3em between list items, a 22
  wide marker column; quotes behind a 2 DIP ink 16% line in `Text2`; code on `Surface` at 13 monospace, radius 12,
  scrolling sideways, inline code on an ink 5.5% fill, radius 4; links in `Accent` with a 1 DIP underline 3 below in
  `Accent` at 38%; pictures at their own aspect ratio (16:9 until known), radius 12 with a 0.5 DIP ink 10% outline,
  at most their own width, captions 12 in `TextMuted`. Loading is a skeleton of `Surface` bars; the empty states
  (getting the text, no text, can't load) are a 48 tile, a 15 semibold heading, a hint at most 320 wide and a button; "no text" has two, the primary "Open the original" and "Try again", which runs the extraction again and shows the getting state meanwhile.
  The whole text is one selection.
- **Immersive reading.** Z (plain, not with Cmd/Ctrl) in the reader hides the sidebar and the toolbar so the article has the whole window;
  Z again or Escape leaves (Escape closes the selection card first, then leaves immersive reading, and only then goes back to the list).
  Entering changes nothing vertically: the 52 high band at the top stays, empty and in `Background` (the window buttons sit in it, so text is
  never under them, and it is the drag area), the article keeps its own column (`MaxWidth` 680 at Medium, following the Text size), now centered in the
  whole window, and the scroll position, selection and focus stay. Pointer in that band shows the toolbar over it (no animation, no
  shift of the text, the hairline shown); it hides when the pointer leaves, unless the keyboard focus is in the toolbar or a menu from it is open.
  Tab reaches the toolbar's buttons even while it is hidden, and a button taking the focus shows it. The toolbar's ends stay at the article column, but no
  closer than 640 wide in all, so the buttons do not fold early in a wide window; below that the narrow-window steps apply as in the normal reader.
  Shown, the toolbar has one more button, "Exit immersive reading" (Lucide `minimize-2`, the toolbar button spec, tooltip "Exit immersive reading (Z)"), right
  before More; it exists only in immersive reading. There is no button that enters it: the key, the palette's "Immersive reading" action (reader only) and a
  Timeline menu item of the same name without accelerator (enabled while the reader shows) do. J, K, N, S, M and O work as ever, so nothing needs the pointer.
  Cmd/Ctrl+\ does nothing while it is on, and the device preference of the sidebar is not changed: leaving shows the sidebar as the preference says.
  It ends with the reader (back to the list, a lost session) and is not remembered across restarts. Resizing the window keeps it; the system's full screen is
  separate and untouched. Accessibility: entering announces "Immersive reading on" and leaving "Immersive reading off" with `Announce`, and the focus stays on the
  article (a focused toolbar button that disappears hands the focus to the article). Drawn in `design/screens/reader.html`, `immersive` and `immersive-bar`.
- **Summary and translation in the reader.** The summary is a card under the title, radius 14, padded 14/16/16, text
  13 at line height 1.65 (1.75 in Chinese), its head 12 medium: while it is made, `Soft(Accent)` with a spinner,
  "Summarizing…", a Cancel text button and three skeleton lines; done, `Soft(Accent)` with "Summary · by {model}", the
  head in `Accent`, the model in `TextMuted`, and a close button; failed, the `Danger` soft fill with what went wrong
  and a small "Try again" (when the daily cap on automatic work stopped it, the reason says so and points to Settings;
  state `summary-cap` in the mockup); while AI is not set up, `Surface` with "AI isn't set up", one line on where to set it and a
  small primary "Open settings". The translation replaces the title and text in place; the note under the title says
  what is shown. While it is made, the original stays with a note (`Surface`, a spinner, Cancel); done, the note is
  `Soft(Accent)`, "Translated by {model}" and a segmented Translation | Original switch, and the translated text is
  plain paragraphs, pictures and links staying in the original; failed, the original stays with the note in the
  `Danger` soft fill, the reason and "Try again". Cancel only stops waiting; leaving the article does the same.
- **Selection card in the reader.** A selection in the article opens the system context menu with Copy and Select All
  and, after a separator, "Translate selection" and "Explain selection" (only while text is selected). Choosing one pins
  a card to the bottom of the reader column, 16 above the window edge, floating over the text: the width of the text
  column at most, radius 14, `Surface` with a 0.5 DIP `Border` rim and a soft shadow, padded 12/16/14. The head (12
  medium, 22 high): the `languages` or `sparkles` icon in `Accent`, the task name, then the first 40 characters of the
  selection in `TextMuted` in curly quotes with an ellipsis, and a 22 close button at the end. Below it the result in
  13 at line height 1.65 (1.75 in Chinese), selectable, scrolling inside the card from 40% of the window height, and a
  small "Copy" button (secondary; copying announces "Copied" to screen readers) with "by {model}" in `TextMuted` beside it.
  Loading is a spinner head ("Translating…" or "Explaining…") over three skeleton lines; failed is the `Danger` soft
  fill with the reason and a small "Try again"; AI off has the "AI isn't set up" copy and the primary "Open
  Settings…"; a selection over 2000 characters shows "Select less text" with the limit and no button. Escape closes
  the card before it goes back to the list. The card has the group role named after the task, and the result is announced when it arrives.
- **The lightbox.** The pictures of a social or image entry, over the whole window (`ui.DialogBase`, drawn in
  `design/screens/timeline.html`, `lightbox`, `lightbox-failed`, with `size=min` for 640). A solid
  layer of `RGB(20,17,13)` covers the window in both appearances, opaque so nothing of the list or the sidebar shows
  through (a picture reads best on a dark ground, so this is the one place the light scheme is dark), sidebar and
  corners included. Everything on it is `ChromeText` and `ChromeMuted`
  with the pane's state fills (ink 5.5% hover, 8.5% pressed), never `Raised`. The picture is fitted: scaled down to the
  largest size that fits between the bars and the side buttons, centered, never above its natural size, radius 8 and
  no border or shadow. Top bar, 56 high, padded 12 to the sides: the counter, "3 / 12", centered, 13 medium in
  `ChromeText` with tabular figures (nothing with one picture); at the right "Open in browser" (a 32 high text button:
  the external-link icon and the label, 13 medium; the icon alone below a 760 wide window), "Open picture in browser"
  (a 32 square bare icon, the image icon) and Close (32 square, the x icon), 8 apart. Previous and next are 44 round
  buttons (chevron-left, chevron-right, 22 icons) in ink 10% on the dim layer, 20 from the window's sides and centered
  vertically; the one with nowhere to go is dimmed to `Disabled` and stays in place, and neither shows with one picture.
  A one-line caption sits under the picture, centered, 12 in `ChromeMuted`: the author and handle of a post, the title of
  an image. While a picture loads its box keeps a 4:3 shape of ink 6% with a spinner; a picture that fails shows the same
  box, now opaque (`RGB(42,38,33)`, 60% of the stage, at most 480 wide), with the muted image-off icon, 32, and "This picture can't be shown" in 13
  `ChromeMuted` under it; paging and both browser buttons stay enabled (the original address may open where the proxy could not fetch it). Below 760 the bar and the side buttons keep their
  sizes and the picture gets the rest, never less than the window's width less 144. The layer opens, closes and changes
  pictures at once, with no motion.
  Keys: left and right page, Home and End jump, O opens the entry in the browser, Escape or a click on the dim layer
  closes (a click on the picture does nothing). Tab order, which is also the reading order: Open in browser, Open
  picture in browser, Close, Previous, Next (the picture area is last, and Tab wraps within the dialog). The dialog opens with the focus on its own picture area, so the arrow keys
  work at once and no ring is drawn there; the buttons take the focus ring (`Focus`, 2, offset 1) when reached by Tab.
  Closing returns the focus to the row that opened it. Names: the dialog "Pictures from “{title}”"; the picture
  "Picture {n} of {total}" (plus the caption); the buttons "Previous picture", "Next picture", "Open in browser", "Open
  picture in browser", "Close". Opening and each page announce "Picture {n} of {total}" with `Announce`.

## Color and dark mode

Colors come from the theme, `c.Theme()`. Oriel puts its own palette, paper and ink (warm in light, neutral graphite in dark), on the theme
roles: the root view calls `theme.Apply` (in `internal/ui/theme/`) first in every frame, which picks the light or
dark palette for the Theme preference (the system appearance by default) and keeps the theme's metrics, so the desktop's text size still applies. The
values are in [design/tokens.md](../design/tokens.md). See
[Styling and themes](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/styling.md).

- **Always use theme roles, never literals.** Screens use only theme colors; for example, secondary text uses
  `TextMuted`. Color literals live only in `internal/ui/theme/`. `ui.Theme` has a fixed set of roles; the rest of the
  scale (a second text color, the reading color, the star, tracks, the raised fill) are helpers in that package,
  called `theme.Text2(t)`, `theme.Prose(t)`, `theme.Track(t)`, `theme.RaisedFill(t)`, `theme.LineStrong(t)`,
  `theme.Disabled(t)`, `theme.Star(t)`, `theme.Marker(t)`, `theme.Chosen(t)`, `theme.Soft(t, c)` and
  `theme.Ink(t, a)`. A faint fill behind an icon or a status is `theme.Soft` of a role.

  | Role | Use |
  |---|---|
  | `Background` | window and content fill, the paper; in dark mode neutral graphite, a step below `Surface` |
  | `Surface` | placeholders, code blocks, notes and plain cards, control faces |
  | `SurfaceHover`, `SurfacePressed` | hover and pressed fills: ink at 3% and 5% (4% and 6% in dark) |
  | `Border` | hairlines between rows and dividers inside cards: ink at 7.5% |
  | `Text`, `TextMuted` | titles and primary text; meta, counts, read titles, hints and placeholders (`theme.Text2` between them for source names and summaries) |
  | `Accent`, `AccentHover`, `AccentPressed`, `AccentText` | the unread dot, links, a chosen icon, the primary action; text on the accent |
  | `Danger`, `Warning`, `Success` | destructive, cautionary and positive status (always with text or a symbol) |
  | `Selection`, `Focus` | selected text; the keyboard focus ring (`Accent` at 70%, 2 DIPs) |
  | `Inverse`, `InverseText` | tooltips and toasts: a charcoal tile (warm in light, graphite in dark) with ivory text, set explicitly |
  | `Scrollbar` | scroll bar thumbs |

- **Appearance.** The only dark mode switch is the Theme preference in Settings, Appearance: System (default), Light
  or Dark, applied by `theme.Apply` alone; screens do not make their own. Teal stays the accent in both. If a screen draws custom
  elements, use only theme colors, or read `c.Theme().Dark` where a choice cannot be expressed with them. For derived
  colors use `theme.Ink` and `Alpha` on theme colors rather than new constants.
- **Accent.** Oriel's accent is a muted teal, in both appearances; it does not follow the system accent. It is the
  only hue besides status and the star: kinds of entries are not colored. One primary action per group gets the accent.
- **Paper and ink.** Oriel is read for long stretches, so the palette is paper, not a screen: warm
  off-white in light and neutral graphite in dark instead of white and black, body text near 15:1 in titles and 11:1 in prose instead of 16:1, a
  low-chroma neutral scale (warm in light; in dark a hint of cool, no brown, so the pane and the accent are the only
  color) and fills that are ink at a low alpha,
  so the same fill works on `Background`, `Surface` and a photograph. The surfaces step evenly: a hover, a press and a
  selection are three equal steps of lightness, the same everywhere.
- **The ink pane.** The sidebar is the one dark surface in the light scheme: an opaque pane, per scheme. In light it is warm
  charcoal, which sits with the warm paper; in dark it is deep teal ink, which separates from the neutral graphite
  content by hue, so the pane and the accent are the only color in dark mode. It has its
  own light-on-dark roles. It is drawn with `c.SetTheme(theme.PaneTheme(main))` before the
  pane's elements are built and the main theme set back after, so its widgets, helpers and `Chosen` read the pane's
  values. Its roles, fill and contrast are in tokens.md; with high contrast it gains a `Border` rim.
- **Contrast.** Text and meaningful graphics should meet WCAG 2.1 AA: 4.5:1 for body text, 3:1 for large text and
  control boundaries. With the desktop's high contrast setting on (`c.Preferences().HighContrast`), `theme.Apply`
  makes `Border` a `Text` line (55%), moves `TextMuted` toward `Text`, makes `Focus` opaque, raises the state fills to
  8%, 12% and 16%, and turns glass (the capsules, the scroll edge) opaque; test with it on. Text on the ink pane holds AA
  (tokens.md has the numbers); `Warning` and `Star` are graphics, never text. `TextMuted` does not hold 4.5:1 on a
  `Track` or a soft fill: the unselected labels of a segmented control and the badges on a `Track` are `Text2`, and the
  text in a `Danger` result box is `Text`, with the icon in `Danger`.
- **States.** One look per state, the same on every screen, as steps of lightness: ink fills of 3%, 5% and 6.5% in
  light (4%, 6% and 8% in dark; 5.5%, 8.5% and 12% on the pane) for hovered, pressed and chosen, so
  hover < pressed < chosen. Hovered: `SurfaceHover`. Pressed: `SurfacePressed`. Chosen in navigation (a kind, a
  subscription, a settings group) and a chosen row: `theme.Chosen`, with the icon in `Accent` in navigation; a chosen
  row of the timeline also has the selection marker, a 2.5 by 18 `Accent` bar, radius 2, inside the fill 8 from its left
  edge. There is no border on a chosen row, and the accent never tints a fill on warm neutrals, where it turns muddy.
  On a chosen or hovered row, `TextMuted` text steps to `Text2` to keep its contrast. Chosen in a
  control (a segment, the kind of a subscription, original or translation): in the glass capsule an ink 10% fill; on a
  `Track` trough, a `theme.RaisedFill` face with a hairline shadow. The keyboard's place: the `Focus` ring,
  2 DIPs, always visible. Disabled: `Surface` with a `TextMuted` label, the primary button too, never a faded accent,
  so its label stays readable (at least 3:1); a disabled glyph may use `theme.Disabled`. Fields and text areas share
  one fill, `Surface`, and `theme.RaisedFill` with the `Focus` ring when focused.
- **Status.** Color supplements a message; the message states the status in words. A read entry, a failing
  subscription or a missing full text is said in text, not only shown as a dimmed or red row.
- **Content colors.** Images and video thumbnails from feeds keep their own colors; everything Oriel draws around
  them uses theme roles.
- **Mockups.** Mockups use the Oriel palette of `design/tokens.md`; the app running on a real device is what
  counts.

## Typography

- **System font.** Leave `Font` empty so text uses the platform's UI font. The Interface font preference (Settings,
  Appearance) sets the theme's `Font` to an installed family, chosen from `ui.FontFamilies()`; screens never set a
  family of their own, except monospace for code and addresses. The default text
  size is 13 on macOS and 14 elsewhere, and scales with the desktop's text size (`Preferences().TextScale`) when you
  rely on the theme; it is not a setting. See [Text](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/text.md).
- **Size relative to the theme.** Use `t.Rem(n)` for sizes (`n` times the theme font size) rather than fixed numbers,
  so custom text follows the user's text scale. Widgets use the theme size.
- **Five sizes.** 24 (the title), 15 (row title, section head, screen title), 13 (body and widget text), 12 (meta,
  labels, segments) and 11 (small labels: counts, section labels in the pane, durations; 12 in Simplified Chinese).
  Weights 400, 500 and 600 only. The reading body is the article's own scale, not interface type. The table of
  sizes, line heights and tracking is in [design/tokens.md](../design/tokens.md).
- **The connect screen.** Title 24 semibold, hint 12 in `TextMuted`.
- **The timeline.** Screen title 15 semibold, row titles 15 semibold in `Text` (read rows 500 in `TextMuted`), the
  source name 12 medium in `Text2`, time and other meta 12 in `TextMuted`, summaries 13 in `Text2`, social text 13 in
  `Text`, counts 11 medium with tabular figures. Titles in rows and video cards wrap and stop at two lines, summaries
  show at most two lines and social text six; the ellipsis ends the last line. A picture's caption in the wall, the
  source name in the meta line, a subscription's name in the pane and the screen title are one line with an ellipsis,
  and the source name gives way first, before the time. A Chinese title breaks between any two characters and a word
  with no break point (a URL, a version string) breaks where it must, so a title never pushes the row wider. The
  full title is always the entry's accessible name and the reader shows it whole; the pane's name also shows in full
  in a tooltip. Drawn in `design/screens/timeline.html`, `long-titles`.
- **A row that is about to leave.** An unread entry of a source with a pace fades during the last quarter of its time:
  the whole row (text, thumbnail, picture) goes from full to 55% opacity, linearly, and its accessible name ends with
  "leaving the timeline soon". Nothing else changes, so the row stays readable and clickable. Drawn in
  `design/screens/timeline.html`, `expiring`.
- **One article from several sources.** When the same link comes from more than one source, the list shows one row,
  and a quiet text button follows the time in its meta line: a 13 `layers` icon and "2 more sources" in 12 `TextMuted`,
  20 high, radius 6, padded 4 and 6, with the `Pressed` fill under the pointer. It is a button, never a badge and never
  an accent. Clicked, it expands under the row (`aria-expanded` follows) a list of the other sources, indented 12
  behind a 2 `Border` hairline: each item is a 30 high row (radius 8, `Hover` fill under the pointer or the keyboard) with the
  source's 16 icon, its name in 12 medium `Text2`, the title in 13 `Text` (cut with an ellipsis) and the relative time
  in 12 `TextMuted`. One row is open at a time; an item opens that copy in the reader. The row's accessible name for
  the button is "Also in 2 other sources, show them". The compact article row has no chip, and neither does a list
  with grouping off, one subscription or a search. Drawn in `design/screens/timeline.html`, `grouped` and
  `grouped-open`.
  In the reader the same copies are one line under the title: "Also in" in 12 medium `TextMuted`, then a text button
  for each other source, 28 high, radius 8, padded 8, with the source's 15 monogram, its name in 13 medium `Text2`
  (`Text` under the pointer, with the `Hover` fill) and the time of that copy in 12 `TextMuted`; the line wraps when
  the column is narrow, and there is no line for an article that only one source carries. Drawn in
  `design/screens/reader.html`, `also`.
- **Hierarchy by lightness.** Within one size, the order is `Text`, `Text2`, `TextMuted`; weight comes second and size
  last. Read rows go quiet in lightness and weight (500), not in size.
- **Tracking and numerals.** Small labels get letter spacing (+0.04em at 11, +0.02em in Chinese at 12); the title
  tightens (-0.015em); everything else is 0 or within 0.01em. Counts, times, durations and any number that changes in
  place use `FontFeatures("tnum")`.
- **Chinese runs taller.** Simplified Chinese gets a larger line height at the same size: body 1.7 instead of 1.5,
  row titles 1.5 instead of 1.35, the title 1.42 instead of 1.28, the reading body 1.85 instead of 1.7. The reader tells by the language stored for the entry (Chinese and
  Japanese run taller) and by the characters of the title and body only when none is stored; Chinese
  tracking is 0 except small labels. The reading font, text size and line spacing preferences change the reader's body
  text only, by the steps in [design/tokens.md](../design/tokens.md). Chinese text in the reader is also set by the
  rules of Chinese typesetting (below).
- **Long font lists.** A font menu with hundreds of families is the font picker (Components), never a plain menu.
- **The reading face.** The article's body is set in the interface font by default, as clean as the rest of the screen:
  Song and other thin serifs look faint and fuzzy at screen sizes and tire the eye over a long read, so the default
  stays the interface font. The Reading font preference (Settings, Appearance) lets the reader choose Serif or an
  installed family for the body only; the title, notes, h2, h3, captions and the toolbar keep the interface font. Serif
  is a platform set, New York with Songti SC on macOS, Georgia with SimSun on Windows, Noto Serif with Noto Serif CJK SC
  on Linux, and the interface font when none is installed (tokens.md, Reading text).
- **The reading column follows the text size.** The column is 680 wide at Medium and grows and shrinks with the text
  size (635 / 680 / 771 / 861), so a larger size does not cut the characters per line; the window can limit it
  further. There is no separate width setting.
- **Headings are real headings.** Give the title of a screen or section `Role(ui.RoleHeading)` and a `Level` so
  assistive technology can navigate by them.
- **Do not truncate silently.** Prefer wrapping. Where a single line is needed (`SingleLine`, `MaxLines`), for
  example an entry title in the timeline, make the full text available in a tooltip or in the reader.
- **Numbers and dates.** Show counts and dates in the user's language when they are data, not identifiers.

## Chinese typesetting

Rules for Chinese text in the reader, written without reference to the toolkit so that the iOS reader (#153) follows the
same ones. They change how text is laid out, never the text: no character is inserted or removed, so copying, search
and accessibility see the original string. Values are in [design/tokens.md](../design/tokens.md), Reading text; drawn in
`design/screens/reader.html` (`cjk=on|off`) and `design/screens/settings.html` (`appearance`, `reading-cjk-off`).

**Where and when.** One per-device switch, Chinese typesetting (Settings, Appearance, Reading, after Line spacing),
on by default. It applies to text that is Chinese, decided as the reader already decides Han text: the language stored
for the entry starts with `zh`, and when none is stored, the text contains Han characters. It covers everything the
reader sets as prose: the article title, h2 and h3, paragraphs, list items, quotes, captions, the notes above the
text, and AI summaries and translations (a translation by its target language). It does not cover code blocks or inline
code, and nothing in the timeline, the pane or the rest of the interface. Off, text is set as the font gives it.

**Terms.** Han characters are the CJK ideographs (U+3400 to U+4DBF, U+4E00 to U+9FFF, U+F900 to U+FAFF and the
supplementary planes). Latin text is the letters (with accents) and the digits 0 to 9. The em is the em of the text
the rule is drawn on. Full-width punctuation sets each mark in a one-em cell with the ink on one side, so half of
the cell is blank:

| Class | Marks | Blank half |
|---|---|---|
| Closing | `，` `。` `、` `；` `：` `！` `？` `）` `」` `』` `》` `〉` `】` | right |
| Opening | `（` `「` `『` `《` `〈` `【` | left |

Other marks are in neither class and are never changed: the curly quotes `“` `”` `‘` `’` (their width depends on the
font), the middle dot `·`, and the two-em dashes and ellipses `——` `……`.

**Rule 1, the gap between Han and Latin text.** Where a Han character is directly followed by a Latin letter or
digit, or a Latin letter or digit by a Han character, add 0.25em of letter spacing after the first of the two (the
last Han character, or the last Latin letter or digit). Examples: "使用Go语言写的RSS阅读器" gets the gap after 用, after
o, after 的 and after S; "支持2种主题" after 持 and after 2.
- Not between Han and punctuation, a symbol (`%`, `$`, `#`) or another Han character.
- Not next to a space: when a real space already separates the two, nothing is added; the author's space stays.
- Not next to inline code or a code block: it has its own padding. Code also ends any run, so the characters on its two
  sides are not paired.
- A link, emphasis or any other inline style does not interrupt a run: the characters on its two sides are paired as
  if the markup were absent, and a link's own text follows the rules as prose.
- The gap is spacing, not a character: it can sit at the end of a line, and nothing is inserted into the text.

**Rule 2, compressing punctuation.** Take each two adjacent characters that are both full-width punctuation marks of
the classes above, in the order they are read. If the first is a closing mark and the second is a closing or an
opening mark, or if both are opening marks, add -0.5em of letter spacing after the first, so the pair takes 1.5em
instead of 2em. The blank half that goes is the first mark's own right half when it is a closing mark, and the second
mark's left half when both are opening marks. Examples: `」。` `）、` `，「` `。」` `（「`. No other pair changes: an
opening mark followed by a closing mark (`「」`) has no blank half between them, and a mark next to a Han or Latin
character keeps its cell. Each pair is judged on its own and a blank half is used at most once, so in a run such as
`」）。` each mark gives up its half to the next one. Markup (a link, emphasis) between the two marks does not
matter; a space or code does, because they are not adjacent.

**Fixed values.** The gap is 0.25em and the compression is -0.5em, in the text's em, at every text size. Line height
and paragraph spacing do not change with the switch; the Chinese values already in Typography and Reading text apply
(line height 1.7, 1.85 or 2.0 by Line spacing, 1.05em between paragraphs).

**Known gap: the ends of lines.** The platform's text engine breaks the lines and the toolkit (MyGo) does not expose
where it broke them, so the rules that depend on line ends are left to that engine and are not implemented in Oriel
yet:
- Hanging and compression at line ends and starts: an opening mark at the start of a line and a closing mark at the end
  should not leave their blank half, so the text should be set flush at the edge. Today the half stays.
- Line-break rules (kinsoku): no closing mark, `！` or `？`, and no `、` `。` `，` at the start of a line; no opening
  mark at the end of a line; no break inside `——` or `……`. Today the engine's own rules apply, which differ between
  platforms and fonts.

A toolkit that knows its line breaks (the iOS reader, #153) implements these in full: each line is laid out first,
then the closing mark at its end is hung or compressed, and a line that would start with a closing mark takes one
character from the line before. Rules 1 and 2 above hold with or without them.

## Icons and images

- **Icons are SVG** shown with `ui.Icon`, which takes the text color and the font size: an icon follows theme colors
  and the text size automatically. Every icon has the same stroke weight, 1.6, at every size. See [Icon](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/icon.md).
- **One set: [Lucide](https://lucide.dev)** (ISC License), recorded in `THIRD_PARTY.md`. The files of lucide-static
  are copied unchanged into `internal/ui/icons/lucide/` and parsed once in `internal/ui/icons`; add an icon there
  before using it. The lightbox adds chevron-right and image-off (chevron-left and image are there); image-off marks a failed picture so that it is not read as the "Open picture in browser" button. Prefer an icon to a word where the icon is clearer, and keep the word where it is not.
- **An icon needs a name when it is alone.** Icons are decoration for assistive technology unless given a `Label`.
  A button that shows only an icon or a symbol must get a readable name with `.Label()`; an icon next to a visible
  text label needs none.
- **Icon before label** in buttons, except a forward arrow, which follows its label ("Connect →"). Icons in fields
  and buttons are 1.25 rem, in error lines 1.05 rem, sized with `FontSize`.
- **Feed images** (thumbnails, image entries, site icons) use
  [Image](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/image.md). Download and decode them in the background and
  make each bitmap once, never in the view; until it is ready, keep the image's box at its final size so the timeline
  does not jump. Images that carry meaning get a `Label`; decorative ones do not.
- **The timeline.** Each kind has an icon (newspaper, play, message circle, image; inbox for All). Kinds are not
  colored: a subscription's monogram is a `Track` tile with its letter in `Text2` (on the pane, pane ink 11% and
  `ChromeText2`), and only the chosen kind's icon takes `Accent`. A subscription shows its site icon where it has one: a
  square tile with radius 4 and a 0.5 DIP inset rim in ink 20%, so a dark icon still reads on the dark pane, 18 in the pane, 15 in the row
  meta, the reader's meta and 32 in the edit dialog header (radius 8, the size of its monogram). The icon
  loads through `/api/image` in the background; the monogram stands until it is decoded and stays when there is none or
  it fails, so nothing jumps (`broken-images`). With about 60 subscriptions the pane scrolls its heading and
  subscriptions together, in the store's order (lowercase name); the kinds above and the server bar below stay in
  place (`many-feeds`). Row actions are bare icons only (star, mark read, open in browser) with labels and
  tooltips, in the row's meta line at the right, never over a thumbnail or a title, shown on hover, on the chosen row and
  on the focused row, fading in 120 ms. Pictures are downloaded through the server (`/api/image`), at most four at a
  time, and the 300 most recent bitmaps are kept; a picture that fails to load keeps its box on `Track` with a muted
  image icon, and a video cover keeps its play mark and duration. A video cover
  carries a play mark and the duration, labelled "Duration 12:34". The unread dot is a 7 `Accent` circle; a failing
  subscription is `Warning` with a mark, the server is a `Success` dot, `Warning` while background checks fail; a starred entry is `theme.Star`.
- **The product icon** is a mixed timeline on a warm ivory tile: a teal rail joins three entries, each a teal dot
  with a title line in ink (`#26231f`) and a shorter summary line in muted gray; the newest entry's dot, at the top,
  is the brightest (`#5eead4` to `#0f766e`). It sits on the macOS grid (an 824 tile at 100 in 1024, radius 185) with
  transparent corners, so every platform shows the same rounded tile. It was drawn for Oriel and is MIT licensed with
  the rest of the project. The source is [design/icon.svg](../design/icon.svg); see
  [design/README.md](../design/README.md) for exporting it.

## Component guide

Pick the widget by the job. Each name links to the upstream page, which also says what assistive technology sees of
the widget. The complete list is the
[UI index](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/README.md).

What Oriel uses today:

| Component | Use |
|---|---|
| `ui.Button` | Secondary actions. A button that shows only a symbol must give a readable name with `.Label()`. |
| `ui.PrimaryButton` | The main action of a group of buttons; at most one. |
| `ui.Text`, `ui.RichText` | Text; `RichText` where one word of a sentence is set apart. Secondary explanations use `TextMuted`. |
| `ui.TextInputBase` | A text field drawn by the screen, inside a box with its icon; `Error` names what is wrong for assistive technology, and the screen shows it under the field. The timeline's search is one too, since `SearchField` moves the focus to its box instead of the text. |
| `ui.ButtonBase` | Buttons drawn by the screen in the Oriel look: an icon and a label, or an icon alone with a `Label`. |
| `ui.Icon` | Lucide icons from `internal/ui/icons`. |
| `ui.SegmentedBase` | The timeline's list of kinds and its Everything/Unread/Starred filter, the kind of a subscription, and the reader's Translation/Original switch, drawn in the Oriel look. |
| `ui.DialogBase` | The add and edit subscription dialogs: Escape and a click outside close them, the window's keys stop behind them, and the focus goes back where it was. |
| Add and continue | The footer of the add dialog is Cancel, "Add and continue" (a secondary button, Cmd/Ctrl+Enter, listed in the key sheet under "Anywhere" and with no tooltip, as its label is visible) and the primary "Add". Adding with "Add and continue" keeps the dialog open: the address and title empty, the kind and the focus back on the address, and above the address a status row (`Success` at 12% fill, a check at 16, 13 medium text) saying "Added “name”", then "Added “name” · 2 added" and so on, announced when it changes. Cancel turns into "Done" once something was added. A failed address shows its reason under the field as before and the row stays, so one failure blocks nothing. Mockup state `add-more`. |
| `ui.SelectBase`, `ui.Switch`, `ui.TextArea` | A subscription's fetch interval and keep period, its pause switch and its mute keywords. |
| `Element.ContextMenu` | Right click on a subscription: Refresh (Try again on a failing one), Mark subscription as read, Mark older than 1 day as read, Mark older than 1 week as read, Mark older than 1 month as read (dimmed with Mark subscription as read), Edit…, Pause or Resume, a hairline, Delete…. Right click on an entry: Star, Mark as read or unread and Open in browser, as on the row, then Copy link (dimmed for an entry with no link), a hairline, Copy as Markdown, Save as Markdown…, a hairline, Mark above as read. Right click on the mark all as read button: Mark all in “…” as read (as the click), a hairline, Mark older than 1 day / 1 week / 1 month as read, dimmed where the button is; `menu-markall`, `menu-feed` and `menu-entry` in `design/screens/timeline.html`. The menu is the platform's own, so its items are text only, with no icons or key hints; an item that has nothing to do (no unread above the entry, none in the subscription) is dimmed and does nothing. |
| Font picker | A popup list for a long list of fonts: the Interface font in Settings, Appearance, and the Reading font in the Reading card (#101). Anchored under its control, right edges aligned, at least 300 wide and as wide as the control, at most 360 high (less where the window leaves less room, below or above the control, whichever is larger); the list scrolls inside, the search stays. On `Raised` with the sheet shadow and radius 12. Top: a 32 high search field (search icon, placeholder "Search fonts"), focused when the popup opens, with the Focus ring. Below it the list: rows 32 high with radius 6, 13 type, every family name set in its own face (a family that cannot draw its own name falls back to the interface font; a name is cut with an ellipsis, never wrapped). The caller supplies the pinned rows (Interface font: "System font" and the current family; Reading font: "Follow interface", "Serif" and the current family), which sit above a hairline; the picker supplies the search, the list, the keys and the states, and a family already pinned is not repeated in the list. The current choice has a check at the right; the keyboard highlight is a `Chosen` fill, and starts on the current choice. Typing filters, case-insensitive, matching anywhere in the name; the pinned rows are searched with the rest (a pinned row that does not match goes, and the hairline with it when only one group is left). The match is not marked inside a name, so the face stays true to what it will look like; the highlight moves to the first row. Up and Down move the highlight (Home and End jump, it does not wrap), Return chooses it and closes the popup, a click chooses a row, Escape closes the popup without a change and the focus returns to the control, as it does after choosing; Tab does nothing special. Choosing applies at once, like the other Appearance controls. The family list is read in the background: until it arrives the popup shows the pinned rows and a "Loading fonts…" row, and typing is kept and applied when it arrives. No match: one muted line, "No fonts match “{query}”" (「{query}」 in Chinese), no rows and no hairline. Accessibility: the list is a list of options (MyGo has no listbox role, so list and list item), each row named by its family name with the current one selected and said so; the result count ("12 fonts", "No fonts match…") is announced with `Announce` as the query changes, after a short pause, and the loading and empty rows are status text. Typing never blocks the UI: filtering is a substring test over the loaded names, run only when the query or the list changes, and only the visible rows are built. Drawn in `design/screens/settings.html`, `font-open`, `font-filtered`, `font-empty`, `font-loading`. |
| `ui.List`, `ui.GridView` | The timeline's rows and its video cards; both build only what is in view. |
| `ui.AlertDialog` | Confirming deleting a subscription; the text says the delete can be undone for a few seconds, and the Deleted toast offers Undo. Its last button, the action, is the default and takes the focus; Escape is Cancel. |
| `c.Toast`, `c.Announce` | Feedback after starring, marking, refreshing all subscriptions (the result, plain, `refreshed`) and failures, and "No unread entries below" and "Back where you left off" (the reader opening a part-read article scrolled to its place, with the rotate-ccw icon, `resumed` in the reader mockup); counts announced without moving the focus. |
| `c.ToastAction` | Deleting a subscription (`deleted` in `subscriptions.html`): "Deleted “Go Blog”" with Undo and a close button, in the `Trash2` icon, for 8 seconds; announced, takes no focus; Undo restores the subscription with its entries and all that went with it, and a toast "Can’t undo this any more" says when the server no longer has it. "Mark all as read", "Mark above as read", "Mark subscription as read", "Mark older than … as read" and the V key act at once and offer Undo here, with a close button, one Undo shared by all, shown for 8 seconds, announced by screen readers and never taking the focus, and none when more than 10000 entries were marked (`marked-older`: "Marked 7 entries in “All” older than 1 week as read"); when no unread entry is that old a plain toast without Undo says so (`marked-older-none`); Cmd/Ctrl+Z undoes it while the toast shows. The toast is centered at the bottom as an `Inverse` tile with `InverseText`. |
| Command palette | Cmd/Ctrl+K, from the timeline and the reader. `ui.DialogBase` drawn like the key sheet (scrim, `Raised`, radius 14, sheet shadow) but anchored 15% from the top, 560 wide at most (the window width less the margins when narrower). Top: a 32 high search field (search icon, label and placeholder "Go to…") with the Focus ring, focused on open. Below it the list, at most 60% of the window high, scrolling inside: small muted group headers (Actions, Kinds, Subscriptions, Settings) over rows 32 high with radius 6 and 13 type. A row has its icon (the sidebar's kind icon, the subscription's glyph, the action's or page's icon), its name, and at the right a muted tabular unread count (subscriptions and kinds; above 999 it reads "999+") or the action's existing shortcut as key chips like the key sheet's. While the reader shows, the Actions group has one more row, "Immersive reading" (the `maximize-2` icon, chip Z), named "Exit immersive reading" while it is on (`minimize-2`). The highlight is a `Chosen` fill and starts on the first row; Up and Down move it and wrap, it scrolls into view, Return or a click runs the row and closes the palette, Escape or Cmd/Ctrl+K closes it. Typing filters and ranks (prefix, word start, substring, then scattered letters; case and spaces ignored) over the loaded names only, with no request; at most 50 rows, and the matches are one ranked list without headers, each row naming its group in muted text after its name. No match: one muted line, "No matches". Accessibility: the field is labeled "Go to…", rows are list items named by their group and name, the highlighted one is announced with `Announce`, and the result count is announced as the query changes. Drawn in `design/screens/timeline.html`, `palette`, `palette-query`, `palette-none`. |
| `ui.DialogBase` (key sheet) | The keyboard shortcut sheet, drawn like the subscription dialogs: two columns (timeline, reader) and an "Anywhere" group below (the Reader column lists "Immersive reading" Z and "Leave immersive reading" Esc before "Back to the list" Esc), each key a small chip; one column below a 760 wide window. Escape or Close dismisses it. |
| `ui.DialogBase` (lightbox) | The pictures of a social or image entry, full window, see "The lightbox" in Layout and spacing. A click on a picture in a row opens it on that picture, Space on a chosen row on the first; the title and text still open the browser. It uses the dialog's focus return, Escape and key blocking, with its own paging keys; the pictures come through the image proxy and the decode cache like the rows' do. No video. Drawn in `design/screens/timeline.html`, `lightbox`, `lightbox-failed`. |

**Tooltips.**

- A button that shows a text label has no `Tooltip`: not a segmented control, and not a label that is a number, may be
  cut short or hides in a narrow window. The label is the name, and a tooltip that repeats it is noise. A shortcut is no
  reason to keep one; the key sheet lists the keys.
- An icon-only button keeps a `Tooltip` with the same text as its accessibility `Label`.
- The accessibility `Label` stays on every button, whether or not it has a tooltip.

MyGo's `Tooltip` has no placement option: it sits above the element and flips below only where there is no room
above. A tooltip on an icon-only button in the top row (the timeline and reader toolbars) can therefore cover the macOS
window buttons. This is a known limit of MyGo; Oriel does not work around it with its own `TooltipBase` anchoring.

The tables below say what to reach for as screens are added.

### Actions

| Need | Use |
|---|---|
| The main action of a group, once | [Button](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/button.md) (`PrimaryButton`) |
| Other actions | [Button](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/button.md) |
| A text button in settings, subscriptions or connect | `theme.Button` (`internal/ui/theme/button.go`): one 32 high button, 26 when small, in four styles (`Secondary`, `Primary`, `Danger`, `Plain`); the sizes and fills are in `design/tokens.md`. A screen draws no button of its own. `Danger` is text in `Danger` with no fill until hovered, never a solid red button |
| Open an entry's original page in the system browser | [Link](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/link.md) |
| Several related actions in one place | [Menu button](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/menu-button.md) |
| Actions on a specific entry or subscription, on right click | [Context menu](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/context-menu.md) |
| An on/off command that stays pressed | [Toggle](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/toggle.md) |
| Choose one of a few views or modes | [Segmented control](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/segmented.md) |
| Window-level commands | [Toolbar](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/toolbar.md) |

### Choices

| Need | Use |
|---|---|
| A setting applied immediately (for example a per-subscription switch) | [Switch](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/switch.md) |
| A yes/no value submitted with a form, or several independent options | [Checkbox](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/checkbox.md) |
| One of two to five visible options (for example a subscription's kind) | [Radio](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/radio.md) |
| One of many options | [Select](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/select.md); with typing to filter, [Combobox](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/combobox.md) |
| A value in a range | [Slider](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/slider.md); an exact number, [Number input](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/number-input.md) or [Stepper](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/stepper.md) |

### Text

| Need | Use |
|---|---|
| Free text, one or many lines (a feed URL, an RSSHub path, a prompt) | [Text input](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/text-input.md) |
| Searching the timeline | [Search field](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/search-field.md), or [Find bar](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/find-bar.md) for text in the reader |
| Mute keywords | [Token field](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/token-field.md) |
| Rename a subscription in place | [Editable text](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/editable-text.md) |
| Labeled controls with help and errors | [Form](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/form.md) |

### Collections

| Need | Use |
|---|---|
| The timeline, and other long lists of one kind | [List](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/list.md) |
| Rows with sortable, resizable columns (for example subscription health) | [Table](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/table.md) |
| Hierarchy | [Tree](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/tree.md), or [Outline](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/outline.md) when large |
| Thumbnails | [Grid view](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/grid-view.md) |

### Containers and navigation

| Need | Use |
|---|---|
| Scrolling content, such as the reader | [Scroll view](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/scroll.md) |
| Columns and rows of controls | [Grid](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/grid.md) |
| Two resizable panes (timeline and reader) | [Split view](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/split.md) |
| Optional detail | [Collapsible](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/collapsible.md), [Accordion](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/accordion.md) |
| Top-level sections of the app | [Sidebar](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/sidebar.md) |
| A few peer pages inside a screen | [Tabs](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/tabs.md) |
| Pages with a history | [Router](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/navigation.md), with [Back and forward buttons](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/history-buttons.md) |

### Dialogs, messages and status

| Need | Use |
|---|---|
| A task that needs the user's full attention | [Dialog](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/dialog.md) |
| Confirm something important or destructive (deleting a subscription and its entries) | [Alert dialog](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/alert-dialog.md); for system file and message dialogs (OPML import and export, the startup error), `mygo.Dialog` ([desktop APIs](https://github.com/egoist/mygo/blob/v0.5.5/docs/native.md)) |
| A small panel tied to an element | [Popover](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/popover.md) |
| A short label or hint for a control | [Tooltip](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/tooltip.md) (never the only place for essential information) |
| Feedback that needs no decision | [Toast](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/toast.md) |
| Known progress / unknown duration (a fetch, an AI summary) | [Progress bar](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/progress.md) / [Spinner](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/spinner.md) |
| A count or short status (unread entries) | [Badge](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/badge.md) |

Overlays share focus and Escape behavior; see
[Overlays](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/overlays.md). Rules of thumb:

- Prefer undo over confirmation. Confirm only what cannot be undone, with a button named for the action ("Delete"),
  not "OK", and Cancel as the Escape action.
  Deleting a subscription does both: the dialog asks first, since its starred entries go too, and the toast that follows
  offers Undo for a few seconds.
- Do not stack dialogs. Use a toast for news that needs no answer.
- Errors say what happened and what to do, next to the control that caused them (`Error`, or a
  [form](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/form.md) field). The server returns stable error codes;
  the UI maps each to localized copy and never shows the raw code alone.
- AI summaries and translations are on demand and can be slow or fail: show that the work is going on, keep the
  original text readable meanwhile, and say plainly when it failed.

## Windows, menus and navigation

- **Windows.** One main window; set `Title`, `Width`, `Height`, and `MinWidth`/`MinHeight` so content never clips.
  Oriel's window is 960×640 with a minimum of 640×480. `StateKey` (`oriel-main`) remembers size, position and the maximized state between runs, in `window-state.json` in the
  user data directory, a client-side file next to `preferences.json`. A saved position that no longer shows on a
  connected display falls back to a centered window on the primary display, at the saved size as far as that display
  allows. A second launch brings the existing
  window to the front. See
  [Windows with native UI](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/windows.md) and the
  [window options](https://github.com/egoist/mygo/blob/v0.5.5/docs/windows.md).
- **Title bar.** No title bar: `TitleBarStyle: mygo.TitleBarHidden`, the macOS window buttons placed with
  `TrafficLightPosition` so they sit over the sidebar's top, centered in the 52 high toolbar row. The view then owns
  the room the window controls take (`c.TitleBar()`: on Windows the caption buttons at the right, which the toolbar
  leaves free) and the draggable area: the toolbar row's empty space and the sidebar's top (the sidebar button apart) call `DragWindow`. The
  window's name stays set (`Title`) for the Window menu, the Dock and screen readers. With the sidebar hidden the
  macOS buttons sit over the toolbar row, which pads its left by `TitleBar().Left` (see Hiding the sidebar).
- **Glass and ink.** Oriel draws its toolbar controls in glass (the [glass
  plugin](https://github.com/egoist/mygo/blob/v0.5.5/docs/plugins/glass.md)) and its sidebar as an ink pane. The pane
  is not glass: it is a plain floating view with an opaque `Background` of ink (warm charcoal `#37332e` in light, deep teal `#1a2422`
  in dark; at 92 to 96% opacity glass added only softness and a muddy mix with the backdrop), radius 14, a 0.5 DIP
  inner rim in the pane ink, in light-on-dark roles of its own (tokens.md): the root view takes them from
  `theme.PaneTheme`, calls `c.SetTheme(paneTheme)` before building the sidebar and sets the main theme back after. The
  toolbar has one glass capsule (a 32 high pill, tint rgba(255, 254, 251, 0.66) in light and rgba(58, 63, 62, 0.52) in
  dark, radius 16, 0.5 DIP rim): the segmented control in the timeline, Summarize and Translate in the reader.
  The toolbar row is a solid bar: `Background`, 52 high, over the content and under the controls, in the timeline, the
  reader and Settings alike (`theme.Edge` draws it; `glass.ScrollEdge{}` is not used). At the top of the content it has no edge; once the content has
  scrolled under it a 0.5 DIP `Border` hairline shows along its bottom, which fades in over 120 ms. A fade, even a tall and opaque one, let text show through
  behind the icons; the bar does not.
  Content stays on opaque `Background`; glass is for chrome only, never for entries, the article, dialogs, search or
  bare toolbar icons. Text on glass keeps AA contrast, and with high contrast glass becomes opaque (the capsule
  `Surface`, the pane is already opaque) with a `Border` rim (MyGo reports no Reduce Transparency setting, so high
  contrast is the one switch). The plugin draws the same glass on Windows and Linux, so the look is one on every
  platform; no system vibrancy is used.
- **Menus.** The macOS menu bar belongs to the app. Oriel's menu has an app menu (About, check for updates, Quit),
  the Edit menu (`RoleEditMenu`, so copy and paste work), the Timeline menu (the commands that have a single key:
  show unread only, show all entries, show starred only, mark all as read, load new entries, and, while the reader shows, immersive reading; no accelerators, see
  Timeline keys), a Subscriptions menu with "Refresh All" (Cmd/Ctrl+R) and the Window menu (`RoleWindowMenu`); "Connect to
  Server…" in the app menu opens the connect screen and "Settings…" (Cmd/Ctrl+,) the settings. Use roles
  (`RoleAbout`, `RoleQuit` and the others) for standard items: they carry the right label and shortcut, and items
  that only macOS has are left out elsewhere. Give custom items an accelerator with `CmdOrCtrl`. Every command
  reachable by a toolbar button should also be in a menu. See
  [Menus](https://github.com/egoist/mygo/blob/v0.5.5/docs/menus.md).
- **Navigation.** Choose the lightest structure that fits: a single screen;
  [tabs](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/tabs.md) for a few peer views; a
  [sidebar](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/sidebar.md) with a
  [router](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/navigation.md) for several top-level sections (timeline,
  subscriptions, settings). A router gives back/forward keys, state restored per page and focus handling. Give every
  page a `Title`.
- **Leaving the app.** Video and social entries have no embedded player; opening them hands the link to the system
  browser. Say so in the control ("Open in browser"), not only with an icon.
- **Toolbar.** Frequent window-level commands, in order of importance; controls that do not fit overflow into a menu.
  One glass capsule at most per toolbar; everything else is a bare icon or the flat search field. Search is last, at the far right.
- **Toolbar buttons.** One spec for every bare icon button in a toolbar, in the timeline and the reader alike: 32 by 32, a 16
  icon, radius 8, 4 between neighbors. No fill at rest; hovered `SurfaceHover`, pressed `SurfacePressed`; the icon is
  `TextMuted`, `Text` when hovered or pressed; disabled keeps no fill and the icon turns `Disabled`; an on button (the
  reading-time budget, a pressed toggle) sits on `Soft(Accent)` in `Accent`, and a starred one is `Star`. A button that does not apply to the
  current article is disabled, never hidden, so the toolbar keeps its shape; it keeps its place and its label, greyed, and its
  tooltip and accessible name give the reason (Translate on an article already in the target language still reads
  "Translate"; the tooltip and name say "Already in Simplified Chinese"). It takes no focus or click. The glass capsule and search are pills (radius 16) of their own. The row actions on a row
  (28 by 24, radius 6, 2 apart) are their own smaller spec with the same colors, but their hover fill is `SurfacePressed`, since
  they sit on a row that is itself `SurfaceHover`-filled; a 32 button would not fit the meta line. In a narrow window the end buttons of the reader move into More in a
  fixed order, copy link first, then open in browser, then read/unread (below 500, 440 and 380 wide in the main column); the
  640 wide minimum window reaches the first step only. The menu then lists them above Copy as Markdown, under a hairline.
- **Notifications.** Use system notifications only for events the user would want when the app is in the background.
- **Timeline keys.** J or Down and K or Up move between entries, N goes to the next unread entry below the chosen one
  (loading the next page when needed, with a toast when there is none), O or Enter opens the chosen one (an article in the
  reader, anything else in the browser) and marks it read, Space opens the pictures of a chosen social or image entry in the lightbox, S stars it, M toggles read, V marks the entries fully in view as
  read and scrolls the first one below them to the top (see "Mark visible" below), `/` and Cmd/Ctrl+F go to search, Escape clears it. The list keys are
  window-wide but stop while the search field has the focus or a dialog is open, so typing never moves the list.
  Cmd/Ctrl+K opens the command palette (see Components), which goes to any subscription, kind or Settings page and runs the common actions. Cmd/Ctrl+N adds a subscription, Cmd/Ctrl+R refreshes all subscriptions (see "Refresh all" below), Cmd/Ctrl+Z undoes the last "mark as read" action while its toast shows, and `?` or Cmd/Ctrl+/
  opens the key sheet, which the Help menu also lists as "Keyboard shortcuts".
  Four more keys act on the list, under the same rules (not while the search field has the focus, a dialog is open
  or the reader shows): U shows unread only, A shows all entries, `*` (Shift+8) shows starred only, Shift+A marks
  everything in the scope as read, with the same toast and Cmd/Ctrl+Z undo as the toolbar button (inert where the
  button is disabled), and R loads the entries the "N new" pill offers and goes to the top (inert when there is no
  pill). Single keys cannot be menu accelerators, as they would fire while a field has the focus, so each of the five
  commands is an item without accelerator in the Timeline menu, and the key sheet and the command palette show the
  keys. The key sheet's Timeline column lists them after "Mark visible as read".
  Every entry is also a focusable element with its own name, so Tab reaches it.
- **Refresh all.** One command asks the server to check every active subscription now: the Subscriptions menu item
  "Refresh All", Cmd/Ctrl+R (also in the reader, but not while a dialog, the palette or the lightbox is open; a key
  sheet row "Refresh all subscriptions" under Anywhere) and the palette action of the same name with the
  keys as chips. There is no button of its own: the heading already holds the failing count and the add button. While it
  runs, the heading's failing count (`Warning`) gives way to a muted progress link with the loader icon turning at 12
  and "3/12" in 11 medium, named "Refreshing, 3 of 12 checked" (no tooltip: the "3/12" is the visible text), and a screen reader hears "Refreshing
  12 subscriptions" once; pressing the command again does nothing. It is not a spinner on the list: the list stays
  where it is. When every subscription has been checked a plain toast, the `circle-check` icon on the `Inverse` tile,
  says "Checked 12 subscriptions", or, with the `circle-alert` icon, "Checked 12 subscriptions, 2 failed"; the
  failing count returns in the heading. After a timeout the toast says "Checked 5 of 12 subscriptions, the rest will
  follow". With nothing active to refresh it says "No subscriptions to refresh", and when the request fails "Can't
  refresh now. Try again later." States `refreshing`, `refreshed`, `refreshed-failed`, `refreshed-part` and
  `refresh-none` in `design/screens/timeline.html`.
- **Marked by scrolling.** With Mark as read set to After scrolling past, the rows that left the top of the list are marked once the scrolling has stopped for about half a second, and the usual 8 second toast appears at the bottom of the list, in the same markup as "Marked visible": "Marked {n} scrolled-past entries as read", Undo, the Cmd/Ctrl+Z hint and a close button. It shares the one undo with the other batch marks. No toast when nothing was marked. The dwell option has no toast: the reader's read button simply turns to read and the screen reader says "Marked as read". Drawn in `design/screens/timeline.html`, `marked-scrolled`.
- **Mark visible (V).** One key clears a screenful: it marks the entries fully inside the list area as read, then
  scrolls so the first entry below them is at the top, chosen and focused. "Fully inside" counts a row (a card in the video
  grid, a tile in the picture wall) that lies wholly between the bottom of the toolbar, which the list scrolls under, and
  the bottom of the window; a row cut by either edge is not marked and, at the bottom, is the one scrolled to the top. The
  range runs in list order from the first entry fully in view to the last one before the first entry that is not; in a
  grid the cards of a row count whole, and in the picture wall, whose columns differ in height, that is the entries up
  to the first tile not wholly in view (the square picture grid counts as the video grid does). Entries already
  read inside the range stay as they are and are not counted. With no unread in the range V only scrolls and shows no
  toast, so it also pages; at the end of the list it marks and stays. If the last loaded entry is wholly in view but
  another page is to come, it is left unread and becomes the one scrolled to, while the page loads. The scroll is
  immediate (MyGo's `ListState.ScrollTo` and `GridState.ScrollTo` do not glide) and not undone by Undo. V works in All and
  Unread without a search, like "Mark all as read", and does nothing under Starred or a search. The toast is the shared
  one: "Marked 5 visible entries as read" with Undo and the close button (`marked-visible` in the mockup). The toast is
  announced, and the new chosen entry is then read by its own name; there is no extra announcement. The trackpad swipe is
  not possible in MyGo 0.5.5 (no gesture callback) and is left out.
- **Sidebar filter and sort.** Under the Subscriptions heading, above the list, one row 28 high with a 4 gap, inset 3 at the
  right so the toggle sits on the pane line: the filter field, which grows, and the Unread first toggle (`side-filter`, `side-filter-none` and
  `side-unread-first` in the mockup). The field is a flat fill of the pane's hover (pane ink 5.5%, 8.5% under the
  pointer and while it has the focus, which adds the 2 DIP focus ring of the pane),
  radius 8, 10 left padding, a 14 magnifier in `TextMuted`, the input at 13 in `Text` with the placeholder "Filter
  subscriptions" in `TextMuted`, and from the first character a 20 square clear button ("Clear filter", an x at 12) at
  the right. The toggle is a 28 square icon button, radius 8, with the arrow-up-down icon at 14 in `TextMuted` (hover:
  `SurfaceHover` fill, `Text2` icon); while on it is a `Chosen` fill with the icon in `Accent`, as a chosen item is. Its
  label and tooltip are "Unread first"; the state is exposed as on or off to a screen reader, never by color alone
  (the fill and the icon change, and the label stays). It is a toggle in the row, not an entry of a menu, so it can be
  seen and found by keyboard; the heading already holds the failing count and the add button, and a third control
  there would cut the name at the narrow width. The row is always there while there are subscriptions and is hidden
  with none. The field matches the name, ignoring case, anywhere in it; it filters the list of the chosen kind (and
  the failing ones when Show all is offered), and the list is shown from the first character, with no pause. When
  nothing matches, the rows give way to one muted line, "No matching subscriptions" (「没有匹配的订阅」), 13 type, 10
  inset, a status for a screen reader. The filter is not kept: the field is empty on the next start, and choosing a
  kind or a subscription leaves it as it is. The order is by name (the store's order) or, with Unread first on, by
  unread count descending and then by name, ignoring case; a paused subscription counts as none. The choice is kept
  on this device in the preferences (`sourceSort`). Keys: Tab reaches the field and then the toggle before the list;
  in the field Escape clears the text and, when it is empty, gives the focus to the chosen item of the sidebar; Down or
  Enter gives it to the first match. While the field has the focus the keys of the list and sidebar (J K O S M N V,
  H L, 1 to 5) type into it instead, as in the search field.
- **Waiting for a first fetch.** After a subscription is added or refreshed its row in the sidebar swaps the warning
  mark (if any) for a 13 `Spinner` in `TextMuted` before the unread count, and the row's name for a screen reader gains
  ", fetching" (`fetching` in the mockup). When the fetch is done the spinner goes and one plain toast (a check-circle
  icon, or the circle-alert in `Warning` on failure; no Undo) says the result: "Fetched 12 entries from “Go Blog”" (or "“Go Blog” fetched, but has no entries yet"),
  "“Go Blog” has 3 new entries", "“Go Blog” has nothing new", or "Couldn't fetch “Go Blog”: reason" (`fetched`,
  `fetched-failed` in the mockup). It is announced like any toast. The wait ends silently after 60 seconds.
- **Sidebar keys.** 1 to 5 switch to All and the four kinds, in the sidebar's order, from anywhere in the timeline. H or
  Left (expanding the sidebar first if it is hidden) moves the focus to the chosen item of the sidebar (the kind, or the subscription when the scope is one), Up and
  Down walk its kinds, which choose as the focus passes, and its subscriptions, which wait for Enter (one vertical
  focus group each, so Tab still goes through the sidebar in two stops, and Left and Right are not the groups' but go
  back to the list); the focus stays on the item chosen, and L, Right or Escape gives the focus back to the list, the chosen entry
  focused. While the sidebar has the focus by the keyboard (H, Left, Tab, the arrows) the entry keys (J K O S M N V,
  Up and Down) do not touch the entries; `/` still goes to search. A click on a kind or subscription does not count:
  the entry keys keep acting on the list. Shift+F10 or the Menu key opens the context menu of the focused subscription
  or entry, which MyGo does natively for an element with `ContextMenu`; a Mac keyboard has no Menu key, so the key
  sheet lists Shift+F10 alone there. All of these are off while the search field has the focus, a dialog is open or the reader shows. A
  scope switched from the keyboard is announced with `c.Announce` ("Videos, 12 unread"), the focus staying where it was.
  The focus ring on a sidebar item is 2 DIPs of the pane's own accent at 70% (`sidebar-focus` in the mockup).
- **Reader keys.** While the reader shows, J and K go to the next and previous article, N to the next unread one (a
  toast when there is none, staying on this article), S stars, M toggles read, O
  opens the original in the browser, Z enters immersive reading (see above) and Escape goes back to the list, the entry it opened from focused (while immersive reading is on, Escape leaves it first). The article
  takes the focus when it opens, so the arrows, Space, Page Up/Down and Home/End scroll it, and Tab goes through its
  links. Every icon-only toolbar button names its key in the tooltip; the back button carries its name as text and has no
  tooltip, its key is in the key sheet.
- **Settings.** The settings take the whole window, in place of the timeline: their own sidebar has "Back to the
  timeline" on top, one item per group (Appearance, Fetching, Rules, Import and export, AI, Devices, Activity and storage) and the server bar, which says whose
  settings these are and does not open the connect screen. They open from the gear beside the timeline's server bar
  or "Settings…" in the app menu; the back button and Escape return to the timeline as it was. Escape never throws typing away: with a text field focused it only takes the focus off the field (to the back button; the text stays), and with text not saved (AI endpoint, model, key, language and limits, the prompts, the RSSHub instances, the muted keywords) it does not leave and the page does not change, it only announces "Some changes are not saved" with `Announce` (no new dialog or visual; the Save button already turns primary); otherwise it returns. The back button always returns. A MyGo text field does not consume Escape (the headless test shows it reaches the window shortcut), so the page decides. The page's toolbar title lines up with the left edge
  of its content column. **Page width.** A page is one column centered in the room beside the sidebar, its width
  taken from the page and the room, not from the window: a form page (Appearance, Fetching, Rules, Import and export, Activity and storage) keeps
  a column at most 600 wide, padding included, for a readable line; Devices is a list and widens to 880 once the room
  is 656 wide (a window of 880); AI splits in two columns, 1040 wide at most with a 24 gap, once the room is 976 wide
  (a window of 1200). Below those widths a page is the single 600 column, padded 32 (20 below a 760 wide window).
  Text outside a card (the lead line) never runs past 600. The RSSHub instances are an ordered list in one
  multi-line monospace text input (as the prompts and mute words), one address per line, the first the primary; Enter
  breaks the line, so only Save sends it. A line that is not an http or https address with a host name is named under
  the field ("Line 2 is not ..."), the field takes the `Danger` outline, nothing is sent and the focus stays. Five
  addresses at most. A setting is saved
  where it is changed: the RSSHub instances with their Save button, the retention as soon as another is
  chosen; the line under it says it was saved, or why not, and a refused value keeps the focus in its field. OPML
  files are chosen and saved with the system's file dialogs, read and sent in the background; the result stays
  under the button, with the subscriptions that could not be added and why, until dismissed. Below the OPML row,
  "From another reader" with an "Import from Reader…" button opens a 440 wide dialog (the sheet of the subscription
  dialogs: radius 16, 20 padding, a 15 semibold title with a 28 close button, the buttons in the footer, the form scrolling
  when the window is low). Step one: a Service pair of small toggle buttons (Miniflux, FreshRSS), the server address in a
  field with a `https://miniflux.example.com` placeholder, for FreshRSS a User name field, and the credential in a
  masked field (API token, or API password for FreshRSS) with a 12 `TextMuted` line saying it is used for this import only
  and kept in memory; Connect is disabled until all are filled, and an address that is not http or https is named under
  its field with the `Danger` outline. Then a polite status with a turning loader, the stage ("Reading starred
  entries…") and a 6 high progress bar with "300 so far" or "204 of 340"; Cancel stops at once. The preview lists
  Subscriptions, Starred entries, Read entries and Folders and tags with tabular figures in one soft card, the folders
  row with a 12 `TextMuted` note that Oriel has no folders yet, and says nothing has been added; Import is the
  primary button. The result is a warning-toned box when anything was left out, else plain, with one line per figure
  (added, already there, marked read, read entries not in Oriel, folders not imported) and the list of what was not
  imported with the reason, as for OPML. A failure is a `Danger` box with a reason one can act on (the server turned
  the credential down, cannot be reached, asked to slow down, ...), with Try again. The credential is never shown
  again after Connect. Closing the dialog while it works cancels. Drawn in `design/screens/settings.html`, states
  `reader` to `reader-failed`. Last on the page are two rows like the OPML ones, "Back up" and "Restore": a label, a 12 `TextMuted` help line (what a backup holds, and that sessions and the AI API key are left out; that restoring replaces what is here), and a secondary button, "Back up…" (database icon) and "Restore from backup…" (counter-clockwise arrow). Both use the system's file dialogs, filtered to `.oriel-backup`, and work
  in the background; while one runs the buttons are disabled and a polite status with a turning 14 loader reads
  "Backing up…" or "Restoring…". The result stays under the buttons until dismissed: a plain box for "Backed up to …"
  and "Restored 12 subscriptions and 480 entries", a `Danger` box with a reason one can act on for a failure, and
  the line that the data is as it was for a refused restore. After choosing a file, "Replace your data with this
  backup?" is an alert dialog (the one that confirms deleting a subscription): the body says the subscriptions,
  entries and settings now here are replaced and this cannot be undone, the focus starts on Cancel, and the
  confirming button, "Replace data", is the `Danger` one. Drawn in `design/screens/settings.html`, states
  `backup`, `restore-confirm`, `restoring`, `restored` and `restore-failed`. The last row, "Back up automatically", is a
  label, a 12 `TextMuted` help line and a switch (off by default); below it, always shown and parted by a hairline,
  three 32 high lines with a 96 wide label: Folder (the path in 13 mono, cut with an ellipsis, or "No folder chosen
  yet" in `TextMuted`, and a small "Choose…" button with a folder icon that opens the system's folder dialog), How
  often (a segmented control, Every day and Every week) and Keep (a 64 wide number field and "backups" in 12
  `TextMuted`), then a 12 status line: the last success ("Last backup: Today 03:00, Oriel-….oriel-backup"), "No backup
  yet", or, after a failed try, the warning-toned "Last backup failed: …. Trying again within the hour." with the
  alert icon. On a remote server the folder is not on this computer, so it is a text field instead of the button,
  with a 12 `TextMuted` note that the path must exist on the server's machine. A refused value is named under its line
  in `Danger` and the control goes back to the server's value. States `auto-off`, `auto-on`, `auto-failed` and
  `auto-remote`. Signing a device out asks
  first; the current device is marked and cannot be signed out. In the wide column Devices is a table in one card:
  a header row 36 high (11 medium, `TextMuted`, 12 in Chinese) over rows 48 high parted by the inset hairline, columns
  Device (a 28 glyph tile, the client's name at 13 medium and the "This device" badge), Platform and Signed in
  (`TextMuted`, 88 and 96 wide), Last used (116 wide, right-aligned, `Text`, tabular figures) and a 96 wide last column
  that holds "Sign out…" (the small danger button), empty on the current device. The first column takes the rest and
  its text is cut with an ellipsis; the gap between columns is 16. Below the table's room the rows are the stacked
  ones (name and badge over "Signed in … · last used …", Sign out at the right). It is a `ui.Grid` with column tracks,
  not `ui.Table`: the devices are few and sit in the page's own scroll, in the card's style. The AI page
  is a "Connection" card (endpoint, model, key, language, Test and Save) and a "Prompts" card, each under its own
  title; in two columns the prompt boxes are 120 high. The AI group has the endpoint, model, API key and
  output language in one card with its own Save; each field, the model's too, has its label above and takes the
  card's width; and the two prompts in a second card. The key field is always
  empty; a badge says "Configured" or "Not configured", a new key replaces the old one and "Remove key" deletes it.
  The card opens with a Preset row: a "Preset" label and two small buttons, Ollama and LM Studio. Choosing one fills the
  endpoint (unsaved) and swaps the model field's placeholder for that runtime's usual name; it never touches a model name
  or key already typed, and the button matching the current address is shown pressed (`Accent` soft fill). The model
  placeholder is a hint, not a value. After the language row come two number fields side by side, "Summary limit" and
  "Chunk size" (characters; the placeholder is the default, 24000 and 6000), monospace like the endpoint, with a one-line
  help and an error line under a refused value. Under the endpoint a 12 `TextMuted` line says where the text goes: it
  leaves this device for the endpoint's host, or, for localhost and loopback addresses, stays on this computer
  (`ai-local`). A second pair of number fields, "Automatic requests a day" and "New entries per fetch" (defaults 200
  and 10), follows with a one-line help that your own requests are not counted; under it a 12 `TextMuted` status line
  shows today's count, and once the day's cap is reached an alert icon in `Warning` with the line in `Text`
  (`ai-cap-reached`).
  Test sends a short request with the saved settings, so it is disabled while there are unsaved edits; its result
  stays beside the buttons. A success is the "Connected" line with a check; under it, one 12 line per further result
  (check and "Success" color for "The model is in the endpoint's list" and "The context holds the limits"; the alert
  icon in `Warning` and the text in `Text`, since `Warning` is never text, for a context too small for the limits, which is a hint, not a failure). A result the
  endpoint cannot give is not drawn at all. A failure is one `Danger` line that says what to do: cannot connect (check the
  address; start the server if it is on this computer), the key was turned down, no such model (check the model name), or
  too slow. Errors never show the key. Each prompt has "Restore default", disabled while it is the default; it acts at once, puts the default in the box (still unsaved) and offers Undo in a toast, "Summary prompt restored to default", with a close button (`prompt-restored` in the mockup). Undo puts back exactly the text that was there, and does nothing if the box was edited again since. The Activity and storage group (icon: database) is four cards in the 600 column, each under its own title, and is the only page that refreshes by itself: every 5 seconds while it is shown (every 2 while a vacuum runs), and with a small "Refresh" button at the right of the page lead. The "Storage" card has a row each for Database size, Entries, Cached images ("N images, X") and Starred copies ("N images, X"; the pictures kept for starred entries, which live in the database), label left and figure right in tabular numerals; under them a hairline and the "Compact database" button (secondary, 32 high) with a 12 `TextMuted` note beside or under it, "Rewrites the database file to give back unused space. It can take a while and runs in the background; Oriel stays usable, and new fetches wait until it is done. About X can be freed." Pressed, the button becomes busy (spinner, "Compacting…") and cannot be pressed again; when it ends a status line shows a check and "Compacted. Freed X" in `Success`, or the alert icon in `Warning` with the reason, and the button is usable again. There is no confirmation, since nothing is lost. The "Diagnostics" card, second, has the "Export diagnostics…" button (secondary, 32 high, download icon) with a 12 `TextMuted` note beside or under it: "Saves the version, the system, the recent errors, the end of the log and the host name and failures of each subscription as a text file. It has no password, keys, full addresses or article text, and it stays on this computer. The log itself is in the logs folder of the data directory, on the computer that runs the server." Pressed, it asks where to save with the system save dialog (default name `Oriel-diagnostics.txt`) and the button is busy ("Exporting…") while the file is fetched and written; then a status line shows a check and "Exported to {file}." in `Success`, or the alert icon in `Warning` with "Couldn't export. Check the connection and try again." (or "{file} could not be saved."); cancelling the dialog shows nothing. The Help menu has the same action, "Export Diagnostics…", which opens this page and starts the export. The "Fetching now" card lists the sources being fetched, one 36 high row each with the source's glyph and name; none shows one muted line ("Nothing is being fetched right now."). The "Recent fetches" card has one row per source, 44 high: the result of the newest attempt as an icon (check in `Success`, alert in `Danger`), the name at 13 medium, a 12 `TextMuted` summary ("3 min ago · 2 new" or "3 min ago · failed") and a chevron that turns when the row is open. A row opens in place to its attempts, at most 10, one 28 high line each (result icon, time ago, then status, duration and new count in 12 tabular figures), and a failed attempt carries its reason on a second 12 `Danger` line; only one row is open at a time and only the open row builds its attempts. A note under the list says how many attempts are kept per source. With no attempts yet the card shows one muted line. Loading uses the page skeleton and a failure the usual "Could not load" with Retry. Drawn in `design/screens/settings.html`, `activity`, `activity-open`, `activity-empty`, `vacuum-running`, `vacuum-done`, `vacuum-failed`, `diagnostics-exported`, `diagnostics-failed`.
  The Rules group, after Fetching (icon `list-filter`), has a lead line (a rule runs once, when an entry first arrives, and
  never changes the entries you already have; muted keywords work on every entry at any time) and two cards. The first
  holds one row per rule: the condition as a sentence on top (13 type, medium, "Title or summary has “a”, “b”", "By
  “name”", or "Every new entry" when nothing narrows it; wrapped, never cut), and under it a 12 `TextMuted` line, the
  scope (the subscription or "All subscriptions", then the kind when one is set), an arrow, the action's icon and name
  ("Star", "Mark as read", "Star + Mark as read"). At the right a Switch (applied at once; an off rule dims its text to
  55% and shows an "Off" badge) and a 28 square ⋯ button that opens a menu (Edit…, Delete in `Danger`) anchored under
  it, right edges aligned, on `Raised` with the sheet shadow and radius 12; rows 32 high with radius 6. The last row of the
  card holds the "Add rule…" button at the right. No rules: one row with the list-filter icon, "No rules yet." and a
  `TextMuted` example line. The second card is "Muted keywords": a help line, a multi-line monospace text input (one
  keyword per line, as the RSSHub instances), a 12 tabular `TextMuted` line "Hides {n} entries right now" that
  follows the typing (asked of the server after a short pause, never on the drawing path), and a Save button that is
  primary while the text is edited and disabled otherwise. A refused list keeps the focus in its field, takes the
  `Danger` outline and says "At most 200 lines, up to 100 characters each." under it. Add and Edit open a 440 wide dialog
  (the sheet of the Reader import dialog: radius 16, 20 padding, title with a 28 close button, buttons in the footer, the
  form scrolling when the window is low): a medium line "When a new entry matches" and under it a 12 `TextMuted` line "Everything you fill in has to match.
  Fill in at least one.", then Subscription and Kind (full width menus, "All subscriptions" and "Any kind" first), Keywords
  (a 64 high multi-line input, one per line, any of them matches) and Author (a single line, a part of the name), then
  "Then" with two Checkboxes (Star, Mark as read; 16 box, radius 5, `Accent` fill with a check when on, 30 high rows) and
  a 12 `TextMuted` note: only entries that arrive from now on are affected. Save is always enabled; pressing it with no
  condition or no action names the problem under the Author field ("Fill in at least one condition.") and under the
  checkboxes ("Pick at least one action."), with `Danger` outlines on the empty inputs and the boxes, and the focus
  stays. A server refusal (more than 50 rules, a subscription that is gone) is one `Danger` line above the buttons.
  Deleting acts at once and shows a toast "Rule deleted" with Undo, which puts the rule back; the rows below move up.
  If the rules cannot be loaded the page shows the page error pattern, "Rules did not load" with Retry. Drawn in
  `design/screens/settings.html`, `rules`, `rules-empty`, `rule-menu`, `rule-add`, `rule-invalid`, `rule-edit`,
  `rule-deleted`, `muted-edited`, `muted-invalid`, `rules-error`.
  The Appearance group, first in the list, is
  different: it is kept on this device, not on the server, and a line at the top of its page says so ("These apply only
  to this device."), where the other pages say settings are kept on the server. One card holds a row each for Theme
  (segmented: System, Light, Dark), Interface language (a menu: System, 简体中文, English, each language name in its own
  language; chosen, it applies at once and the screens are rebuilt), Interface font (a button that opens the font picker
  below: "System font" and the current family first, then the installed families, each set in its own face) and, in a second card titled "Reading", Reading font
  (a button like Interface font that opens the same font picker, with "Follow interface" (the default) and "Serif" pinned above a hairline, each drawn in its own face, then the current family if it is one, then the installed families; its value shows the current choice in its own face), Text size
  (segmented: Small, Medium, Large, Extra large) and Line spacing (segmented: Compact, Standard, Relaxed) and Chinese typesetting (a switch, on by default; see Chinese typesetting). A second card, titled "Timeline" under the Reading card, has two rows. List density (segmented: Comfortable, Compact; default Comfortable), with the help "How tight the rows sit. Compact keeps everything and only tightens the spacing.", then Mark as read (a popup menu like Interface language: When opened (default), After a short stay, After scrolling past, Never automatically; a device setting; the help under it follows the choice: "Opening an entry marks it as read.", "An article opened in the reader is marked as read after the time below; one opened in the browser is marked at once.", "An entry is marked as read when it scrolls off the top of the list; opening it does not.", "Nothing is marked automatically. Only you mark: the read button, M, V or Mark all as read."), and, only while After a short stay is chosen, a Stay time row (segmented: 3 s, 5 s, 10 s; default 5 s) with the help "How long an article stays open before it counts as read.", and under it Show an article once (a switch, on by default, a setting of the server rather than of this device), with the help "When several subscriptions carry the same article, the timeline shows one row." Drawn in `design/screens/settings.html`, `appearance`, `density`, `mark-read` and `mark-read-scroll`. A third card, titled "View as" under the Timeline card, has one row per kind, each a segmented control of its own values (Articles: List, Compact; Videos: Grid, Large cover, List; Social: Card, Compact; Images: Wall, Grid, Card), the default first, each row just the kind's name and its control; the last row of the card is a note in 12 `TextMuted` that says "Grid and Wall are used in Videos and Images. Elsewhere those entries appear as rows or cards." The segments are 26 high in a 32 high capsule, as in Theme, so a row never needs a popup. Drawn in `design/screens/settings.html`, `viewers`. A sample paragraph on `Surface` under them shows the result, set in the chosen Reading font (the column width is not shown there). Drawn in `design/screens/settings.html`, `reading-font`, `reading-font-open` and `reading-cjk-off`, and in `design/screens/reader.html` with `textsize`, `font` and `cjk`. Every control
  is saved and applied as soon as it is chosen; there is no Save button. The page starts 32 below the toolbar; rows are padded 16 by 20, help
  sits 4 under its label and the control 12 under that; cards and section titles are 28 apart.

## Accessibility checklist

MyGo exposes native UI to VoiceOver, Narrator, NVDA and Orca; see
[Accessibility](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/accessibility.md).

- [ ] Every interactive element has an accessible name: its text, or `Label` when it shows only an icon, a symbol or
      a drawing.
- [ ] `Description` carries help text; `Error` marks invalid values.
- [ ] Custom elements that act as widgets set `Role`, and the states they show (`Checked`, `Expanded`, `Range`).
- [ ] Section and screen titles are headings with a `Level`.
- [ ] Changes that do not move the focus (a count, new entries arriving, a finished summary) call `c.Announce(text)`.
      Toasts announce their text, and a router the title of a page shown while the focus stays outside it.
- [ ] The whole UI works with the keyboard: the focus order is the standard one and follows reading order, there is
      a visible focus ring (do not remove it), shortcuts exist for frequent actions, Escape closes overlays.
- [ ] Immersive reading announces "Immersive reading on" and "Immersive reading off", keeps the focus on the article, leaves with Z or Escape, and needs no pointer for any reader action.
- [ ] The lightbox is a dialog named for its entry; paging announces "Picture {n} of {total}", every button has a name, the focus order is bar, then Previous and Next, and Escape closes it with the focus back on its row.
- [ ] A row's screen-reader label reads source, title, time, reading time, unread or read, starred; the reading-time
      button's name states the current budget, and its menu items are radio items with the current one checked.
- [ ] Text uses theme sizes (`Rem`) and scales; layouts reflow at larger text and in the longer translation.
- [ ] Colors are theme roles; contrast holds in both appearances and with high contrast on.
- [ ] Nothing depends on color, sound or motion alone.
- [ ] Pointer targets are at least about 24×24 DIPs, and 32 or more for primary controls. A control drawn smaller (the 20 square clear and dismiss buttons, the 22 square add and AI card close buttons, the 20 and 22 high text buttons) keeps its drawn size and layout slot: `theme.Hit` wraps it in an invisible 24 by 24 target with a negative margin, so neighbors do not move and targets do not overlap.
- [ ] `ui.NewTester` tests click controls by their accessible names, so a missing name fails the test (see
      [Testing](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/testing.md)).

Contrast, scaling, keyboard use and screen readers are verified on real devices; a passing headless test does not
count as accessibility acceptance.
The [Inspector](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/inspector.md) (F12 in development builds) shows
the element tree, roles and accessible names of a running window, which helps find a missing name before that check.

## Motion

Oriel has almost no custom animation: state changes take effect immediately, except that state fills, text and link
color, and the row actions fade. See
[Transitions](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/transitions.md) and
[Drawing and animation](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/drawing.md) for what the toolkit offers.

- **Default is none.** Do not add `Transition`, `Animate` or page transitions without a mockup that shows the change
  they explain and the maintainer's agreement; record the exception here.
- **State fills fade.** The agreed exception: the hover, pressed and chosen fills of rows (navigation rows, entry rows,
  the server bar, settings groups), text color and the underline color of links fade over 140 ms, ease-out, colors
  only, through `theme.Fade`, so the pointer moving down a list leaves a soft trail instead of a flicker. A faded row
  needs a stable `Key`. Nothing moves or resizes; controls drawn by MyGo keep their own behavior.
- **Row actions fade.** The row actions fade in and out over 120 ms, ease-out, opacity only.
- **Short and interruptible.** Nothing is longer than 200 ms, only color and opacity change, and a new state starts
  from where the old one is. If another exception is agreed, use the built-ins with their defaults (element
  transitions are 200 ms, ease-out); avoid frame-by-frame custom animation.
- **Reduced motion.** `Animate`, page transitions and the moving parts of element transitions already jump to the
  target when the desktop asks for less motion (`c.Preferences().ReduceMotion`); color and opacity fades still play,
  since nothing moves. Any motion you compute yourself must read the preference.
  Spinners may keep turning: they show that work is going on.
- **Never animate to hide latency**, and do not loop decoration.
- The connect screen's only motion is its button's spinner icon while it connects. Apart from the state fills, the timeline has none: new
  entries do not move the list; a "N new" pill appears at the top instead, and choosing it reloads and scrolls to
  the top.

## Localization

- The UI is in English and Simplified Chinese only. Every user-visible string is in the embedded catalog `copy.json`
  next to the screen, keyed `en` and `zh-Hans`. Change both together; do not add other languages.
- `locale.Language` (in `internal/ui/locale/`) maps the system locale: every Chinese locale, Traditional included,
  reads Simplified Chinese; everything else reads English. Screens use it rather than their own mapping. The Interface
  language preference (System, 简体中文, English; Settings, Appearance) overrides it: System means the mapping above.
  Its choices are named in their own language and are not translated. A change rebuilds the screens at once.
- Every key has both translations, and each screen's test fails on a missing or untranslated entry. Accessible
  names are copy too: translate `Label` and `Description`.
- Feed content (titles, text, author names) is shown as it comes, in whatever language it is in; it is not copy.
  Translation of content is the on-demand AI feature, not localization.
- English labels: short imperatives ("Reset", "Add subscription"), sentence case everywhere, menu items, dialog
  titles and buttons included ("Keyboard shortcuts", "Back up…", "Replace data"); only names keep their capitals
  (Markdown, OPML, RSSHub). No trailing period on labels and a full stop on sentences. Chinese sentences use full-width punctuation. Do not concatenate sentence fragments, since
  word order differs; use full-sentence strings with placeholders.
- The ellipsis is the single character "…", never three dots. A label ends in it when choosing it asks for more
  before anything happens: a dialog, a file picker or a typed query ("Import OPML…", "Save as…", "Go to…",
  "Delete…"). A label that only goes somewhere or acts at once does not ("Open settings", "Try again"); "Settings…" in
  the app menu keeps the platform's own form. A running state ends in it too ("Checking…").
- Chinese terms are used the same way everywhere. A term below does not get a second word for the same thing.

  | English | Chinese | Note |
  |---|---|---|
  | subscription, feed | 订阅 | One word for both; the screens never say 源. |
  | entry | 条目 | In lists and the keys of the timeline ("下一条"); in the reader it is 篇 ("上一篇", "下一篇"). |
  | kind | 形态 | Not 类型, which is left to the technical sense. |
  | mark as read | 标为已读 | Sentences use 把 ("把上面的标为已读"), not 将. |
  | star | 收藏 | |
  | reading time | 阅读时间 | The row label is "{n} 分钟阅读"; the filter is "阅读时长" in its name ("阅读时长：10 分钟内") and "{n} 分钟内" in its menu. |
  | sign in, sign out | 登录, 退出登录 | A device is signed out, never "吊销". |
  | password | 口令 | |
  | server | 服务端 | Not 服务器, which names the machine. |
  | this computer | 本机 | |
  | device | 设备 | |
  | fetch | 抓取 | The schedule is spoken of as 检查 ("检查间隔", "检查过"); both stand until they are unified. |
  | summary, translation | 摘要, 译文 | The verbs are 摘要 and 翻译. |
  | fallback address | 备用地址 | The address that takes over when the first RSSHub address does not answer; the Chinese is never 备用实例 in the UI, and English never calls it "backup", which is the copy of the data. |
  | AI endpoint | AI 接口 | |
  | prompt, API key | prompt, API key | Kept in Latin; "key" alone is fine after the first mention. |
  | Bilibili | 哔哩哔哩 | Not "B 站". |

- Quotes around a placeholder that holds a name, a scope or a query: 「{name}」 in Chinese, “{name}” in English.
  The same pair is used for names of screens and actions ("编辑订阅").
- Spacing in Chinese strings: one half-width space between Chinese and Latin letters or digits (`Oriel 无法启动`,
  `AI 接口`), and the same around a placeholder that holds a number, a file name or user content
  (`{n} 条未读`, `{file} 超过 1 MB`). Exceptions: none beside full-width punctuation or 「」; none beside a placeholder
  that holds already localized Chinese (`{t}前检查过`, `{date}登录`, `回到{scope}`); none inside date layouts
  (`1月2日`). A duration phrase is a number, a space and a unit (`1 天`, `3 个月`), so what follows it
  sits right after the unit (`{age}前`, `{n} 分钟前`).
- Layouts must tolerate the longer of the two translations, and English text 30–50% longer than the first draft.
  Do not fix widths of text controls.
- Relative times are one set of strings for every screen, written by `locale.Ago` (`internal/ui/locale/`): "just now",
  "{n} min ago", "{n} h ago", "yesterday", "{n} days ago" (`{n} 分钟前`, `{n} 小时前`, `昨天`, `{n} 天前` in Chinese),
  and from a week on the date ("Oct 15", with the year when it is not this one: "Dec 31, 2024"). The strings are lower
  case; the caller capitalizes (`locale.Capital`) a time that stands alone, as in a timeline row, and leaves it as
  it is inside a line ("Checked 5 min ago", "last used yesterday"). A new screen uses `locale.Ago`, not strings of its
  own. Lengths of time that are not "ago" ("Auto · 6 h", "next in 3 h") are a separate matter and keep the short unit.
- Times of day follow the hour cycle the system reports (MyGo `Region().HourCycle`); when the system says nothing, the
  language decides: 12 hours in English ("3:04 PM", "Jan 2, 3:04 PM"), 24 hours in Chinese ("15:04"). In Chinese the
  12-hour form puts the half of the day first ("下午3:04"). The reader's date line and the timeline's "last updated"
  line use `locale.DateTime` and `locale.Clock`; no screen has its own time layout.
- Format dates, times, numbers and plurals for the language. Chinese has no plural forms; English needs separate
  strings for one and many where it matters. A count in a string is filled by `locale.Count`: `{n}` becomes the
  number and `{n:entry|entries}` becomes the first form when the number is 1 and the second otherwise
  (`Marked {n} {n:entry|entries} as read`). Write the plural form in the English string only, with the count it
  follows in the same string; a Chinese string just carries `{n}`. Reword a sentence whose verb changes with the
  count (`{n:entry is|entries are} not in Oriel`) or find another way to say it.

## Design acceptance checklist

Run before a UI pull request is merged, and on each target platform before a release:

- [ ] Every state in the mockup is reached in the app (for the connect screen: each state of the mockup's switcher).
- [ ] Both languages, light and dark, the default and the minimum window size checked.
- [ ] Keyboard-only run-through, and a screen reader pass on each target platform.
- [ ] High contrast and larger text size checked.
- [ ] Screenshots of each state, light and dark, are in the pull request body.

## Polish checklist

A reviewer's pass over any screen, after the acceptance checklist:

- [ ] Fewer borders: separation comes from space, a fill or a hairline between rows; no outline around a card, a
      row or a toolbar control.
- [ ] Neutrals are the scale (`Background`, `Surface`, `Track`, `Text2`): warm in light, graphite in dark, none a pure gray, black or white.
- [ ] One accent: the unread dot, links, the chosen icon and the primary action; kinds are not colored, and
      status colors appear only with text or a symbol.
- [ ] Radii are from the fixed scale (4, 8, 12, 14, 16); nothing is rounded by eye.
- [ ] No more than five type sizes on the screen, weights 400 to 600, hierarchy by lightness before weight.
- [ ] Counts, times and durations use tabular numerals.
- [ ] Small labels are tracked; large titles are tightened.
- [ ] Line height follows the script: Simplified Chinese runs taller than English at the same size.
- [ ] Motion is short (140 ms fills, 120 ms row actions, never above 200 ms), color and opacity only, and
      interruptible.
- [ ] Hairlines separate rows only, and drop away from a hovered or chosen row.
- [ ] Icons share one stroke weight (1.6) and sit on the text's optical center.
- [ ] Every control shows a focus ring when reached with the keyboard.
- [ ] The empty, loading and error states are designed, not left as a blank or a raw message.
- [ ] Hover, pressed and chosen are lightness steps of the same ink fill, in that order, never a new hue.
- [ ] Chinese and Latin text sit together cleanly: no stray spaces next to full-width punctuation, full-width
      punctuation in Chinese sentences, and Latin runs in a Chinese line keep the same size and color.

## References

This design language draws on the UI documentation of MyGo (MIT License, Copyright (c) egoist):
<https://github.com/egoist/mygo/tree/v0.5.5/docs/ui>, together with `docs/native.md`, `docs/menus.md` and
`docs/windows.md` from the same version. The text here is a summary in our own words; the upstream pages are the
source for APIs and behavior. See also [THIRD_PARTY.md](../THIRD_PARTY.md).
