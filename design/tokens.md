# Design tokens

The values Oriel's UI uses, in one place. Each is a role or a unit of the MyGo theme (`c.Theme()`), or a helper in
`internal/ui/theme` for what the theme has no role for, so screens read the theme and never copy the numbers; the
colors and the radius are set on the theme by `internal/ui/theme`. The rules behind them are in
[docs/design.md](../docs/design.md). The direction is ink and paper: an ink pane for the sidebar (warm charcoal in light, deep teal in dark), a paper
canvas for content (warm in light, neutral graphite in dark), a quiet scale and one clean sans for the interface and reading alike.

## Spacing and size

The unit is `Theme.Spacing`: 4 DIPs by default (3 compact, 5 roomy). In code, `t.Space(n)` is `n` units and
`t.Rem(n)` is `n` times the theme font size (13 on macOS, 14 elsewhere, scaled by the desktop's text size).
Mockups show the 4 DIP unit and 13 px text. Sizes taken from a mockup are written `px(t, n)`, which is
`t.Rem(n / 13)`.

| Token | Code | Value at defaults (macOS / elsewhere) | Used for |
|---|---|---|---|
| space-1 | `t.Space(1)` | 4 | label to control |
| space-1.5 | `t.Space(1.5)` | 6 | field to its error line |
| space-2 | `t.Space(2)` | 8 | gap between related controls (button row, fields) |
| space-3 | `t.Space(3)` | 12 | inside a card or a note, between its parts |
| space-4 | `t.Space(4)` | 16 | gap between groups (column gap), row padding |
| space-6 | `t.Space(6)` | 24 | outer window padding, gap between the parts of a sheet, side padding of the timeline column |
| space-8 | `t.Space(8)` | 32 | the largest step: between the parts of a page |
| control | `t.Space(8)` | 32 | height of fields and buttons drawn by a screen |
| sheet | `t.Space(80)` | 320 | widest a centered sheet (the connect screen) grows |
| symbol-button | `t.Space(11)` | 44 | width of square symbol buttons |
| radius | `Theme.Radius` | 8 | corners of controls |
| toolbar | `t.Space(13)` | 52 | the toolbar row; the window buttons are centered in it |
| capsule | `t.Space(8)`, radius 16 | 32 | glass toolbar controls: the segmented control, the Summarize/Translate capsule |
| segment | 26 high, radius 13 | 26 | a segment inside the capsule, 3 from its edge, 2 apart |
| pane-inset | `t.Space(2)` | 8 | the ink pane's distance from the window's edges |
| pane-width | 216 (184 below a 760 wide window) | 216 / 184 | the ink pane |
| pane-radius | 14 | 14 | corners of the ink pane |
| pane-item | 30 high, radius 8, 10 padding, 10 gap | 30 | a kind, a subscription or a settings group in the pane |
| pane-add | 22 square, radius 8, 14 icon, 6 from the right edge of the pane's content (4 more around the icon makes 10, as the row counts) | 22 | the add button at the end of the pane's section header |
| pane-filter | 28 high, radius 8, 10 left padding, 6 gap; toggle 28 square, 4 gap | 28 | the subscription filter field and the Unread first toggle |
| column | `MaxWidth` 680 (720 for Social) | 680 / 720 | article, social and mixed rows in the timeline; the reader's text column is 680 at Medium and follows the Text size (Reading text) |
| row | padding 16, gap 14, radius 12 | | an entry row; 28 on the left, where the unread dot (7, 11 from the edge) sits |
| density-compact | row padding 16 to 10 (social compact 12 to 8), one-line article 36 to 30, gap between parts 14 to 10, text gap 3 to 2, large cover gap 8 to 6, grid and wall gap 14 to 10, wall tile gap 16 to 10 | | List density Compact; Comfortable keeps the values above |
| row-meta | 20 high | 20 | the meta line of a row; it keeps 88 free at the right for the row actions |
| thumbnail | 96 x 64, radius 8 | 96 x 64 | an article's picture, beside the title, below the meta line |
| row-compact | 36 high, radius 10, padding 0 16 0 28 | 36 | an article in the Compact viewer; source field 128 (96 below 760), title takes the rest, time 56 at least |
| social-compact-thumb | 56 square, radius 8; strip of up to four 40 squares, radius 6, 4 apart | 56 / 40 | pictures of a social post in the Compact viewer |
| cover-large | 16:9 at the row's content width, at most 640 wide (360 high), left-aligned with the text, radius 12 | about 620 x 350, at most 640 x 360 | a video in the Large cover viewer; title 15 semibold under it |
| video-grid | cells at least 200 wide, gap 16 across and 24 down, padding 16 24 32 | 200 | Videos scope; 16:9 cover, title 13 up to two lines, 7 below the cover |
| picture-wall | up to three columns at least 170 wide, column gap 14, tile gap 20 below, padding 16 24 32 | 170 | Images scope (Wall); each picture at its own aspect ratio, 4:3 when unknown; caption 13 semibold, one line |
| picture-grid | cells at least 170 wide, same gaps and padding as video-grid | 170 | Images scope (Grid); square picture, radius 10, title 13 semibold one line, 4 below |
| badge | radius 5, 11 medium white on ink 62%, 6 from the corner | | duration (bottom right) and picture count (top right) on a cover |
| icon | `t.Rem(1.25)` | about 16 / 18 | icons in fields and buttons; 1.05 rem in error lines; 14 in segments, 13 in the meta line |
| icon stroke | 1.6 | 1.6 | every Lucide icon, at every size |

Sizes of text are in [Typography](#typography); the window sizes follow.

| Token | Code | Value |
|---|---|---|
| window | `Width`, `Height` | 960 x 640 |
| window-min | `MinWidth`, `MinHeight` | 640 x 480 |

## Typography

Five sizes, three weights (400, 500, 600). Hierarchy comes from lightness first (`Text`, `Text2`, `TextMuted`), then
weight, then size. Sizes are `px(t, n)`; line height is a multiple of the size and is larger in Simplified Chinese
(zh), which runs taller. Tabular numerals (`FontFeatures("tnum")`) for every count, time and duration.
`LetterSpacing` is the em value times the size, in DIPs.

| Role | Size en / zh | Line height en / zh | Weight | Color | Tracking en / zh |
|---|---|---|---|---|---|
| Title (reader title, connect title) | 24 | 1.28 / 1.42 | 600 | `Text` | -0.015em / 0 |
| Row title, section head, screen title | 15 | 1.35 / 1.5 | 600; a read row's 500 in `TextMuted` | `Text` | -0.005em / 0 |
| Body UI (summary, social text, cards, notes, buttons) | 13 | 1.5 / 1.7 | 400 | `Text2` (social text `Text`) | 0 |
| Meta, labels, segments | 12 | 1.4 / 1.4 | source name 500 in `Text2`, the rest 400 in `TextMuted`; segments and the capsule 500 | | +0.005em (segments +0.01em) |
| Small label (section heads in the pane, counts, duration) | 11 / 12 | 1.3 | 500 | `TextMuted` (pane: `ChromeMuted`) | +0.04em / +0.02em |

Notes:

- Body UI in a card that reads as prose (the summary card) uses line height 1.65 / 1.75.
- Widget text (buttons, fields) uses the theme size, which is the 13 of this table.
- The reading body and its headings are a sixth, separate scale, set by the reading preferences below; they are
  the article's own text and not interface type. Headings in prose use the interface font: h2 19 / 600, h3 16 / 600,
  line height 1.35 / 1.4 (zh 1.5), h2 tracking -0.01em (zh 0).
- Monospace (code, addresses): 0.86em of the surrounding text in a block of 13, line height 1.6.

## Reading text

The reader's article text is set in the interface font by default, never a serif: Song and other thin serifs blur at
screen sizes and tire the eye over a long read. Four per-device preferences (Settings, Appearance, the Reading card)
change the article's text: the font, the size, the line height and Chinese typesetting. The default is Follow
interface, Medium, Standard and Chinese typesetting on. The choice of a serif is the reader's; the default does not change.

| Part | Value |
|---|---|
| Reading face | by default the interface font: the theme's `Font`, so the system UI font (SF and PingFang SC on macOS, Segoe UI and Microsoft YaHei on Windows, the GTK default on Linux) or the family chosen in Interface font; otherwise the Reading font below |
| Weight, color | 400, `theme.Prose` |
| Interface font preference | changes the reading face with the rest of the interface while the Reading font is Follow interface |
| Applies to | the article's body only (paragraphs, lists, quotes). The title, the source line, notes, captions, h2 and h3, code and the toolbar keep the interface font (code stays monospace) |

| Preference | Choice | Value |
|---|---|---|
| Reading font | Follow interface / Serif / an installed family | Follow interface (default): the reading face above. Serif: the platform serif set below. A family: that family, with the interface font behind it for the glyphs it lacks |
| Text size | Small / Medium / Large / Extra large | body 14.9 / 16 / 18.1 / 20.3 in every language, scale 0.933 / 1 / 1.133 / 1.267 (exactly 14/15, 1, 17/15, 19/15); column 635 / 680 / 771 / 861 |
| Line spacing | Compact / Standard / Relaxed | line height 1.55 / 1.7 / 1.85; Simplified Chinese 1.7 / 1.85 / 2.0 |

| Chinese typesetting | on / off | on (default). On: rules 1 and 2 below, for text that is Chinese (docs/design.md, Chinese typesetting). Off: text as the font gives it. Line height and paragraph spacing are the same either way |

Chinese typesetting values, in the em of the text they are drawn on, the same at every Text size and applied as letter
spacing on a character, never as inserted characters:

| Rule | Value | Where |
|---|---|---|
| Han and Latin letter or digit | +0.25em | after the last character before the boundary, in either direction; not beside a space, punctuation, symbol or code |
| Compression of full-width punctuation | -0.5em | after the first mark of a pair, when it is a closing mark followed by a closing or an opening mark, or an opening mark followed by an opening mark (pair 2em to 1.5em) |
| Reach | title, h2, h3, paragraphs, lists, quotes, captions, notes, AI summary and translation in the reader | not code, not the timeline or other interface text |

The Chinese line heights in the Line spacing row (1.7 / 1.85 / 2.0) and the 1.05em between paragraphs are the Chinese
values; they do not depend on the switch.

The scale multiplies the article's other sizes with the body: h2 (19) and h3 (16), code (13), and the spaces,
which are in em: 1.05em between paragraphs, 0.3em between list items, 1.9em above and 0.6em below h2. The list marker
column is 22 and its marker `TextMuted`. The title, the source line, notes, captions and the toolbar keep their sizes.

Serif is a platform set, a Latin face with a Chinese one behind it, falling back to the interface font when neither is
installed:

| Platform | Latin | Simplified Chinese |
|---|---|---|
| macOS | New York | Songti SC |
| Windows | Georgia | SimSun (宋体) |
| Linux | Noto Serif | Noto Serif CJK SC |

New York is not in the installed family list on macOS and MyGo does not find it by that name, so the code addresses it
by the system font name `.AppleSystemUIFontSerif`; it is taken as installed without a check.

The reading column grows with the Text size so that the characters per line stay about the same: its maximum width
(the `MaxWidth` of the column, padding included) is 680 times the size scale, rounded to a whole DIP: 635 (Small), 680
(Medium), 771 (Large), 861 (Extra large). The window still limits it: the column is never wider than the space the
window leaves beside the ink pane. Line spacing and the
Reading font do not change the width. The title, the source line and the note sit in the same column, and in a wide
window the toolbar lines up with its edges (docs/design.md, Reader). Social rows in the timeline keep 720; this rule is
the reader's text only.

## Color roles

Oriel's palette is warm paper in light and neutral graphite in dark, with an ink pane that is warm charcoal in light and deep
teal in dark: a low-chroma neutral scale, one muted teal accent, and status
colors in the same chroma band, set by `theme.Apply` in `internal/ui/theme/theme.go`. MyGo's `ui.Theme` has the
roles below and no way to add one, so the scale's other steps are helpers in `internal/ui/theme`, called with the
theme (`theme.Text2(t)`); a helper called with the pane theme returns the pane value. Every text role holds 4.5:1
on the surface it is drawn on (numbers below).

**Ink** is the color of every alpha fill: `Text` itself, rgb(37, 34, 30) in light and rgb(234, 240, 238) in dark,
written `theme.Ink(t, a)` (`t.Text` at alpha `a`; in the ink pane warm white rgb(255, 248, 235) in light, mint white
rgb(235, 248, 244) in dark). Fills, hairlines
and outlines are ink at an alpha, so they sit on any surface and step in lightness, not in hue.

### Roles on the theme

| Role | Light | Dark | Used for |
|---|---|---|---|
| `Background` | `#fdfcfa` | `#171918` | window and content fill (the paper) |
| `Surface` | `#f6f3ee` | `#1d1f1e` | a thumbnail placeholder, a code block, a note, a plain card, a control face |
| `SurfaceHover` | ink 3% | ink 4% | hovered control or row |
| `SurfacePressed` | ink 5% | ink 6% | pressed control or row |
| `Border` | ink 7.5% | ink 7.5% | hairlines between rows, dividers inside cards; not drawn around controls |
| `Text` | `#25221e` | `#dde1df` | titles, primary text |
| `TextMuted` | `#736d62` | `#8a918f` | meta, counts, read titles, hints, placeholders |
| `Accent` | `#2a6d65` | `#78beb1` | unread dot, links, a chosen icon, the AI card's head, the selection marker, the primary button |
| `AccentHover` | `#225a54` | `#8ccbbf` | hovered primary button |
| `AccentPressed` | `#1c4a45` | `#64aa9d` | pressed primary button |
| `AccentText` | `#ffffff` | `#10241f` | text on the accent |
| `Danger` | `#b4533c` | `#e0897a` | failure, destructive |
| `Warning` | `#9a7222` | `#d6aa55` | caution; a graphic, never text |
| `Success` | `#4c7c59` | `#8ab997` | the server dot |
| `Selection` | `Accent` 22% | `Accent` 30% | selected text |
| `Focus` | `Accent` 45% | `Accent` 50% | keyboard focus ring, 2 DIPs |
| `Inverse` / `InverseText` | `#2a2620` / `#f4efe6` | `#333837` / `#f4efe6` | tooltips and toasts |
| `Scrollbar` | ink 28% | ink 30% | scroll bar thumbs, 6 DIPs wide |

### Helpers in `internal/ui/theme`

| Helper | Light | Dark | Ink pane | Used for |
|---|---|---|---|---|
| `theme.Text2(t)` | `#58534a` | `#aab0ae` | `#cdc7bb` / `#cbd5d2` | source names, summaries, unselected names in the pane |
| `theme.Prose(t)` | `#3a362f` | `#c3c9c7` | | the reading body |
| `theme.Track(t)` | `#ece8e0` | `#2c2f2e` | pane ink 11% | the trough of a segmented control, a monogram tile, a skeleton bar |
| `theme.RaisedFill(t)` | `#ffffff` | `#252827` | | a focused search, a dialog, a sheet, a popover, the chosen segment on a `Track` |
| `theme.LineStrong(t)` | `#d4cec2` | `#3d4241` | | form edges, rarely |
| `theme.Disabled(t)` | `#b3ada1` | `#565b59` | | disabled icons and glyphs; the label beside them stays `TextMuted` |
| `theme.Star(t)` | `#b98a22` | `#e2b24f` | `#e2b24f` | a starred entry, the only hue besides status and the accent |
| `theme.Marker(t)` | `Accent` | `Accent` | `ChromeAccent` | the selection marker of a row |
| `theme.Chosen(t)` | ink 6.5% | ink 8% | pane ink 12% | the selected fill of a row or a navigation item |
| `theme.Soft(t, color)` | `color` 9% | `color` 12% | `color` 12% | a faint fill behind an icon or a status: the AI card, a note, a pressed labeled tool |
| `theme.Ink(t, a)` | rgb(37, 34, 30) at `a` | rgb(234, 240, 238) at `a` | pane ink at `a` | the alpha fills below |

Alpha fills built with `theme.Ink`: search field 5.5% (hover 7.5%), the chosen segment of the capsule 10%, inline
code 5.5%, a quote's line 16%, the outline of an image 10% (0.5 DIP), the segmented control in a note 7%.

`theme.Soft` replaces the old mix toward `Background`. The proposal's separate 10% (warning) and 13% (danger) are
folded into 9% / 12%.

### State fills

Ink at an alpha over any surface: one lightness step each, hover < pressed < selected.

| State | Content light | Content dark | Ink pane |
|---|---|---|---|
| Hover (`SurfaceHover`) | 3% | 4% | 5.5% |
| Pressed (`SurfacePressed`) | 5% | 6% | 8.5% |
| Selected (`theme.Chosen`) | 6.5% | 8% | 12% |

- A selected content row (the timeline) adds the selection marker inside the fill: `theme.Marker`, 2.5 wide,
  18 high, radius 2, centered vertically, 8 from the fill's left edge. No border.
- A selected item in the pane is the fill plus its icon in `Accent`. In the pane `Accent` colors icons only, never
  text: on the selected fill it is 4.5:1 in light, enough for a graphic (3:1) but with no margin for text.
- A hovered or selected row drops the separators above and below it.
- Fill radius: row 12, pane item 8, row action 8, control 8.
- On a selected row, `TextMuted` text (a read title, its summary) steps to `Text2`; on a selected or hovered pane
  item, the count steps from `ChromeMuted` to `ChromeText2`, and so does the icon on hover (a selected icon is `Accent`).
- With high contrast on, the fills are 8% / 12% / 16%.

### The ink pane

The sidebar is drawn with its own theme: `c.SetTheme(paneTheme)` before the sidebar's elements are created and the
main theme set back after (MyGo applies the theme to widgets created after the call). `theme.PaneTheme(main)` returns
it. It is a dark theme (`Dark` true) in both schemes, opaque (no glass, no blur), radius 14, 8 from the window's edges. In
light it is warm charcoal, which sits with the warm paper; in dark the content is neutral graphite and the deep teal ink
separates from it by hue, so the pane and the accent are the only color in dark mode. The fill, the ink and the text
roles differ by scheme; the accent and status colors do not.

| Role (pane theme) | Light scheme | Dark scheme | Name in the proposal |
|---|---|---|---|
| `Background` (the pane's fill, opaque) | `#37332e` | `#1a2422` | Chrome |
| `Surface` | pane ink 11% | pane ink 11% | |
| `SurfaceHover` / `SurfacePressed` | pane ink 5.5% / 8.5% | same | pane hover / pressed |
| `Border` (hairline) | pane ink 8% | pane ink 8% | pane hairline |
| `Text` | `#ece7dd` | `#e8eeec` | ChromeText |
| `TextMuted` | `#a49e91` | `#93a39f` | ChromeMuted |
| `Accent` | `#7cc9bb` | `#7cc9bb` | ChromeAccent |
| `AccentHover` / `AccentPressed` | `#92d5c9` / `#68b5a7` | same | |
| `AccentText` | `#10241f` | `#10241f` | |
| `Danger` | `#e0897a` | `#e0897a` | |
| `Warning` / `Success` | `#d6aa55` / `#8ab997` | same | |
| `Selection` / `Focus` | `Accent` 30% / 50% | same | |
| `Inverse` / `InverseText` / `Scrollbar` | as the main dark theme | | pane ink 30% for the scroll bar |
| `theme.Text2` | `#cdc7bb` | `#cbd5d2` | ChromeText2 |
| `theme.Track` (monogram fill) | pane ink 11% | same | |
| `theme.Chosen` | pane ink 12% | same | pane selected |

The pane is a plain view with an opaque `Background` from its own theme, not a `glass.Glass`: at 92 to 96% opacity
glass added only softness and a muddy mix with the backdrop. The pane ink is warm white rgb(255, 248, 235) in the light scheme and mint white
rgb(235, 248, 244) in the dark scheme. The rim is 0.5 DIP pane ink at 10% on the inside in
the light scheme and 8% in the dark scheme, with no border; the rim also keeps the pane visible on a dark window.
Names in the pane: `Text2` unselected, `Text` selected (weight 500); counts `TextMuted`, selected
`Text2`; section labels `TextMuted` in the small label style.

Contrast of the pane (WCAG; the pane is opaque, so the backdrop does not matter), light scheme `#37332e`: `Text`
10.2:1, `Text2` 7.5:1, `TextMuted` 4.7:1, `Accent` 6.5:1; dark scheme `#1a2422`: 13.6, 10.6, 6.1 and 8.3. On the
selected fill (12%), light scheme: `Text` 7.1, `Text2` 5.2, `Accent` 4.5; dark scheme: 9.5, 7.4 and 5.8. `TextMuted`
falls to 3.3 (light) and 4.2 (dark) there, and to 4.0 on the hover fill in light, so a selected or hovered item shows
its count in `Text2`. `Warning` and `Success` are graphics (3:1) on the pane: 3.9 on the selected fill at worst.

With high contrast on, the pane is already opaque; it only gains a 1 DIP `Border` rim (pane ink mixed 45% toward
`Text`), and `TextMuted` (mixed 40% toward `Text`) reaches 6.6:1 in light and 8.6:1 in dark.

### Accent and status

One accent, `Accent`. Status colors are in the same chroma band and are always with text or a symbol. The soft
forms (`theme.Soft`) tint an AI card, a note or a health line. `Warning` and `Star` are for graphics, `Danger`
and `Success` may be text.

Contrast on `Background` (light / dark): `Text` 15.4 / 13.4, `Prose` 11.7 / 10.5, `Text2` 7.4 / 8.0, `TextMuted`
5.0 / 5.5 (4.6 / 5.2 on `Surface`), `Accent` 5.9 / 8.2, `Danger` 4.8 / 6.8, `Success` 4.7 / 8.0, `Warning` 4.3 / 8.2,
`Star` 3.0 / 9.0. `Disabled` is about 2.2 / 2.6: it is for inactive graphics, which WCAG exempts, and never for the
only copy of a label.

### High contrast

With high contrast on (`c.Preferences().HighContrast`), `theme.Apply` changes: `Border` becomes `Text` at 55%
(opaque enough for 3:1 against `Background`), `TextMuted` is mixed 40% toward `Text`, `Focus` is opaque, the state
fills are 8% / 12% / 16%, and glass (the capsules, the scroll edge) becomes opaque: `Surface` with a 1 DIP
`Border` rim (`SurfaceHover` under the pointer).

## Glass

| Part | Light | Dark | Notes |
|---|---|---|---|
| Toolbar capsule | tint rgba(255, 254, 251, 0.66) | tint rgba(58, 63, 62, 0.52) | radius 16, rim 0.5 DIP |
| Scroll edge under the toolbar | `Background` 88% | `Background` 90% | 72 high, over the content and under the toolbar |

The timeline's toolbar has one capsule, the segmented control (Everything / Unread / Starred); the reader's has one
capsule, Summarize and Translate. Search is a flat ink 5.5% fill and every other toolbar button is a bare icon.
The reading-time button is a bare 32 square icon; with a budget it is a 32 high pill, radius 16, filled `Soft(Accent)`, with `Accent` text and icon, and in a narrow window an icon with a 7 `Accent` dot.
The ink pane is not glass: it is opaque (see above). Content never sits on glass.

## Shape

| Token | Value |
|---|---|
| Radii | 4 (site icon, small tile, inline code, duration badge), 8 (controls, pane items, row actions, thumbnails), 12 (rows, code blocks, notes, pictures, video covers), 14 (the ink pane, the AI card), 16 (capsules, dialogs), 22 (the window in mockups; the system draws it) |
| Hairline | 1 DIP, `Border`, between rows only (28 in from the left, 16 from the right); never around a card or control |
| Image outline | 0.5 DIP, ink 10% |
| Link | `Accent`, underline 1 DIP, offset 3, `Accent` at 38%, full `Accent` on hover |
| Inline code | ink 5.5% fill, `Text`, radius 4 |
| Shadow | only on raised things (dialogs, popovers, the focused search, the chosen segment in a control): `RaisedFill` and a soft ink shadow |

## Row actions

Star, mark read and open in browser are icon buttons, 27 wide and 24 high, radius 8, in `TextMuted` (hovered:
`SurfacePressed` fill and `Text`; a starred one in `theme.Star`). They sit at the right of the row's meta line,
which keeps 88 free (84 in a video card and a picture tile), and never over a thumbnail or a title. Shown on hover,
on the selected row and on the row with the focus; the fade is 120 ms.

## Motion

| Token | Value |
|---|---|
| default | none: state changes take effect immediately |
| state fill | 140 ms, ease-out, colors only: the hover, pressed and chosen fills, text color and link underline color (`theme.Fade`) |
| row actions | 120 ms, ease-out, opacity only |
| spinner | one turn in 900 ms, linear: the busy icon of a button |
| ceiling | nothing is longer than 200 ms and nothing moves; only color and opacity |
| reduced motion | nothing moves (`Preferences().ReduceMotion`); color and opacity fades still play |

## Keeping this file true

Change a token here, in `docs/design.md` and in the code together. Mockups in `screens/` copy the color values above
as CSS variables. When the MyGo version in `go.mod` changes, check that `internal/ui/theme` still sets every color
role its `ui/theme.go` has.
