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
5. [Icons and images](#icons-and-images)
6. [Component guide](#component-guide)
7. [Windows, menus and navigation](#windows-menus-and-navigation)
8. [Accessibility checklist](#accessibility-checklist)
9. [Motion](#motion)
10. [Localization](#localization)
11. [Design acceptance checklist](#design-acceptance-checklist)
12. [Polish checklist](#polish-checklist)
13. [References](#references)

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
  equally.
- **The timeline.** An ink pane and the main column. The pane is 216 wide (184 below a 760 wide window), an opaque
  pane of ink (warm charcoal in light, deep teal in dark) floating 8 from the window's top, bottom and left edges, radius 14, with no divider; it is drawn with its
  own light-on-dark theme (see [Glass and ink](#windows-menus-and-navigation)); its first 40 are left to the window
  buttons. Then the five kinds (All, Articles, Videos, Social, Images) as 30 high rows, each an icon, its name and its
  unread count; then the subscriptions of the chosen kind, 30 high rows; at the bottom the server bar, 40 high. The main
  column is the paper: `Background`. It has a 52 high toolbar row (title, count, one capsule with the
  Everything/Unread/Starred segments, search, mark all as read, whose name and tooltip say its scope: "Mark all in
  “Videos” as read") with no bar and no divider: the segments are one 32 high capsule of glass floating over the
  entries, which fade under the row (the soft scroll edge); search is a flat ink fill, and the other buttons are
  bare icons. When a background check fails after the list loaded, a quiet 28 high status line sits under the toolbar,
  lined up with the list: a 14 `Warning` wifi-off icon and 12 `TextMuted` text, "Can't reach the server · last updated
  10:42", with no fill; it is a polite status, announced once, and goes away on the next successful check. With no subscriptions the list is an empty state: a 48 tile, a
  15 semibold heading, a hint at most 320 wide that names the links Oriel recognises, then a row of two buttons 8 apart,
  wrapping when narrow: the primary "Add your first subscription" and the secondary "Import OPML…", which opens
  Settings on Import and export. Article, social and mixed rows sit in one column at most 680 wide (720 for Social), centered, padded 24
  at the sides; a row is as high as its text, padded 16 with 14 between its parts, radius 12 when it has a fill, and
  an article's thumbnail, at most 96 by 64, radius 8, starts under the meta line and never makes the row taller. A
  row reads, top to bottom: the meta line (20 high: the site icon, the source name, a dot, the time, the kind
  mark; the row actions at its right), the title, the summary or the text, the pictures. Rows are separated by a
  hairline only, which hover and selection hide. The toolbar's title and buttons line up with that column's edges in
  a wide window (Videos and Images, which fill the width, keep the 20 inset). Pictures in a social post share a grid at
  most 440 wide (520 in Social): one picture keeps its aspect ratio, at most 384 wide and 240 high; two sit side by
  side at 4:3; three or more show the first three as squares in one row, the third covered by "+N" for the rest
  (labelled "N more pictures"), so a post is never taller than one row of pictures. Videos are a grid of cards at least
  200 wide, 16:9 covers; Images are a masonry wall of up to three columns at least 170 wide, each picture at its own
  aspect ratio. Below a 760 wide window the segments show icons only. Sizes taken from the mockup are written
  `px(t, n)`, which is `t.Rem(n / 13)`, so they follow the text size. The pane's section header, 22 high, ends in a 22
  square + button that adds a subscription; it shows even when there are none. A paused subscription has its name in
  `TextMuted`, a pause mark instead of the failure mark, and no count. With one subscription chosen, a pencil button
  after the count edits it. A row is chosen with a fill (ink 6.5%) and the selection marker; see
  [States](#color-and-dark-mode).
- **The subscription dialogs.** Dialogs over the timeline on a `RGBA(28,24,18,.32)` backdrop: 440 wide to add, 480 to
  edit, radius 16, `theme.RaisedFill` and a soft shadow, at most the window's height less `Space(8)`. A header (a 32 square
  tile or monogram, the title 15 semibold, a 28 square bare close button), a form that scrolls when the window is too low while the header and the buttons stay (a `Border` line appears under
  the header once the form is scrolled and above the buttons while more is below),
  with `Space(3.5)` between fields and 20 to the sides, and a footer with the buttons at the right; in the edit
  dialog "Delete…" sits alone at the left, in `Danger` without a fill. Fields are 32 high; title and interval share a
  row. Under the address, one line at a time: the error, the help, the RSSHub instance card (`Warning` soft fill) or what
  the address was recognized as (`Accent` soft fill, the address in monospace). The edit dialog starts with a health line
  (`Surface`, or the `Warning` soft fill while fetches fail) with a small Refresh button, Try again while fetches fail. It ends with "Summaries and
  translations", a 12 medium muted group title with the sparkles icon: two switches that queue new entries, and two
  monospace prompt boxes, three lines that grow to eight, whose placeholder says the empty box uses the prompt in
  Settings. While AI is not set up a line on `Surface` in `TextMuted` says so, with a small "Open Settings…".
- **The reader.** It takes the main column's place; the pane stays. A 52 high toolbar row over the article, as in the
  timeline: "Back to {scope}" (a chevron and the scope's name) at the left as a bare button; at the right
  [previous, next] and [star (`Star` when starred), read toggle, open in browser] as bare icon buttons, and between
  them the one glass capsule, [Summarize (sparkles), Translate (languages)], carrying their names beside the icon (12
  medium, icon only below a 760 wide window). The read toggle is an envelope: closed (mail) to mark unread, open
  (mail-open) to mark read, the same icons as the timeline's row action. Summarize and Translate are toggles: pressed,
  they sit on `Soft(Accent)` in `Accent` and their names become "Hide the summary" and "Show the original". In a wide
  window the toolbar lines up with the article column: the back button's text starts at the column's left edge and
  the last button ends at its right edge. Under the toolbar the article scrolls, a column at most 680 wide, centered,
  padded 28/32/48 (20 below a 760 wide window): the source's monogram, name and full date at 12; the title at 24 (20
  when narrow) weight 600 in `Text`; the note on where the text comes from (`Warning` soft fill when it is the feed's,
  `Surface` with a spinner while the full text is being got), 12, its "Open the original" link underlined; then the
  text 22 below. The body is set in the interface font (tokens.md) at 16 and line height 1.7 (1.85 in
  Chinese) by default, in `theme.Prose`; the Reading text size and Line spacing preferences change it, and h2, h3, the
  gaps between paragraphs and list items and code scale in step with the size. h2 and h3 use the interface font,
  19 and 16 semibold, 1.9em and 1.3em above, 0.6em below; 1.05em between paragraphs, 0.3em between list items, a 22
  wide marker column; quotes behind a 2 DIP ink 16% line in `Text2`; code on `Surface` at 13 monospace, radius 12,
  scrolling sideways, inline code on an ink 5.5% fill, radius 4; links in `Accent` with a 1 DIP underline 3 below in
  `Accent` at 38%; pictures at their own aspect ratio (16:9 until known), radius 12 with a 0.5 DIP ink 10% outline,
  at most their own width, captions 12 in `TextMuted`. Loading is a skeleton of `Surface` bars; the empty states
  (getting the text, no text, can't load) are a 48 tile, a 15 semibold heading, a hint at most 320 wide and a button.
  The whole text is one selection.
- **Summary and translation in the reader.** The summary is a card under the title, radius 14, padded 14/16/16, text
  13 at line height 1.65 (1.75 in Chinese), its head 12 medium: while it is made, `Soft(Accent)` with a spinner,
  "Summarizing…", a Cancel text button and three skeleton lines; done, `Soft(Accent)` with "Summary · by {model}", the
  head in `Accent`, the model in `TextMuted`, and a close button; failed, the `Danger` soft fill with what went wrong
  and a small "Try again"; while AI is not set up, `Surface` with "AI isn't set up", one line on where to set it and a
  small primary "Open Settings…". The translation replaces the title and text in place; the note under the title says
  what is shown. While it is made, the original stays with a note (`Surface`, a spinner, Cancel); done, the note is
  `Soft(Accent)`, "Translated by {model}" and a segmented Translation | Original switch, and the translated text is
  plain paragraphs, pictures and links staying in the original; failed, the original stays with the note in the
  `Danger` soft fill, the reason and "Try again". Cancel only stops waiting; leaving the article does the same.

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
  | `Selection`, `Focus` | selected text; the keyboard focus ring (`Accent` at 45%, 2 DIPs) |
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
  (tokens.md has the numbers); `Warning` and `Star` are graphics, never text.
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
- **Hierarchy by lightness.** Within one size, the order is `Text`, `Text2`, `TextMuted`; weight comes second and size
  last. Read rows go quiet in lightness and weight (500), not in size.
- **Tracking and numerals.** Small labels get letter spacing (+0.04em at 11, +0.02em in Chinese at 12); the title
  tightens (-0.015em); everything else is 0 or within 0.01em. Counts, times, durations and any number that changes in
  place use `FontFeatures("tnum")`.
- **Chinese runs taller.** Simplified Chinese gets a larger line height at the same size: body 1.7 instead of 1.5,
  row titles 1.5 instead of 1.35, the title 1.42 instead of 1.28, the reading body 1.85 instead of 1.7; Chinese
  tracking is 0 except small labels. The reading text size and line spacing preferences change the reader's body
  text only, by the steps in [design/tokens.md](../design/tokens.md).
- **The reading face.** The article's body is set in the interface font, as clean as the rest of the screen: Song and
  other thin serifs look faint and fuzzy at screen sizes and tire the eye over a long read. #101 will add a choice of
  reading font, serif included; the default stays the interface font.
- **Headings are real headings.** Give the title of a screen or section `Role(ui.RoleHeading)` and a `Level` so
  assistive technology can navigate by them.
- **Do not truncate silently.** Prefer wrapping. Where a single line is needed (`SingleLine`, `MaxLines`), for
  example an entry title in the timeline, make the full text available in a tooltip or in the reader.
- **Numbers and dates.** Show counts and dates in the user's language when they are data, not identifiers.

## Icons and images

- **Icons are SVG** shown with `ui.Icon`, which takes the text color and the font size: an icon follows theme colors
  and the text size automatically. Every icon has the same stroke weight, 1.6, at every size. See [Icon](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/icon.md).
- **One set: [Lucide](https://lucide.dev)** (ISC License), recorded in `THIRD_PARTY.md`. The files of lucide-static
  are copied unchanged into `internal/ui/icons/lucide/` and parsed once in `internal/ui/icons`; add an icon there
  before using it. Prefer an icon to a word where the icon is clearer, and keep the word where it is not.
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
| `ui.SelectBase`, `ui.Switch`, `ui.TextArea` | A subscription's fetch interval, its pause switch and its mute keywords. |
| `Element.ContextMenu` | Right click on a subscription: Refresh (Try again on a failing one), Mark subscription as read, Edit…, Pause or Resume, a hairline, Delete…. Right click on an entry: Star, Mark as read or unread and Open in browser, as on the row, a hairline, Mark above as read. The menu is the platform's own, so its items are text only, with no icons or key hints; an item that has nothing to do (no unread above the entry, none in the subscription) is dimmed and does nothing. |
| `ui.List`, `ui.GridView` | The timeline's rows and its video cards; both build only what is in view. |
| `ui.AlertDialog` | Confirming deleting a subscription, which cannot be undone. Its last button, the action, is the default and takes the focus; Escape is Cancel. |
| `c.Toast`, `c.Announce` | Feedback after starring, marking and failures, and "No unread entries below"; counts announced without moving the focus. |
| `c.ToastAction` | "Mark all as read", "Mark above as read", "Mark subscription as read" and the V key act at once and offer Undo here, with a close button, one Undo shared by the four; Cmd/Ctrl+Z undoes it while the toast shows. The toast is centered at the bottom as an `Inverse` tile with `InverseText`. |
| `ui.DialogBase` (key sheet) | The keyboard shortcut sheet, drawn like the subscription dialogs: two columns (timeline, reader) and an "Anywhere" group below, each key a small chip; one column below a 760 wide window. Escape or Close dismisses it. |

The tables below say what to reach for as screens are added.

### Actions

| Need | Use |
|---|---|
| The main action of a group, once | [Button](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/button.md) (`PrimaryButton`) |
| Other actions | [Button](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/button.md) |
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
- Do not stack dialogs. Use a toast for news that needs no answer.
- Errors say what happened and what to do, next to the control that caused them (`Error`, or a
  [form](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/form.md) field). The server returns stable error codes;
  the UI maps each to localized copy and never shows the raw code alone.
- AI summaries and translations are on demand and can be slow or fail: show that the work is going on, keep the
  original text readable meanwhile, and say plainly when it failed.

## Windows, menus and navigation

- **Windows.** One main window; set `Title`, `Width`, `Height`, and `MinWidth`/`MinHeight` so content never clips.
  Oriel's window is 960×640 with a minimum of 640×480. `StateKey` remembers size and position between runs and is not used yet. A second launch brings the existing
  window to the front. See
  [Windows with native UI](https://github.com/egoist/mygo/blob/v0.5.5/docs/ui/windows.md) and the
  [window options](https://github.com/egoist/mygo/blob/v0.5.5/docs/windows.md).
- **Title bar.** No title bar: `TitleBarStyle: mygo.TitleBarHidden`, the macOS window buttons placed with
  `TrafficLightPosition` so they sit over the sidebar's top, centered in the 52 high toolbar row. The view then owns
  the room the window controls take (`c.TitleBar()`: on Windows the caption buttons at the right, which the toolbar
  leaves free) and the draggable area: the toolbar row's empty space and the sidebar's top call `DragWindow`. The
  window's name stays set (`Title`) for the Window menu, the Dock and screen readers.
- **Glass and ink.** Oriel draws its toolbar controls in glass (the [glass
  plugin](https://github.com/egoist/mygo/blob/v0.5.5/docs/plugins/glass.md)) and its sidebar as an ink pane. The pane
  is not glass: it is a plain floating view with an opaque `Background` of ink (warm charcoal `#37332e` in light, deep teal `#1a2422`
  in dark; at 92 to 96% opacity glass added only softness and a muddy mix with the backdrop), radius 14, a 0.5 DIP
  inner rim in the pane ink, in light-on-dark roles of its own (tokens.md): the root view takes them from
  `theme.PaneTheme`, calls `c.SetTheme(paneTheme)` before building the sidebar and sets the main theme back after. The
  toolbar has one glass capsule (a 32 high pill, tint rgba(255, 254, 251, 0.66) in light and rgba(58, 63, 62, 0.52) in
  dark, radius 16, 0.5 DIP rim): the segmented control in the timeline, Summarize and Translate in the reader.
  `glass.ScrollEdge{}` fades content under the toolbar row into `Background` (88% in light, 90% in dark, 72 high).
  Content stays on opaque `Background`; glass is for chrome only, never for entries, the article, dialogs, search or
  bare toolbar icons. Text on glass keeps AA contrast, and with high contrast glass becomes opaque (the capsule
  `Surface`, the pane is already opaque) with a `Border` rim (MyGo reports no Reduce Transparency setting, so high
  contrast is the one switch). The plugin draws the same glass on Windows and Linux, so the look is one on every
  platform; no system vibrancy is used.
- **Menus.** The macOS menu bar belongs to the app. Oriel's menu has an app menu (About, check for updates, Quit),
  the Edit menu (`RoleEditMenu`, so copy and paste work) and the Window menu (`RoleWindowMenu`); "Connect to
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
  One glass capsule at most per toolbar; everything else is a bare icon or the flat search field.
- **Notifications.** Use system notifications only for events the user would want when the app is in the background.
- **Timeline keys.** J or Down and K or Up move between entries, N goes to the next unread entry below the chosen one
  (loading the next page when needed, with a toast when there is none), O or Enter opens the chosen one (an article in the
  reader, anything else in the browser) and marks it read, S stars it, M toggles read, V marks the entries fully in view as
  read and scrolls the first one below them to the top (see "Mark visible" below), `/` and Cmd/Ctrl+F go to search, Escape clears it. The list keys are
  window-wide but stop while the search field has the focus or a dialog is open, so typing never moves the list.
  Cmd/Ctrl+N adds a subscription, Cmd/Ctrl+Z undoes the last of the four "mark as read" actions while its toast shows, and `?` or Cmd/Ctrl+/
  opens the key sheet, which the Help menu also lists as "Keyboard Shortcuts".
  Every entry is also a focusable element with its own name, so Tab reaches it.
- **Mark visible (V).** One key clears a screenful: it marks the entries fully inside the list area as read, then
  scrolls so the first entry below them is at the top, chosen and focused. "Fully inside" counts a row (a card in the video
  grid, a tile in the picture wall) that lies wholly between the bottom of the toolbar, which the list scrolls under, and
  the bottom of the window; a row cut by either edge is not marked and, at the bottom, is the one scrolled to the top. The
  range runs in list order from the first entry fully in view to the last one before the first entry that is not; in the
  picture wall, whose columns differ in height, that is the entries up to the first tile not wholly in view. Entries already
  read inside the range stay as they are and are not counted. With no unread in the range V only scrolls and shows no
  toast, so it also pages; at the end of the list it marks and stays. If the last loaded entry is wholly in view but
  another page is to come, it is left unread and becomes the one scrolled to, while the page loads. The scroll is
  immediate (MyGo's `ListState.ScrollTo` and `GridState.ScrollTo` do not glide) and not undone by Undo. V works in All and
  Unread without a search, like "Mark all as read", and does nothing under Starred or a search. The toast is the shared
  one: "Marked 5 visible entries as read" with Undo and the close button (`marked-visible` in the mockup). The toast is
  announced, and the new chosen entry is then read by its own name; there is no extra announcement. The trackpad swipe is
  not possible in MyGo 0.5.5 (no gesture callback) and is left out.
- **Sidebar keys.** 1 to 5 switch to All and the four kinds, in the sidebar's order, from anywhere in the timeline. H or
  Left moves the focus to the chosen item of the sidebar (the kind, or the subscription when the scope is one), Up and
  Down walk its kinds, which choose as the focus passes, and its subscriptions, which wait for Enter (one vertical
  focus group each, so Tab still goes through the sidebar in two stops, and Left and Right are not the groups' but go
  back to the list); the focus stays on the item chosen, and L, Right or Escape gives the focus back to the list, the chosen entry
  focused. While the sidebar has the focus by the keyboard (H, Left, Tab, the arrows) the entry keys (J K O S M N V,
  Up and Down) do not touch the entries; `/` still goes to search. A click on a kind or subscription does not count:
  the entry keys keep acting on the list. Shift+F10 or the Menu key opens the context menu of the focused subscription
  or entry, which MyGo does natively for an element with `ContextMenu`; a Mac keyboard has no Menu key, so the key
  sheet lists Shift+F10 alone there. All of these are off while the search field has the focus, a dialog is open or the reader shows. A
  scope switched from the keyboard is announced with `c.Announce` ("Videos, 12 unread"), the focus staying where it was.
  The focus ring on a sidebar item is 2 DIPs of the pane's own accent at 55% (`sidebar-focus` in the mockup).
- **Reader keys.** While the reader shows, J and K go to the next and previous article, N to the next unread one (a
  toast when there is none, staying on this article), S stars, M toggles read, O
  opens the original in the browser and Escape goes back to the list, the entry it opened from focused. The article
  takes the focus when it opens, so the arrows, Space, Page Up/Down and Home/End scroll it, and Tab goes through its
  links. Every toolbar button names its key in the tooltip.
- **Settings.** The settings take the whole window, in place of the timeline: their own sidebar has "Back to the
  timeline" on top, one item per group (Appearance, Fetching, Import and export, AI, Devices) and the server bar, which says whose
  settings these are and does not open the connect screen. They open from the gear beside the timeline's server bar
  or "Settings…" in the app menu; the back button and Escape return to the timeline as it was. The page's toolbar title lines up with the left edge
  of its content column. **Page width.** A page is one column centered in the room beside the sidebar, its width
  taken from the page and the room, not from the window: a form page (Appearance, Fetching, Import and export) keeps
  a column at most 600 wide, padding included, for a readable line; Devices is a list and widens to 880 once the room
  is 656 wide (a window of 880); AI splits in two columns, 1040 wide at most with a 24 gap, once the room is 976 wide
  (a window of 1200). Below those widths a page is the single 600 column, padded 32 (20 below a 760 wide window).
  Text outside a card (the lead line) never runs past 600. A setting is saved
  where it is changed: the RSSHub instance with its Save button or Enter, the retention as soon as another is
  chosen; the line under it says it was saved, or why not, and a refused value keeps the focus in its field. OPML
  files are chosen and saved with the system's file dialogs, read and sent in the background; the result stays
  under the button, with the subscriptions that could not be added and why, until dismissed. Signing a device out asks
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
  Test sends a short request with the saved settings, so it is disabled while there are unsaved edits; its result
  stays beside the buttons. Each prompt has "Restore default", disabled while it is the default. The Appearance group, first in the list, is
  different: it is kept on this device, not on the server, and a line at the top of its page says so ("These apply only
  to this device."), where the other pages say settings are kept on the server. One card holds a row each for Theme
  (segmented: System, Light, Dark), Interface language (a menu: System, 简体中文, English, each language name in its own
  language; chosen, it applies at once and the screens are rebuilt), Interface font (a menu of the installed families
  with "System font" first, each family name set in that family) and, in a second card titled "Reading", Text size
  (segmented: Small, Medium, Large, Extra large) and Line spacing (segmented: Compact, Standard, Relaxed). A sample paragraph on `Surface` under them shows the result. Every control
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
- [ ] Text uses theme sizes (`Rem`) and scales; layouts reflow at larger text and in the longer translation.
- [ ] Colors are theme roles; contrast holds in both appearances and with high contrast on.
- [ ] Nothing depends on color, sound or motion alone.
- [ ] Pointer targets are at least about 24×24 DIPs, and 32 or more for primary controls.
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
- English labels: short imperatives ("Reset", "Add subscription"), sentence case, no trailing period on labels and a
  full stop on sentences. Chinese sentences use full-width punctuation. Do not concatenate sentence fragments, since
  word order differs; use full-sentence strings with placeholders.
- Chinese terms are used the same way everywhere. A term below does not get a second word for the same thing.

  | English | Chinese | Note |
  |---|---|---|
  | subscription, feed | 订阅 | One word for both; the screens never say 源. |
  | entry | 条目 | In lists and the keys of the timeline ("下一条"); in the reader it is 篇 ("上一篇", "下一篇"). |
  | kind | 形态 | Not 类型, which is left to the technical sense. |
  | mark as read | 标为已读 | Sentences use 把 ("把上面的标为已读"), not 将. |
  | star | 收藏 | |
  | sign in, sign out | 登录, 退出登录 | A device is signed out, never "吊销". |
  | password | 口令 | |
  | server | 服务端 | Not 服务器, which names the machine. |
  | this computer | 本机 | |
  | device | 设备 | |
  | fetch | 抓取 | The schedule is spoken of as 检查 ("检查间隔", "检查过"); both stand until they are unified. |
  | summary, translation | 摘要, 译文 | The verbs are 摘要 and 翻译. |
  | AI endpoint | AI 接口 | |
  | prompt, API key | prompt, API key | Kept in Latin; "key" alone is fine after the first mention. |
  | Bilibili | 哔哩哔哩 | Not "B 站". |

- Quotes around a placeholder that holds a name, a scope or a query: 「{name}」 in Chinese, “{name}” in English.
  The same pair is used for names of screens and actions ("编辑订阅").
- Spacing in Chinese strings: one half-width space between Chinese and Latin letters or digits (`Oriel 无法启动`,
  `AI 接口`), and the same around a placeholder that holds a number, a file name or user content
  (`{n} 条未读`, `{file} 超过 1 MB`). Exceptions: none beside full-width punctuation or 「」; none beside a placeholder
  that holds already localized Chinese (`{t}前检查过`, `{date}登录`, `回到{scope}`); none inside date layouts
  (`1月2日`).
- Layouts must tolerate the longer of the two translations, and English text 30–50% longer than the first draft.
  Do not fix widths of text controls.
- Format dates, times, numbers and plurals for the language. Chinese has no plural forms; English needs separate
  strings for one and many where it matters.

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
