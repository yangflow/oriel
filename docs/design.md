# Oriel design language

This is the design language of Oriel: the rules every screen shares. How each screen looks, its layout, copy and
states, is drawn on the screens canvas (below), which is the authority on screens; this document is the authority on
the shared rules. Oriel is a single-user reader that mixes RSS and RSSHub subscriptions into one timeline of
articles, videos, social posts and images (see [product.md](product.md), in Chinese). The same program is the
desktop app and, with `oriel serve`, a headless server; only the desktop app has a UI, and that UI reads and writes
through the HTTP API.

The UI is built on the native toolkit of [MyGo](https://github.com/egoist/mygo). This document summarizes the parts
of its [UI guides](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/README.md) that matter for design; the guides
stay the authority on API details. Links point to MyGo v0.7.4, the version pinned in `go.mod`. When the pin moves,
check the pages again and update the links and this document together.

The screens canvas is the [Oriel 设计稿](https://claude.ai/artifact/M1YW6F7vwnQm2Ze8BJnYcb) Design artifact. Its
`4 页面` page holds one artboard per state of each screen, titled `4 页面：<screen> <state>`; its `5 桌面` page holds
the window on each platform, the menu bar menu, the app menu and the startup failure. Each screen is one package,
`internal/ui/<screen>/`, and [design/README.md](../design/README.md) maps the screens to their artboards and to the
code. "Drawn in the `timeline` page, `lightbox`" below names a page and an artboard there.

Related files: [design/README.md](../design/README.md) (the design artifacts and how a UI change is made),
[design/tokens.json](../design/tokens.json) and [design/layout.json](../design/layout.json) (every design value and
layout pattern, the one source), [design/tokens.md](../design/tokens.md) (why the values are what they are) and the
[Oriel design system](https://claude.ai/artifact/LBUfZ9m6MHwCsXp4D1d97w) Design artifact (the design system drawn).

## Contents

1. [The design system](#the-design-system)
2. [Principles](#principles)
3. [Layout and spacing](#layout-and-spacing)
4. [Color and dark mode](#color-and-dark-mode)
5. [Typography](#typography)
6. [Chinese typesetting](#chinese-typesetting)
7. [Icons and images](#icons-and-images)
8. [Component guide](#component-guide)
9. [Windows, menus and navigation](#windows-menus-and-navigation): window, menus, navigation and toolbars, keyboard
   and focus, screen readers, platforms
10. [Accessibility checklist](#accessibility-checklist)
11. [Motion](#motion)
12. [Localization](#localization)
13. [Design acceptance checklist](#design-acceptance-checklist)
14. [Polish checklist](#polish-checklist)
15. [References](#references)

## The design system

Oriel's values form one system, kept in `design/tokens.json` and `design/layout.json`:

- **One source.** Colors, alphas, space, sizes, radii, text styles, the reading scale, shadows and motion are named
  tokens in `tokens.json`; layout patterns (bar, column, stack, inset, entry row…) and the geometry of each part are in
  `layout.json`. `tokens.json` is generated into the Go package `internal/ui/theme/tokens`, values only; package
  `theme` decides how they are used, scales DIPs with the text size and applies high contrast.
- **Color in layers.** Every color names its layer: region (paper, pane), surface (surface, float, raised), state (fill,
  hover, selected, pressed), role (accent and its steps, on-accent, soft, selection, focus, inverse, badge, viewer),
  text (fg, prose, fg2, muted, faint), meaning (star, danger, success, warning) and line (line, line2, rule,
  scrollbar, shadow, scrim). The ink pane and the reading presets replace a region and the roles drawn on it, nothing
  else.
- **Contrast by target.** Text tiers are set by contrast targets on their worst bed, not by eye: fg 10:1, prose 8.5,
  fg2 7, muted 4.5, faint 2.6; accent, danger and success 4.5; star and warning 3 (graphics). The pane keeps
  ivory text at fg 8, fg2 6, muted 4.5, with its accent at 3 and faint at 2.6. The beds and targets are the audit in
  `tokens.json`; `TestContrastAudit` in `internal/ui/theme/tokens` computes it in every scheme, the pane and every
  preset, with alphas as MyGo stores them (a byte), and a value that misses cannot merge. The helpers of package
  `theme` are checked by `TestContrast` and `TestReadingPresetContrast`.
- **State ladder.** fill < hover < selected < pressed, four alphas of the scheme's ink, so a hovered chosen row still
  reads as chosen and pressing it still shows.
- **MyGo roles.** A token marked `mygo` sets that role of `ui.Theme` (fill is `Surface`, line is `Border`, space-1 is
  `Spacing`, radius-control is `Radius`), so MyGo's own fields and buttons draw with the system; every other token
  is read by name.
- **Type and opacity are tokens too.** Three families (sans, the platform's UI font or the chosen Interface font;
  serif, for the reading body only; mono, for code and addresses), ten interface text styles and the reading scale.
  Opacity comes only from `opacity-faded` and `opacity-separator`; a disabled glyph takes `faint` instead of fading.
- **Patterns and parts first.** A screen is assembled from the patterns and parts of `layout.json`; when one value
  needs changing, the view uses MyGo's own method with a token name (`Padding`, `Gap`, `Height`, `Width`, `MaxWidth`).
  A value no token has is added to the JSON first, never written in a view.
- **Tokens win.** Where a hand-written value in `internal/ui/theme` or an artboard differs from `tokens.json`,
  `tokens.json` wins.

## Principles

- **Native first.** Use MyGo's native widgets and the theme defaults as they are, and follow the system's light and
  dark appearance (unless the user chose one in Settings, Appearance) and its text size. A control that looks and
  behaves like the platform's is learned already. Style an element yourself only when no widget does the job, and
  build it on the [custom widget](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/custom-widgets.md) bases so
  focus, keyboard and accessibility stay intact. No WebView and no HTML frontend.
- **Consistent across platforms, respectful of each.** One view serves macOS, Windows and Linux. Follow each
  platform's conventions where MyGo exposes them: menu roles, the accent color, the system font, shortcuts that use
  `CmdOrCtrl`. Do not add chrome that imitates one platform on the others.
- **Content first.** Oriel is for reading. The timeline and the reader give the room to titles, text and images;
  controls stay quiet and secondary until they are needed. An ink pane for the sidebar, paper for the content, one
  accent, and fewer lines, fills and sizes than the toolkit's defaults.
- **Accessible by default.** Every element can be reached with the keyboard, named for a screen reader, read in both
  appearances, and still works at larger text sizes. See the [checklist](#accessibility-checklist).
- **Almost no custom motion.** State changes take effect at once; the few fades change color and opacity only. See
  [Motion](#motion).
- **Meaning is never carried by color alone.** Say it in words; pair color with text, a symbol or a shape.
- **The UI thread draws.** A frame is built from in-memory state. No disk, network or other blocking work in a view,
  including calls to Oriel's own HTTP API and decoding images; start that work in the background and change the state
  back on the UI thread (see [Views](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/views.md)).
- **Spec before code.** Every screen is drawn on the screens canvas before it is written, and follows the shared rules
  of this document. Only `design/`, the two design artifacts it lists and this document count as the design reference
  for Oriel.

## Layout and spacing

Layout uses flexbox in device-independent pixels (DIPs); see
[Layout](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/layout.md).

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
  [scroll view](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/scroll.md).
- **Sizes follow the text size.** A size taken from a mockup is written `theme.Px(t, n)`, which is `t.Rem(n / 13)`, so
  it grows and shrinks with the text size.
- **Narrow hides, never removes.** A control that a narrow window or column hides stays reachable by its key, the
  command palette or a menu.
- **Late content moves nothing it need not.** Room for a picture is held before it arrives, at the declared ratio or a
  fixed height when none is declared, so the text under it is laid out once and a late picture moves nothing. When a
  decoded picture changes the height of a box that lies wholly above the top of the viewport, the scroll offset moves
  by the same amount, so the text being read stays where it is; selection and focus stay too.
- **Loading.** A list's loading state shows only after 300 ms; an answer that comes sooner shows none, so switching
  never flashes.

## Color and dark mode

Colors come from the theme, `c.Theme()`. Oriel puts its own palette, paper and ink (warm in light, neutral graphite in
dark), on the theme roles: the root view calls `theme.Apply` (in `internal/ui/theme/`) first in every frame, which
picks the light or dark palette for the Theme preference (the system appearance by default) and keeps the theme's
metrics, so the desktop's text size still applies. The values are in [design/tokens.json](../design/tokens.json)
(reasons in [design/tokens.md](../design/tokens.md)). See
[Styling and themes](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/styling.md).

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
  or Dark, applied by `theme.Apply` alone; screens do not make their own. Teal stays the accent in both. If a screen
  draws custom elements, use only theme colors, or read `c.Theme().Dark` where a choice cannot be expressed with them.
  For derived colors use `theme.Ink` and `Alpha` on theme colors rather than new constants.
- **Accent.** Oriel's accent is a muted teal, in both appearances; it does not follow the system accent. It is the
  only hue besides status and the star: kinds of entries are not colored. One primary action per group gets the accent.
- **Paper and ink.** Oriel is read for long stretches, so the palette is paper, not a screen: warm off-white in light
  and neutral graphite in dark instead of white and black, body text near 15:1 in titles and 11:1 in prose instead of
  16:1, a low-chroma neutral scale (warm in light; in dark a hint of cool, no brown, so the pane and the accent are the
  only color) and fills that are ink at a low alpha, so the same fill works on `Background`, `Surface` and a
  photograph. The surfaces step evenly: a hover, a press and a selection are three equal steps of lightness, the same
  everywhere.
- **The ink pane.** The sidebar is the one dark surface in the light scheme: an opaque pane, per scheme. In light it is
  warm charcoal, which sits with the warm paper; in dark it is deep teal ink, which separates from the neutral graphite
  content by hue, so the pane and the accent are the only color in dark mode. It is not glass. It has its own
  light-on-dark roles. It is drawn with `c.SetTheme(theme.PaneTheme(main))` before the pane's elements are built and
  the main theme set back after, so its widgets, helpers and `Chosen` read the pane's values. Its roles, fill and
  contrast are in tokens.md; with high contrast it gains a `Border` rim.
- **Contrast.** Text and meaningful graphics should meet WCAG 2.1 AA: 4.5:1 for body text, 3:1 for large text and
  control boundaries. With the desktop's high contrast setting on (`c.Preferences().HighContrast`), `theme.Apply`
  makes `Border` a `Text` line (55%), moves `TextMuted` toward `Text`, makes `Focus` opaque, raises the state fills to
  8%, 12% and 16%, and turns glass (the capsules, the scroll edge) opaque; test with it on. Text on the ink pane holds
  AA (tokens.md has the numbers); `Warning` and `Star` are graphics, never text. `TextMuted` does not hold 4.5:1 on a
  `Track` or a soft fill: the unselected labels of a segmented control and the badges on a `Track` are `Text2`, and the
  text in a `Danger` result box is `Text`, with the icon in `Danger`.
- **States.** One look per state, the same on every screen, as steps of lightness: ink fills of 3%, 5% and 6.5% in
  light (4%, 6% and 8% in dark; 5.5%, 8.5% and 12% on the pane) for hovered, pressed and chosen, so
  hover < pressed < chosen. Hovered: `SurfaceHover`. Pressed: `SurfacePressed`. Chosen in navigation (a kind, a
  subscription, a settings group) and a chosen row: `theme.Chosen`, with the icon in `Accent` in navigation. There is
  no border on a chosen row, and the accent never tints a fill on warm neutrals, where it turns muddy. On a chosen or
  hovered row, `TextMuted` text steps to `Text2` to keep its contrast. Chosen in a control (a segment, the kind of a
  subscription, original or translation): in the glass capsule an ink 10% fill; on a `Track` trough, a
  `theme.RaisedFill` face with a hairline shadow. The keyboard's place: the `Focus` ring, 2 DIPs, always visible.
  Disabled: `Surface` with a `TextMuted` label, the primary button too, never a faded accent, so its label stays
  readable (at least 3:1); a disabled glyph may use `theme.Disabled`. Fields and text areas share one fill, `Surface`,
  and `theme.RaisedFill` with the `Focus` ring when focused.
- **Status.** Color supplements a message; the message states the status in words. A read entry, a failing
  subscription or a missing full text is said in text, not only shown as a dimmed or red row.
- **Reading appearance.** The reader's content area can wear a preset instead of the theme's paper: Follow theme
  (default), Sepia (warm cream, `#f4ecd8`, with dark brown ink) or Mist (cool gray-blue, `#e8edf0`, with slate ink), a
  device preference in Settings, Appearance, Reading. A preset keeps the scheme: Sepia and Mist each have a light and a
  dark variant, picked by the app's Theme, so choosing Sepia never turns a dark window light; and it paints only the
  reader's page, toolbar bar, text, links, code, notes and the AI card, never the ink pane, the timeline or an overlay
  (the selection card, the toasts and the system menus, such as More, keep the theme; the find bar is part of the page
  and is on paper). The accent stays a muted teal, darker or lighter to hold 4.5:1 on the preset's paper. Every text
  pair of every preset holds 4.5:1 and every graphic 3:1, with and without high contrast (the table is in tokens.md,
  Reading appearance presets, and `Warning` and `Star` stay graphics); `TextMuted` is tuned per preset so that it holds
  4.5:1 on its `Surface`. The presets are paper and ink only: no pattern, no texture, no gradient and no new accent
  hue. Drawn in the `reader` page, `preset`, and the `settings` page, `reading-appearance`.
- **Content colors.** Images and video thumbnails from feeds keep their own colors; everything Oriel draws around
  them uses theme roles.
- **Mockups.** The screens canvas uses the Oriel palette of `design/tokens.json`; the app running on a real device is
  what counts.

## Typography

- **System font.** Leave `Font` empty so text uses the platform's UI font. The Interface font preference (Settings,
  Appearance) sets the theme's `Font` to an installed family, chosen from `ui.FontFamilies()`; screens never set a
  family of their own, except monospace for code and addresses. The default text size is 13 on macOS and 14
  elsewhere, and scales with the desktop's text size (`Preferences().TextScale`) when you rely on the theme; it is not
  a setting. See [Text](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/text.md).
- **Size relative to the theme.** Use `t.Rem(n)` for sizes (`n` times the theme font size) rather than fixed numbers,
  so custom text follows the user's text scale. Widgets use the theme size.
- **Five sizes.** 24 (the title), 15 (row title, section head, screen title), 13 (body and widget text), 12 (meta,
  labels, segments) and 11 (small labels: counts, section labels in the pane, durations; 12 in Simplified Chinese).
  No other size is drawn on the screen. The exceptions, each with its reason, are listed in
  [design/tokens.md](../design/tokens.md) (Typography, Exceptions); a new size is a new row there first.
  Weights 400, 500 and 600 only. The reading body is the article's own scale, not interface type. The table of
  sizes, line heights and tracking is in [design/tokens.md](../design/tokens.md).
- **Hierarchy by lightness.** Within one size, the order is `Text`, `Text2`, `TextMuted`; weight comes second and size
  last. Read rows go quiet in lightness and weight (500), not in size.
- **Tracking and numerals.** Small labels get letter spacing (+0.04em at 11, +0.02em in Chinese at 12); the title
  tightens (-0.015em); everything else is 0 or within 0.01em. Counts, times, durations and any number that changes in
  place use `FontFeatures("tnum")`.
- **Chinese runs taller.** Simplified Chinese gets a larger line height at the same size: body 1.7 instead of 1.5,
  row titles 1.5 instead of 1.35, the title 1.42 instead of 1.28, the reading body 1.85 instead of 1.7. The reader
  tells by the language stored for the entry (Chinese and Japanese run taller) and by the characters of the title and
  body only when none is stored; Chinese tracking is 0 except small labels. The reading font, text size and line
  spacing preferences change the reader's body text only, by the steps in [design/tokens.md](../design/tokens.md).
  Chinese text in the reader is also set by the rules of [Chinese typesetting](#chinese-typesetting).
- **Long font lists.** A font menu with hundreds of families is the font picker ([Component guide](#component-guide)),
  never a plain menu.
- **The reading face.** The article's body is set in the interface font by default, as clean as the rest of the screen:
  Song and other thin serifs look faint and fuzzy at screen sizes and tire the eye over a long read, so the default
  stays the interface font. The Reading font preference (Settings, Appearance) lets the reader choose Serif or an
  installed family for the body only; the title, notes, h2, h3, captions and the toolbar keep the interface font. Serif
  is a platform set, New York with Songti SC on macOS, Georgia with SimSun on Windows, Noto Serif with Noto Serif CJK SC
  on Linux, and the interface font when none is installed (tokens.md, Reading text).
- **The reading column follows the text size, and the reader can change its width.** The column is 680 wide at Medium
  and grows and shrinks with the text size (635 / 680 / 771 / 861), so a larger size does not cut the characters per
  line. The Line width preference (Narrow, Standard, Wide) multiplies it by 0.85, 1 or 1.2 (578 / 680 / 816 at
  Medium) in three steps, not a free slider: a step is a decision a reader makes once and a slider asks to be
  adjusted again for every window, while the window already limits the column by itself. Standard is the default.
  The window can limit it further, and then the steps that do not fit look the same.
- **Headings are real headings.** Give the title of a screen or section `Role(ui.RoleHeading)` and a `Level` so
  assistive technology can navigate by them.
- **Do not truncate silently.** Prefer wrapping. Where a single line is needed (`SingleLine`, `MaxLines`), for
  example an entry title in the timeline, make the full text available in a tooltip or in the reader.
- **Numbers and dates.** Show counts and dates in the user's language when they are data, not identifiers.

## Chinese typesetting

Rules for Chinese text in the reader, written without reference to the toolkit. They change how text is laid out,
never the text: no character is inserted or removed, so copying, search and accessibility see the original string.
Values are in [design/tokens.md](../design/tokens.md), Reading text; drawn in the `reader` page (`cjk=on|off`) and
the `settings` page (`appearance`, `reading-cjk-off`).

**Where and when.** One per-device switch, Chinese typesetting (Settings, Appearance, Reading), on by default. It
applies to text that is Chinese, decided as the reader already decides Han text: the language stored for the entry
starts with `zh`, and when none is stored, the text contains Han characters. It covers everything the reader sets as
prose: the article title, h2 and h3, paragraphs, list items, quotes, captions, the notes above the text, and AI
summaries and translations (a translation by its target language). It does not cover code blocks or inline code, and
nothing in the timeline, the pane or the rest of the interface. Off, text is set as the font gives it.

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

**The ends of lines.** The platform's text engine breaks the lines, and MyGo does not expose where it broke them, so
line ends are left to that engine: a mark at the start or end of a line keeps its blank half, and the engine's own
line-break rules apply, which differ between platforms and fonts.

## Icons and images

- **Icons are SVG** shown with `ui.Icon`, which takes the text color and the font size: an icon follows theme colors
  and the text size automatically. Every icon has the same stroke weight, 1.6, at every size. See
  [Icon](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/icon.md).
- **One set: [Lucide](https://lucide.dev)** (ISC License), recorded in `THIRD_PARTY.md`. The files of lucide-static
  are copied unchanged into `internal/ui/icons/lucide/` and parsed once in `internal/ui/icons`; add an icon there
  before using it. Prefer an icon to a word where the icon is clearer, and keep the word where it is not.
- **An icon needs a name when it is alone.** Icons are decoration for assistive technology unless given a `Label`.
  A button that shows only an icon or a symbol must get a readable name with `.Label()`; an icon next to a visible
  text label needs none.
- **Icon before label** in buttons, except a forward arrow, which follows its label ("Connect →"). Icons come in
  three sizes, small 14, medium 16 and large 24 (the values are in `design/tokens.md`), all scaled with the text
  size; every icon is one of them.
- **Feed images** (thumbnails, image entries, site icons) use
  [Image](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/image.md). Download and decode them in the background and
  make each bitmap once, never in the view; until it is ready, keep the image's box at its final size so the timeline
  does not jump. Images that carry meaning get a `Label`; decorative ones do not.
- **Pictures.** Pictures are downloaded through the server (`/api/image`), at most four at a time, and the 300 most
  recent bitmaps are kept. A picture that fails to load keeps its box on `Track` with a muted image icon. A video
  cover carries a play mark and the duration, labelled "Duration 12:34", and keeps them when its picture fails.
- **Kinds and site icons.** Each kind has an icon (newspaper, play, message circle, image; inbox for All). Kinds are
  not colored: only the chosen kind's icon takes `Accent`. A subscription's monogram is a `Track` tile with its letter
  in `Text2` (on the pane, pane ink 11% and `ChromeText2`). A subscription shows its site icon where it has one: a
  square tile with radius 4 and a 0.5 DIP inset rim in ink 20%, so a dark icon still reads on the dark pane. The icon
  loads through `/api/image` in the background; the monogram stands until it is decoded and stays when there is none
  or it fails, so nothing jumps.
- **Status marks.** The unread dot is a 7 `Accent` circle; a failing subscription is `Warning` with a mark; the server
  is a `Success` dot, `Warning` while background checks fail; a starred entry is `theme.Star`.
- **The product icon** is a drop of ink on a warm ivory tile (`logo-tile`), with the subscription sign inside it. The
  drop is ink (`logo-body`, `#26231f`); two arcs are cut out of it to the tile, and the sign's dot is teal
  (`logo-piece`), drawn as a sphere from `#5eead4` to `#0f766e`, the same teal as the unread dot. The arcs and the dot
  share one center and are spaced evenly. The drop says paper and ink, the product's ground, and the sign says what it
  does. The icon sits on the macOS grid (an 824 tile at 100 in 1024, radius 185) with transparent corners, so every
  platform shows the same rounded tile. The menu bar template is the drop in black on transparent, with the arcs and the
  dot cut out. The logo is managed in the design system's Logos group and copied to [design/](../design/) (`icon.svg`,
  `mark.svg`, `tray.svg`); see [design/README.md](../design/README.md) for copying and exporting it. It was drawn for
  Oriel and is MIT licensed with the rest of the project.

## Component guide

Pick the widget by the job. Each name links to the upstream page, which also says what assistive technology sees of
the widget. The complete list is the
[UI index](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/README.md).

What Oriel uses:

| Component | Use |
|---|---|
| `ui.Button` | Secondary actions. A button that shows only a symbol must give a readable name with `.Label()`. |
| `ui.PrimaryButton` | The main action of a group of buttons; at most one. |
| `ui.Text`, `ui.RichText` | Text; `RichText` where one word of a sentence is set apart. Secondary explanations use `TextMuted`. |
| `ui.TextInputBase` | A text field drawn by the screen, inside a box with its icon; `Error` names what is wrong for assistive technology, and the screen shows it under the field. The timeline's search is one too, since `SearchField` moves the focus to its box instead of the text. |
| `ui.ButtonBase` | Buttons drawn by the screen in the Oriel look: an icon and a label, or an icon alone with a `Label`. |
| `ui.Icon` | Lucide icons from `internal/ui/icons`. |
| `ui.SegmentedBase` | The timeline's list of kinds and its Everything/Unread/Starred filter, the kind of a subscription, and the reader's Translation/Original switch, drawn in the Oriel look. |
| Select trigger | The button of a popup menu in Settings: it hugs its content, label then a normal gap (8) then the chevron, 32 high, padding 10 left and 8 right, at least 72 wide so a very short label is not a chip and at most 240 (a longer label is cut with an ellipsis). It is never stretched to a common column width, so there is no gap between label and chevron beyond the normal one. The menu it opens may be wider than the trigger (at least 160). Form fields that fill their row (the select in a subscription form) are fields, not triggers, and keep the full width. |
| `ui.DialogBase` | The add and edit subscription dialogs and the change sheet of Settings → Subscriptions: Escape and a click outside close them, the window's keys stop behind them, and the focus goes back where it was. |
| `ui.SelectBase`, `ui.Switch`, `ui.TextArea` | A subscription's fetch interval and keep period, its pause switch and its mute keywords. |
| `Element.ContextMenu` | Right click on a subscription, a folder, an entry or the mark all as read button; the items are drawn in the `timeline` page (`menu-feed`, `menu-entry`, `menu-markall`, `menu-remind`). The menu is the platform's own, so its items are text only, with no icons or key hints; an item that has nothing to do (no unread above or below the entry, none in the subscription) is dimmed and does nothing. |
| Font picker | A popup list for a long list of fonts: the Interface font and the Reading font in Settings, Appearance. It is anchored under its control and drawn with `theme.Sheet` at the Popup level; the list scrolls inside, the search field above it stays and has the focus when the popup opens. Every family name is set in its own face (a family that cannot draw its own name falls back to the interface font; a name is cut with an ellipsis, never wrapped). The caller supplies the pinned rows, which sit above a hairline; the picker supplies the search, the list, the keys and the states, and a family already pinned is not repeated in the list. The current choice has a check; the keyboard highlight is a `Chosen` fill, and starts on the current choice. Typing filters, case-insensitive, matching anywhere in the name; the pinned rows are searched with the rest. The match is not marked inside a name, so the face stays true to what it will look like; the highlight moves to the first row. Up and Down move the highlight (Home and End jump, it does not wrap), Return chooses it and closes the popup, a click chooses a row, Escape closes the popup without a change and the focus returns to the control, as it does after choosing; Tab does nothing special. Choosing applies at once, like the other Appearance controls. The family list is read in the background: until it arrives the popup shows the pinned rows and a loading row, and typing is kept and applied when it arrives. Accessibility: the list is a list of options (MyGo has no listbox role, so list and list item), each row named by its family name with the current one selected and said so; the result count is announced with `Announce` as the query changes, after a short pause, and the loading and empty rows are status text. Typing never blocks the UI: filtering is a substring test over the loaded names, run only when the query or the list changes, and only the visible rows are built. Drawn in the `settings` page, `font-open`, `font-filtered`, `font-empty`, `font-loading`. |
| `ui.List`, `ui.GridView` | The timeline's rows and its video cards; both build only what is in view. |
| `ui.DialogBase` as a CDialog confirm | Confirming deleting a subscription, a folder or several subscriptions. Cancel has the focus, so Return does nothing harmful, and the Delete action is in `Danger`; Escape and a click outside cancel. |
| `c.Toast`, `c.Announce` | Feedback after an action, and failures; counts announced without moving the focus. A title takes up to two lines, then an ellipsis, with the whole text in a tooltip while it is cut and as the accessible name. |
| `c.ToastAction` | An action that can be undone: deleting a subscription, and every way of marking as read (mark all, mark above, mark a subscription, mark older than, the V key), which act at once. The toast offers Undo and a close button for 8 seconds, is announced and never takes the focus; one Undo is shared by all, and Cmd/Ctrl+Z undoes while the toast shows. Undoing a delete restores the subscription with its entries and all that went with it, and a toast says when the server can no longer undo it. Marking more than 10000 entries offers no Undo; when nothing was old enough to mark, a plain toast says so. The toast is centered at the bottom as an `Inverse` tile with `InverseText`. |
| Command palette | Cmd/Ctrl+K, from the timeline and the reader: a `ui.DialogBase` that goes to any subscription, kind or Settings page and runs the common actions. A search field labeled "Go to…" has the focus when it opens; below it the list, in groups (Actions, Kinds, Subscriptions, Settings), scrolls inside. A row shows its icon, its name, and a tabular unread count (above 999 it reads "999+") or the action's shortcut as key chips. While the reader shows, the Actions group has one more row, "Immersive reading", named "Exit immersive reading" while it is on. The highlight is a `Chosen` fill and starts on the first row; Up and Down move it and wrap, it scrolls into view, Return or a click runs the row and closes the palette, Escape or Cmd/Ctrl+K closes it. Typing filters and ranks (prefix, word start, substring, then scattered letters; case and spaces ignored) over the loaded names only, with no request; at most 50 rows, and the matches are one ranked list without headers, each row naming its group after its name. Accessibility: rows are list items named by their group and name, the highlighted one is announced with `Announce`, and the result count is announced as the query changes. Drawn in the `timeline` page, `palette`, `palette-query`, `palette-none`. |
| `ui.DialogBase` (key sheet) | The keyboard shortcut sheet: every key, grouped by where it works, each key a small chip. Escape or Close dismisses it. |
| `ui.DialogBase` (lightbox) | The pictures of a social or image entry, full window; its keys and names are under Keyboard and focus and Screen readers. A click on a picture in a row opens it on that picture, Space on a chosen row on the first; the title and text still open the browser. It uses the dialog's focus return, Escape and key blocking, with its own paging keys; the pictures come through the image proxy and the decode cache like the rows' do. No video. Drawn in the `timeline` page, `lightbox`, `lightbox-failed`. |

**Tooltips.**

- A button that shows a text label has no `Tooltip`: not a segmented control, and not a label that is a number, may be
  cut short or hides in a narrow window. The label is the name, and a tooltip that repeats it is noise. A shortcut is no
  reason to keep one; the key sheet lists the keys.
- An icon-only button keeps a `Tooltip` with the text of its accessibility `Label`. On a toolbar the tooltip also names
  the button's keyboard shortcut, when it has one.
- Where an icon carries the meaning, the icon stands alone and its words go into the tooltip and the `Label`, as for
  the reader's summary and translate and the bulk actions of a list.
- The one exception to the first rule is a row whose text is a name of the user's data that the layout cuts short (a
  subscription in the pane): it has a `Tooltip` with the full name, only while the name is actually cut, found by
  measuring the text against its box. A row that fits has none. The row's `Label` is the full name either way and its
  `Description` is the unread count: the tip is drawn with `theme.Tip` (`TooltipBase` in the look of
  `Tooltip`), which does not describe the row, so a screen reader does not read the name twice.
- The accessibility `Label` stays on every button, whether or not it has a tooltip.

MyGo's `Tooltip` has no placement option: it sits above the element and flips below only where there is no room
above. A tooltip on an icon-only button in the top row (the timeline and reader toolbars) can therefore cover the macOS
window buttons. This is a known limit of MyGo; Oriel does not work around it with its own `TooltipBase` anchoring.

The tables below say what to reach for as screens are added.

### Actions

| Need | Use |
|---|---|
| The main action of a group, once | [Button](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/button.md) (`PrimaryButton`) |
| Other actions | [Button](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/button.md) |
| A text button in settings, subscriptions or connect | `theme.Button` (`internal/ui/theme/button.go`): one 32 high button, 26 when small, in four styles (`Secondary`, `Primary`, `Danger`, `Plain`); the sizes and fills are in `design/tokens.md`. A screen draws no button of its own. `Danger` is text in `Danger` with no fill until hovered, never a solid red button |
| Open an entry's original page in the system browser | [Link](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/link.md) |
| Several related actions in one place | [Menu button](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/menu-button.md) |
| Actions on a specific entry or subscription, on right click | [Context menu](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/context-menu.md) |
| An on/off command that stays pressed | [Toggle](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/toggle.md) |
| Choose one of a few views or modes | [Segmented control](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/segmented.md) |
| Window-level commands | [Toolbar](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/toolbar.md) |

### Choices

| Need | Use |
|---|---|
| A setting applied immediately (for example a per-subscription switch) | [Switch](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/switch.md) |
| A yes/no value submitted with a form, or several independent options | [Checkbox](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/checkbox.md) |
| One of two to five visible options (for example a subscription's kind) | [Radio](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/radio.md) |
| One of many options | [Select](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/select.md); with typing to filter, [Combobox](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/combobox.md) |
| A value in a range | [Slider](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/slider.md); an exact number, [Number input](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/number-input.md) or [Stepper](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/stepper.md) |

### Text

| Need | Use |
|---|---|
| Free text, one or many lines (a feed URL, an RSSHub path, a prompt) | [Text input](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/text-input.md) |
| Searching the timeline | [Search field](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/search-field.md), or [Find bar](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/find-bar.md) for text in the reader |
| Mute keywords | [Token field](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/token-field.md) |
| Rename a subscription in place | [Editable text](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/editable-text.md) |
| Labeled controls with help and errors | [Form](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/form.md) |

### Collections

| Need | Use |
|---|---|
| The timeline, and other long lists of one kind | [List](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/list.md) |
| Rows with sortable, resizable columns (for example subscription health) | [Table](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/table.md) |
| Hierarchy | [Tree](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/tree.md), or [Outline](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/outline.md) when large |
| Thumbnails | [Grid view](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/grid-view.md) |

### Containers and navigation

| Need | Use |
|---|---|
| Scrolling content, such as the reader | [Scroll view](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/scroll.md) |
| Columns and rows of controls | [Grid](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/grid.md) |
| Two resizable panes (timeline and reader) | [Split view](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/split.md) |
| Optional detail | [Collapsible](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/collapsible.md), [Accordion](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/accordion.md) |
| Top-level sections of the app | [Sidebar](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/sidebar.md) |
| A few peer pages inside a screen | [Tabs](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/tabs.md) |
| Pages with a history | [Router](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/navigation.md), with [Back and forward buttons](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/history-buttons.md) |

### Dialogs, messages and status

| Need | Use |
|---|---|
| A task that needs the user's full attention | [Dialog](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/dialog.md) |
| Confirm something important or destructive (deleting a subscription and its entries) | a CDialog confirm on [Dialog](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/dialog.md) (the danger chip, Cancel focused, the action in `Danger`); for system file and message dialogs (OPML import and export, the startup error), `mygo.Dialog` ([desktop APIs](https://github.com/egoist/mygo/blob/v0.7.4/docs/native.md)) |
| A small panel tied to an element | [Popover](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/popover.md) |
| A short label or hint for a control | [Tooltip](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/tooltip.md) (never the only place for essential information) |
| Feedback that needs no decision | [Toast](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/toast.md) |
| Known progress / unknown duration (a fetch, an AI summary) | [Progress bar](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/progress.md) / [Spinner](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/spinner.md) |
| A count or short status (unread entries) | [Badge](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/badge.md) |

Overlays share focus and Escape behavior; see
[Overlays](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/overlays.md). Rules of thumb:

- Prefer undo over confirmation. Confirm only what cannot be undone, with a button named for the action ("Delete"),
  not "OK", and Cancel as the Escape action. Deleting a subscription does both: the dialog asks first, since its
  starred entries go too, and the toast that follows offers Undo for a few seconds.
- Dialogs, the palette and the key sheet are `theme.Sheet` at the Dialog level over `theme.Scrim`; select menus and the
  font picker are `theme.Sheet` at the Popup level. No overlay sets its own shadow, rim or scrim; the values are in
  design/tokens.md, Sheet and scrim.
- Do not stack dialogs. Use a toast for news that needs no answer.
- Errors say what happened and what to do, next to the control that caused them (`Error`, or a
  [form](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/form.md) field). The server returns stable error codes;
  the UI maps each to localized copy and never shows the raw code alone.
- AI summaries and translations are on demand and can be slow or fail: show that the work is going on, keep the
  original text readable meanwhile, and say plainly when it failed.

## Windows, menus and navigation

How the window looks on each platform, the menu bar menu, the app menu and the startup failure are drawn on the
`5 桌面` page of the [screens canvas](https://claude.ai/artifact/M1YW6F7vwnQm2Ze8BJnYcb). This section holds what a
drawing cannot show: the menus' items and shortcuts, how the window behaves, the keyboard and the focus, and where the
platforms differ.

### Window

- **Size and state.** One main window; set `Title`, `Width`, `Height`, and `MinWidth`/`MinHeight` so content never
  clips. Oriel's window is 960×640 with a minimum of 640×480. `StateKey` (`oriel-main`) remembers size, position and
  the maximized state between runs, in `window-state.json` in the user data directory, a client-side file next to
  `preferences.json`. A saved position that does not show on a connected display falls back to a centered window on
  the primary display, at the saved size as far as that display allows. A second launch brings the existing window to
  the front. See [Windows with native UI](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/windows.md) and the
  [window options](https://github.com/egoist/mygo/blob/v0.7.4/docs/windows.md).
- **Title bar.** No title bar: `TitleBarStyle: mygo.TitleBarHidden`, the macOS window buttons placed with
  `TrafficLightPosition` over the sidebar's top, level with the toolbar row. The view owns the room the window
  controls take (`c.TitleBar()`: on Windows the caption buttons at the right, which the toolbar leaves free) and the
  draggable area: the toolbar row's empty space and the sidebar's top (its buttons apart) call `DragWindow`. The
  window's name stays set (`Title`) for the Window menu, the Dock and screen readers. With the sidebar hidden the
  macOS buttons sit over the toolbar row, which pads its leading edge by `TitleBar().Left`; in immersive reading they
  sit in the empty band at the top, so text is never under them and the band stays the drag area.
- **Sidebar.** Hiding the sidebar removes the pane, with no icon rail and no animation. The choice is a device
  preference and is remembered. Immersive reading hides it for the reader only and never changes that preference.
- **Glass and ink.** Glass (the [glass plugin](https://github.com/egoist/mygo/blob/v0.7.4/docs/plugins/glass.md)) is
  for chrome only, never for entries, the article, dialogs, search or bare toolbar icons; content stays on opaque
  `Background`. The sidebar is the opaque [ink pane](#color-and-dark-mode). The toolbar row is a solid bar over the
  content, drawn by `theme.Edge`, with a hairline along its bottom once the content has scrolled under it. Text on
  glass keeps AA contrast, and with high contrast glass becomes opaque with a `Border` rim (MyGo reports no Reduce
  Transparency setting, so high contrast is the one switch).
- **Close and quit.** As docs/product.md says: with the menu bar or tray icon on, closing the window only hides it and
  Oriel keeps running; Quit in the tray menu or the app menu ends it. Without the icon, closing the last window quits.
  The Dock badge alone does not change this. Opened at login with the icon on, Oriel shows only the icon and no window.
- **Tray and Dock.** The tray menu is the system's own menu, not drawn by Oriel. Its items, top to bottom: a disabled
  "N unread" line ("No unread" at 0), a separator, Open Oriel, Refresh all, a separator, Quit, in the interface
  language. The Dock badge and the macOS menu bar title show the unread count, `999+` above 999, and nothing at 0. A
  headless server has no window, tray, badge or login item.
- **Leaving the app.** Video and social entries have no embedded player; opening them hands the link to the system
  browser. Say so in the control ("Open in browser"), not only with an icon.
- **Notifications.** Use system notifications only for events the user would want when the app is in the background.

### Menus

- **Menu bar.** The macOS menu bar belongs to the app. Oriel's menu has an app menu (About, check for updates,
  "Connect to Server…", which opens the connect screen, "Settings…" (Cmd/Ctrl+,), Quit), the Edit menu
  (`RoleEditMenu`, so copy and paste work), the Timeline menu, a Subscriptions menu with "Refresh All" (Cmd/Ctrl+R),
  the Window menu (`RoleWindowMenu`) and the Help menu with "Keyboard shortcuts" and "Export Diagnostics…" (which opens
  Activity and storage in Settings and starts the export).
- **Timeline menu.** The commands that have a single key: show unread only, show all entries, show starred only, mark
  all as read, load new entries, and immersive reading (it does something only while the reader shows). None has an
  accelerator: single keys cannot be menu accelerators, as they would fire while a field has the focus, so the key
  sheet and the command palette show the keys.
- **Roles and accelerators.** Use roles (`RoleAbout`, `RoleQuit` and the others) for standard items: they carry the
  right label and shortcut, and items that only macOS has are left out elsewhere. Give custom items an accelerator with
  `CmdOrCtrl`. Every command reachable by a toolbar button should also be in a menu. See
  [Menus](https://github.com/egoist/mygo/blob/v0.7.4/docs/menus.md).

### Navigation and toolbars

- **Navigation.** Choose the lightest structure that fits: a single screen;
  [tabs](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/tabs.md) for a few peer views; a
  [sidebar](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/sidebar.md) with a
  [router](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/navigation.md) for several top-level sections (timeline,
  subscriptions, settings). A router gives back/forward keys, state restored per page and focus handling. Give every
  page a `Title`.
- **Toolbar.** Frequent window-level commands, in order of importance; controls that do not fit overflow into a menu
  in a fixed order, listed first in it. One glass capsule at most per toolbar; everything else is a bare icon, a
  segmented groove or the search field. Search is last, at the far right.
- **Toolbar buttons.** Every icon-only toolbar button has a name and a tooltip ([Tooltips](#component-guide)). A
  button that does not apply to the current article is disabled, never hidden, so the toolbar keeps its shape: it
  keeps its place and its label, takes no focus or click, and its tooltip and accessible name give the reason
  (Translate on an article already in the target language says "Already in Simplified Chinese"). A toggle says its
  state in its name as well as its fill (Summarize pressed is "Hide the summary", Translate pressed is "Show the
  original").

### Keyboard and focus

- **Order.** Tab follows the visual order. In the timeline's toolbar: the sidebar button, the segments, the buttons,
  search. Every entry is a focusable element with its own name, so Tab reaches it.
- **Timeline keys.** J or Down and K or Up move between entries, N goes to the next unread entry below the chosen one
  (loading the next page when needed, with a toast when there is none), O or Enter opens the chosen one (an article in
  the reader, anything else in the browser) and marks it read, Space opens the pictures of a chosen social or image
  entry in the lightbox, S stars it, M toggles read, V marks the entries fully in view as read (below), T cycles the
  time range, `/` and Cmd/Ctrl+F go to search while the list shows (in the reader Cmd/Ctrl+F opens find in the article
  instead), Escape clears it. The search field opens with `/` and closes again when it loses the focus with no text in
  it. The list keys are window-wide but stop while the search field has the focus or a dialog is open, so typing never
  moves the list. Cmd/Ctrl+K opens the command palette. Cmd/Ctrl+N adds a subscription, Cmd/Ctrl+R refreshes all
  subscriptions, Cmd/Ctrl+Z undoes the last "mark as read" action while its toast shows, and `?` or Cmd/Ctrl+/ opens
  the key sheet, which the Help menu also lists as "Keyboard shortcuts".
  Five more keys act on the list, under the same rules (not while the search field has the focus, a dialog is open or
  the reader shows): U shows unread only, A shows all entries, `*` (Shift+8) shows starred only, Shift+A marks
  everything in the scope as read, with the same toast and Cmd/Ctrl+Z undo as the toolbar button (inert where the
  button is disabled), and R loads the entries the "N new" pill offers and goes to the top (inert when there is no
  pill).
- **Mark visible (V).** It marks the entries fully inside the list area as read, then scrolls so the first entry below
  them is at the top, chosen and focused. A row, card or tile cut by the toolbar or the window's bottom is not marked;
  in a grid a row of cards counts whole. With no unread in the range V only scrolls and shows no toast, so it also
  pages. The scroll is immediate (MyGo's `ListState.ScrollTo` and `GridState.ScrollTo` do not glide) and not undone by
  Undo. V works in All and Unread without a search, and does nothing under Starred or a search. The toast is announced,
  and the new chosen entry is then read by its own name; there is no extra announcement.
- **Fold row.** The row that shows a subscription's folded entries is a button: Enter, Space or a click opens it, and
  "Showed {n} entries" is announced. Tab reaches it; it is not part of J and K, "next unread" or "mark visible".
- **Refresh all.** Cmd/Ctrl+R works in the timeline and the reader, but not while a dialog, the palette or the lightbox
  is open; it is a key sheet row under Anywhere and a palette action. Pressing it again while it runs does nothing.
- **Sidebar keys.** 1 to 5 switch to All and the four kinds, in the sidebar's order, from anywhere in the timeline. H or
  Left (expanding the sidebar first if it is hidden) moves the focus to the chosen item of the sidebar (the kind, or the
  subscription when the scope is one), Up and Down walk its kinds, which choose as the focus passes, and its
  subscriptions, which wait for Enter (one vertical focus group each, so Tab still goes through the sidebar in two
  stops, and Left and Right are not the groups' but go back to the list); the focus stays on the item chosen, and L,
  Right or Escape gives the focus back to the list, the chosen entry focused. While the sidebar has the focus by the
  keyboard (H, Left, Tab, the arrows) the entry keys (J K O S M N V, Up and Down) do not touch the entries; `/` still
  goes to search. A click on a kind or subscription does not count: the entry keys keep acting on the list. Shift+F10 or
  the Menu key opens the context menu of the focused subscription or entry, which MyGo does natively for an element
  with `ContextMenu`. All of these are off while the search field has the focus, a dialog is open or the reader shows.
  Cmd+\ on macOS and Ctrl+\ elsewhere hides and shows the sidebar, from the timeline and the reader, and is in the key
  sheet under Anywhere.
- **Sidebar filter.** Tab reaches the filter field and then the Unread first toggle before the list. In the field
  Escape clears the text and, when it is empty, gives the focus to the chosen item of the sidebar; Down or Enter gives
  it to the first match. While the field has the focus the keys of the list and sidebar (J K O S M N V, H L, 1 to 5)
  type into it instead, as in the search field.
- **Reader keys.** While the reader shows, J and K go to the next and previous article, N to the next unread one (a
  toast when there is none, staying on this article), S stars, M toggles read, O opens the original in the browser,
  Z (plain, not with Cmd/Ctrl) enters and leaves immersive reading, Cmd/Ctrl+F opens find in the article, and Escape
  goes back to the list, the entry it opened from focused. Escape works in this order, one step per press: the find
  row, the selection card, immersive reading, then back. The article takes the focus when it opens, so the arrows,
  Space, Page Up/Down and Home/End scroll it, and Tab goes through its links.
- **Immersive reading.** Cmd/Ctrl+\ leaves it instead of toggling the sidebar. J, K, N, S, M and O work as ever, so
  nothing needs the pointer. The hidden toolbar is only transparent, not removed, so Tab reaches its buttons and a
  button taking the focus shows it; it also shows while a menu from it is open. A focused toolbar button that
  disappears hands the focus to the article. It ends with the reader and is not remembered across restarts; the
  system's full screen is separate and untouched.
- **Find in the article.** Cmd/Ctrl+F does nothing while the article is still loading or has failed. Enter or Cmd+G /
  F3 goes to the next match and Shift+Enter or Shift+Cmd+G / Shift+F3 to the previous, both wrapping; reopening
  selects the last query. Escape or Done closes the row, clears the highlight and returns the focus to the article
  body. A match it steps to is scrolled clear of the row, and a match in a table scrolls the table sideways as well.
- **In the article.** A table wider than the column takes the keyboard focus and its arrow keys scroll it. A footnote
  reference scrolls to its note and marks it ([Motion](#motion)); the note's back arrow returns the same way. Tab
  reaches each Try again of a failed picture in reading order.
- **Lightbox.** Left and Right page, Home and End jump, O opens the entry in the browser, Escape or a click on the dim
  layer closes (a click on the picture does nothing). Tab order, which is also the reading order: Open in browser,
  Open picture in browser, Close, Previous, Next, then the picture area; Tab wraps within the dialog. It opens with the
  focus on its own picture area, so the arrow keys work at once and no ring is drawn there. Closing returns the focus
  to the row that opened it.
- **Dialogs.** Escape or a click outside closes a dialog and the focus goes back where it was. A confirmation of
  something destructive (deleting a subscription, restoring a backup) starts with the focus on Cancel, so Return does
  nothing harmful. A one-field dialog (a folder's name) opens with the field focused and Return saves. A popover list
  (the subscriptions of a rule) stays open while ticking and closes with Escape, a click outside or its button, with
  the focus back on the button.
- **Settings.** The back button and Escape return to the timeline as it was. Escape never throws typing away: with a
  text field focused it only takes the focus off the field (to the back button; the text stays), and with text not
  saved it does not leave and the page does not change, it only announces "Some changes are not saved". A MyGo text
  field does not consume Escape, so the page decides. A select or a switch applies at once; a text box carries its own
  Save, quiet until it changed and primary after. In a multi-line box Enter breaks the line, so only Save sends it. A
  refused value keeps the focus in its field.
- **Settings → Subscriptions.** The list is one tab stop. A click on the checkbox toggles a row; a click elsewhere on
  it, or Enter, opens its page. Cmd/Ctrl+click toggles a row and Shift+click selects the range from the last clicked
  row. Up and Down move the focus, Home and End go to the ends, Space toggles the focused row, Shift+Up and Shift+Down
  extend the range from the anchor, Cmd/Ctrl+A selects every visible row, and Escape clears the selection before it
  does anything else. Down in the filter field moves the focus to the list. While a batch action runs Escape does
  nothing. On a subscription's page the crumb button and Escape go back to the list with the row focused.

### Screen readers

- **Names.** A row's name carries what the eye reads beside it: a translated title adds the original ("translation,
  original"); a reminder reads "reminder on {time}"; an entry about to leave the timeline ends with "leaving the
  timeline soon"; an updated entry ends with "Updated"; a subscription hidden from All reads "{name}, hidden from
  All", one being fetched adds ", fetching"; a capped unread count ("999+") keeps the full number in its description.
  A search highlight is paint only: it is not its own element, takes no focus, is not announced and does not split the
  row's name. The search hint is tied to the field with a description and never announced on its own. The password
  eye keeps the name "Show password" and says by its pressed state whether the password shows. A toggle such as Unread
  first exposes on or off, not only its fill.
- **Names in the reader.** A footnote reference is named "Footnote N". A row standing for a video, audio or embed is
  named "{words}. Open in browser", with the element's title joining the name only; without a link it is not a button
  and takes no focus. A data table is read as "Table, N columns, M rows". A failed picture is a group named "{alt}.
  This picture can't be shown" and its button "Try loading the picture again". A changed paragraph of an updated
  entry reads "Removed: text" or "Added: text", in a list named "Changes". The find row is a toolbar named "Find in
  article"; the selection card has the group role named after its task.
- **Names in dialogs and lists.** The subscription health dialog is labeled by its title, each row a list item named
  by the source and its reason, its buttons carry the source's name ("Pause {name}"). The subscriptions list in
  Settings is a multi-selectable listbox named "Subscriptions", each row an option named "{title}, {kind}, {folder},
  {interval}" with "paused", "hidden from All" or the failure appended; the action bar is a toolbar named by the
  count; a result band is an alert; the checkbox is never the only way, and a selected row is told by its state, not
  by the fill alone. The lightbox is named "Pictures from “{title}”", its picture "Picture {n} of {total}".
- **Announcements.** With `c.Announce`: a scope switched from the keyboard ("Videos, 12 unread", the focus staying
  where it was); a list order chosen, by its name; immersive reading on and off; the find count as it changes ("2 of
  4", "No matches"); a selection card's result when it arrives, and "Copied"; "Picture {n} of {total}" when the
  lightbox opens and pages; the loaded count of the health dialog; the selection count in Settings → Subscriptions
  after it settles for half a second, with the progress and result of a batch action; "Refreshing 12 subscriptions"
  once when refresh all starts. A failed picture is not announced, since a page can have many, nor is a media row. A
  status that stays (the offline banner, an import's progress line, a drop zone) is polite and announced once; an
  import's count is announced only as it passes a quarter, for files of 20 or more subscriptions.
- **Not by color alone.** A drop zone is told by its outline and words; a search highlight and the current find
  match by an underline as well as the fill; a failed picture by its icon and words.

### Platforms

- **One look.** One view serves macOS, Windows and Linux. The glass plugin draws the same glass on every platform; no
  system vibrancy is used.
- **Keys.** Shortcuts use `CmdOrCtrl`. A Mac keyboard has no Menu key, so the key sheet lists Shift+F10 alone there.
- **Desktop settings.** On macOS the Desktop card has Open at login, Menu bar icon and Unread badge on the Dock icon; on
  Windows and Linux it has Open at login and Tray icon, the help saying "tray" instead of "menu bar", and no row is
  shown disabled. The card appears in the desktop app only. The tray icon is a one-colour template image on macOS and
  the app icon elsewhere. On Linux without libayatana-appindicator3 the tray icon cannot start: its switch returns to
  off with an error line. On macOS, when Oriel does not run from an app bundle, Open at login is disabled and its help
  says only the installed app can open at login.

## Accessibility checklist

MyGo exposes native UI to VoiceOver, Narrator, NVDA and Orca; see
[Accessibility](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/accessibility.md).

- [ ] Every interactive element has an accessible name: its text, or `Label` when it shows only an icon, a symbol or
      a drawing.
- [ ] `Description` carries help text; `Error` marks invalid values.
- [ ] Custom elements that act as widgets set `Role`, and the states they show (`Checked`, `Expanded`, `Range`).
- [ ] Section and screen titles are headings with a `Level`.
- [ ] Changes that do not move the focus (a count, new entries arriving, a finished summary) call `c.Announce(text)`.
      Toasts announce their text, and a router the title of a page shown while the focus stays outside it.
- [ ] The whole UI works with the keyboard: the focus order is the standard one and follows reading order, there is
      a visible focus ring (do not remove it), shortcuts exist for frequent actions, Escape closes overlays.
- [ ] Text inside a find match keeps 4.5:1 or more in both schemes.
- [ ] A row's screen-reader label reads source, title, time, reading time, unread or read, starred; the time-range
      button's name states the current range and, when it is oldest first, the order, and its menu items
      are radio items with the current one checked.
- [ ] Text uses theme sizes (`Rem`) and scales; layouts reflow at larger text and in the longer translation.
- [ ] Colors are theme roles; contrast holds in both appearances and with high contrast on, and on every Reading
      appearance (the numbers are in tokens.md, Reading appearance presets).
- [ ] A Reading appearance or Line width chosen in Settings applies to an open reader at once, keeps the scroll place
      and is announced by the control's own state; neither is the only cue for anything.
- [ ] Nothing depends on color, sound or motion alone.
- [ ] Pointer targets are at least about 24×24 DIPs, and 32 or more for primary controls. A control drawn smaller
      keeps its drawn size and layout slot: `theme.Hit` wraps it in an invisible 24 by 24 target with a negative
      margin, so neighbors do not move and targets do not overlap.
- [ ] `ui.NewTester` tests click controls by their accessible names, so a missing name fails the test (see
      [Testing](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/testing.md)).

Contrast, scaling, keyboard use and screen readers are verified on real devices; a passing headless test does not
count as accessibility acceptance.
The [Inspector](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/inspector.md) (F12 in development builds) shows
the element tree, roles and accessible names of a running window, which helps find a missing name before that check.

## Motion

Oriel is for reading, so motion stays out of the way of the text. It keeps your place (a popup scales out of the
control that opened it, a folder opens where it was, the scroll position stays when panes and bars change) and it
acknowledges the pointer (state fills, link color, the row actions). Nothing in the reading column or the list moves on
its own, nothing slides, and closing takes no longer than opening. See
[Transitions](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/transitions.md) and
[Drawing and animation](https://github.com/egoist/mygo/blob/v0.7.4/docs/ui/drawing.md) for what the toolkit offers.

The motion tokens are `motion-fast` (120 ms, ease-out: state fills, text color, row actions, a popup scaling in from
0.97 with a fade), `motion-base` (200 ms, ease-out: folding a folder or group, a toast) and `motion-spin` (900 ms,
linear), in `design/tokens.json`.

- **Default is none.** State changes take effect at once. Do not add `Transition`, `Animate` or page transitions
  without a mockup that shows the change they explain and the maintainer's agreement; record the exception here.
- **State fills fade.** The hover, pressed and chosen fills of rows (navigation rows, entry rows, the server bar,
  settings groups), text color and the underline color of links fade over `motion-fast`, colors only, through
  `theme.Fade`, so the pointer moving down a list leaves a soft trail instead of a flicker. A faded row needs a stable
  `Key`. Nothing moves or resizes; controls drawn by MyGo keep their own behavior.
- **Row actions fade.** The row actions fade in and out over `motion-fast`, opacity only.
- **Short and interruptible.** Nothing is longer than 200 ms, only color and opacity change, and a new state starts
  from where the old one is. If another exception is agreed, use the built-ins with their defaults (element
  transitions are 200 ms, ease-out); avoid frame-by-frame custom animation.
- **Reduced motion.** `Animate`, page transitions and the moving parts of element transitions already jump to the
  target when the desktop asks for less motion (`c.Preferences().ReduceMotion`); color and opacity fades still play,
  since nothing moves. Any motion you compute yourself must read the preference. Spinners may keep turning: they show
  that work is going on.
- **Never animate to hide latency**, and do not loop decoration.
- **New entries never move the list.** A "N new" pill appears at the top instead, and choosing it reloads and scrolls
  to the top.

The agreed exceptions beyond the state fills and row actions:

- **Pictures fade in.** A decoded picture fades in over 140 ms, opacity only; its box was held before it arrived, so
  nothing moves.
- **The toolbar hairline.** Once the content has scrolled under the toolbar row, the hairline along its bottom fades
  in over 120 ms (`motion-fast`), colors only.
- **The footnote highlight.** The note a footnote reference leads to stays marked for about 1.6 seconds; with reduced
  motion the mark does not fade.

## Localization

- The UI is in English and Simplified Chinese only. Every user-visible string is in the embedded catalog `copy.json`
  next to the screen, keyed `en` and `zh-Hans`. Change both together; do not add other languages.
- `locale.Language` (in `internal/ui/locale/`) maps the system locale: every Chinese locale, Traditional included,
  reads Simplified Chinese; everything else reads English. Screens use it rather than their own mapping. The Interface
  language preference (System, 简体中文, English; Settings, Appearance) overrides it: System means the mapping above.
  Its choices are named in their own language and are not translated. A change rebuilds the screens at once.
  MyGo's own words (find bar, dialogs, menus) follow the same language. MyGo keeps the language it is given for the
  next launches, and an app-level language beats the system's, so System gives it none and MyGo follows the system by
  itself; only a language chosen in Settings is given. The one exception is a Chinese in another script (zh-Hant,
  zh-TW): MyGo finds Simplified no match for it and would say English, so Simplified is given then.
- Help text is the exception, not the rule. When a control already says it (a label, a placeholder, a value, an
  icon) there is no line under it. A line that stays is a short fact: a state, a number, a time, a consequence
  ("Excludes starred · can't be undone", "Updated 3 min ago"); never what the control does or why it exists. A
  settings group has a title and no description.
- Every key has both translations, and each screen's test fails on a missing or untranslated entry. Accessible
  names are copy too: translate `Label` and `Description`.
- Feed content (titles, text, author names) is shown as it comes, in whatever language it is in; it is not copy.
  Translation of content is the on-demand AI feature, not localization.
- English labels: short imperatives ("Reset", "Add subscription"), sentence case everywhere, menu items, dialog
  titles and buttons included ("Keyboard shortcuts", "Back up…", "Replace data"); only names keep their capitals
  (Markdown, OPML, RSSHub). No trailing period on labels and a full stop on sentences. Chinese sentences use
  full-width punctuation. Do not concatenate sentence fragments, since word order differs; use full-sentence strings
  with placeholders.
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
  | reading time | 阅读时间 | The row label is "{n} 分钟阅读"; there is no reading-time filter. |
  | sign in, sign out | 登录, 退出登录 | A device is signed out, never "吊销". |
  | password | 口令 | |
  | server | 服务端 | Not 服务器, which names the machine. |
  | this computer | 本机 | |
  | device | 设备 | |
  | fetch | 抓取 | The schedule is spoken of as 检查 ("检查间隔", "检查过"). |
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
  it is inside a line ("Checked 5 min ago", "last used yesterday"). Every screen uses `locale.Ago`, not strings of its
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

- [ ] Every state in the mockup is reached in the app (each artboard of its page of the screens canvas).
- [ ] Both languages, light and dark, the default and the minimum window size checked.
- [ ] Keyboard-only run-through, and a screen reader pass on each target platform.
- [ ] High contrast and larger text size checked.
- [ ] Screenshots of each state, light and dark, are in the pull request body.

## Polish checklist

A reviewer's pass over any screen, after the acceptance checklist:

- [ ] Fewer borders: separation comes from space, a fill or a hairline between rows; no outline around a card, a
      row or a toolbar control.
- [ ] Neutrals are the scale (`Background`, `Surface`, `Track`, `Text2`): warm in light, graphite in dark, none a pure
      gray, black or white.
- [ ] One accent: the unread dot, links, the chosen icon and the primary action; kinds are not colored, and
      status colors appear only with text or a symbol.
- [ ] Radii are from the fixed scale (4, 8, 12, 14, 16); nothing is rounded by eye.
- [ ] No more than five type sizes on the screen, weights 400 to 600, hierarchy by lightness before weight.
- [ ] Counts, times and durations use tabular numerals.
- [ ] Small labels are tracked; large titles are tightened.
- [ ] Line height follows the script: Simplified Chinese runs taller than English at the same size.
- [ ] Motion is short (`motion-fast` for fills and row actions, never above 200 ms), color and opacity only, and
      interruptible; anything else is one of the exceptions in [Motion](#motion).
- [ ] Hairlines separate rows only, and drop away from a hovered or chosen row.
- [ ] Icons share one stroke weight (1.6) and sit on the text's optical center.
- [ ] Every control shows a focus ring when reached with the keyboard.
- [ ] The empty, loading and error states are designed, not left as a blank or a raw message.
- [ ] Hover, pressed and chosen are lightness steps of the same ink fill, in that order, never a new hue.
- [ ] Chinese and Latin text sit together cleanly: no stray spaces next to full-width punctuation, full-width
      punctuation in Chinese sentences, and Latin runs in a Chinese line keep the same size and color.

## References

This design language draws on the UI documentation of MyGo (MIT License, Copyright (c) egoist):
<https://github.com/egoist/mygo/tree/v0.7.4/docs/ui>, together with `docs/native.md`, `docs/menus.md` and
`docs/windows.md` from the same version. The text here is a summary in our own words; the upstream pages are the
source for APIs and behavior. See also [THIRD_PARTY.md](../THIRD_PARTY.md).
