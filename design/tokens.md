# Design tokens

The values Oriel's UI uses are kept in [tokens.json](tokens.json) and [layout.json](layout.json), the one source, and
drawn in the [Oriel design system](https://claude.ai/artifact/LBUfZ9m6MHwCsXp4D1d97w). This file explains them; the
rules for using them on a screen are in [docs/design.md](../docs/design.md). Where this file and `tokens.json`
differ, `tokens.json` wins.

## In code

`tokens.json` is generated into Go as `internal/ui/theme/tokens` (`go generate` there). `TestGeneratedIsCurrent`
fails when the generated file is stale, and `TestContrastAudit` holds both schemes, the pane and both reading presets
to the audit's floors, so a color that misses cannot merge.

- Colors: `tokens.SchemeLight` and `tokens.SchemeDark` hold every main and pane token; `SetRoles` and `SetPaneRoles`
  set the MyGo roles the tokens marked `mygo` stand for. `tokens.Presets` holds Sepia and Mist, `tokens.HighContrast`
  the state ladder with high contrast on.
- Shadows: `tokens.ElevationLight` and `ElevationDark`.
- Space, radius, size and opacity are constants in DIPs at the default text size (`tokens.Space3`,
  `tokens.RadiusCard`, `tokens.Control`, `tokens.OpacityFaded`); package `theme` scales them with the text size and
  density. `SetMetrics` sets MyGo's Spacing and Radius.
- Type: `tokens.TextTitle` and the other interface styles carry English and Chinese metrics; the reading scale is
  `tokens.ReadingBody` and the others, with the reading choices in `ReadingSizeScale`, `ReadingLineSpacing` and
  `ReadingLineWidth`.
- Motion: `tokens.MotionFast`, `MotionBase`, `MotionSpin` and the popup scale `MotionPop`.

The `canvas` family of the design system (the desktop wallpaper behind the desk boards) is design-only and is not
kept here.

## Color

Colors come in layers, and each layer only sits on the one below it.

| Layer | Tokens | Use |
| --- | --- | --- |
| Region | `paper`, `pane` | The window and the reader's page, the ink pane. |
| Surface | `surface`, `float`, `raised` | Code blocks, notes, fields; popups and dialogs; the chosen segment. |
| State | `fill` < `hover` < `selected` < `pressed` | Ink at rising alpha over whatever is beneath. One ladder for every control. |
| Role | `accent`, `accent-hover`, `accent-pressed`, `on-accent`, `soft` | The single teal and its tinted ground. |
| Text | `fg`, `prose`, `fg2`, `muted`, `faint` | Strongest to weakest. `faint` is for disabled and decoration only. |
| Meaning | `star`, `danger`, `success`, `warning` | Starred entries, destructive actions, done, attention. |
| Line | `line`, `line2`, `rule`, `scrollbar` | Hairlines between rows, rims of floating things, rules in prose. |

The pane has its own roles, prefixed `pane-` (`pane-fg`, `pane-muted`, `pane-selected`…); everything not listed keeps
the main value. The reader has two appearance presets, **Sepia** and **Mist**, prefixed `sepia-` and `mist-`; they
replace the page region and the roles drawn on it inside the reader's content area only. Their alpha fills are the
preset's ink at the main ladder's alphas.

**Meaning colors in the pane take their dark value.** The pane is dark in both schemes, and the light `warning`,
`danger`, `success` and `accent` fall to about 2:1 on it, while their dark values reach 4.8:1 or more on both panes.
So a health mark, a warning tag's icon or the status dot in the pane is drawn under `data-theme="dark"`, whatever
the window's scheme.

**MyGo roles.** Some tokens set a role of MyGo's theme, so MyGo's own fields and buttons draw with the system:
`paper` Background, `fill` Surface, `hover` SurfaceHover, `pressed` SurfacePressed, `fg` Text, `muted` TextMuted, the
`accent` family Accent, AccentHover, AccentPressed and AccentText, the meaning colors, `selection`, `focus`, `inverse`
and `on-inverse`, `line` Border, `scrollbar` Scrollbar; `space-1` is Spacing and `radius-control` is Radius. Every
other token is read by name.

**Contrast targets**, checked in light, dark, the pane and both presets, on every ground listed in the `audit` token family (paper, hover, selected, surface, float, a hovered
menu row; pane, pane hover, pane selected), with alphas rounded to a byte as MyGo stores them: `fg` 10:1, `prose` 8.5:1, `fg2` 7:1, `muted` 4.5:1, `accent`, `danger`, `success` and `on-accent`
4.5:1, `star` and `warning` 3:1 (icons), `faint` 2.6:1 (never for text that must be read). In the pane: `pane-fg` 8:1,
`pane-fg2` 6:1, `pane-muted` 4.5:1, `pane-accent` 3:1, `pane-faint` 2.6:1. `TestContrastAudit` computes
every one of them, and a value that misses cannot merge.

**High contrast.** With the desktop's high contrast on, the state alphas rise (.06 / .08 / .12 / .16), `line` becomes
`fg` at .55, `muted` moves 40% toward `fg`, focus is opaque accent and floating things gain a 1 DIP rim.

## Type

Three families. **Sans** is the interface: the platform's UI font (SF on macOS, Segoe UI on Windows, the GTK font on
Linux) with PingFang SC, Microsoft YaHei or Noto Sans CJK for Chinese, or the Interface font chosen in Settings.
**Serif** is for the reading body only, when Reading font is Serif: New York and Songti SC on macOS, Georgia and
SimSun on Windows, Noto Serif and Noto Serif CJK SC on Linux. Titles, headings, notes and captions keep sans. **Mono**
is the platform's own monospace, for code and addresses. A screen sets no other family. Every style has an English and a Chinese line height; Chinese lines are
taller and carry no tracking.

| Style | Size / line | Weight | Use |
| --- | --- | --- | --- |
| `display` | 24 / 30 (zh 34) | 600 | The reader title. `display-compact` 21 / 27 below 760 wide. |
| `title` | 15 / 20 (zh 22) | 600 | Row titles, section heads, screen titles. |
| `body` | 13 / 20 (zh 22) | 400 | Summaries, social text, cards, anything read as prose at interface size. |
| `label` | 13 / 18 (zh 20) | 500 | Buttons, menu items, pane items, fields. |
| `meta` | 12 / 17 | 400 | Times, hints, status lines. `meta-strong` 500 for the source name and segments. |
| `caption` | 11 / 14 (zh 12 / 16) | 500, +0.04em | Pane section heads, counts, durations, badges. |
| `code` | 12 / 19 mono | 400 | Rewrite rules, prompts, addresses. |

The **reading scale** is separate and set by the reader's preferences: body 16 at line 1.7 (zh 1.85), h2 19, h3 16,
code 13; text size Small ×0.933, Large ×1.133, Extra large ×1.267; line spacing Compact 1.55, Relaxed 1.85; line width
Narrow ×0.85, Wide ×1.2. Between Han and Latin text sits a 0.25em gap.

## Space, size and shape

- **Spacing** is a 4-based scale (`space-0_5` 2 to `space-12` 48) that follows the density setting (3 Compact, 5
  Roomy). Gaps grow with distance in meaning: `space-1` label to control, `space-2` related controls, `space-3` inside
  a card, `space-4` between groups, `space-6` between the parts of a sheet, `space-8` between a page's parts.
- **Heights** come in fixed steps and nothing in between: a control is `control` (32) or `control-small` (24); a row
  is `row-dense` 24, `row` 28 (the pane's rows too) or `row-compact` 36; the toolbar is 52. A tag or badge is `tag` 18. All are on the 4 grid, except `tag`, which sits on 2.
- **Widths**: `menu-min` 160 for a menu or a select, `sheet-width` 320 for a narrow form, `dialog` 440 for a dialog,
  `dialog-wide` 560 for a list dialog and the command palette, `column` 680 for reading.
- **Radius** by role: `radius-xs` 4 tiles and badges, `radius-sm` 6 popup rows and segments, `radius-control` 8
  controls and pane items, `radius-card` 12 entry rows, cards and popups, `radius-pane` 14 the pane, sheets and dialogs,
  `radius-pill` capsules and dots.
- **Elevation**: `elevation-raised` the chosen segment, `elevation-popup` menus, `elevation-dialog` dialogs. Each is a
  shadow plus a 0.5 rim in `line2`.
- **Opacity** has four tokens and no others: `opacity-faded` (0.55) for a row still there that has stepped back,
  `opacity-separator` (0.7) for the dot of a meta line, `opacity-tint` (0.14) for a status pill's fill in its own
  meaning color, and `opacity-wash` (0.08) for a status strip's fill over paper. A disabled control does not fade: its
  glyph takes `faint` and its label stays `muted`, so the label keeps 4.5:1.

## Layout

One window, two frames: the ink pane on the left (`pane-width` 216, 184 below 760) and a toolbar bar 52 high. The
content is one centered column at most `column` wide (`column-social` 720 for social timelines). The default window
is `window-w` × `window-h`. The pane hides with 隐藏侧栏 at the end of its traffic-light row (⌘\\); then
the main area takes the whole window, and its bar starts with the lights' room and 显示侧栏. The lights' row in the
pane is centered on the toolbar's line, so the lights never move. A settings page opens with its head in the
column: the page's icon in a `soft` square and its name in `display`; its groups follow
`space-8` apart, each named for what it holds, never repeating the page. Dialogs are built like the window: a body
and a `surface` sill that holds the buttons.

`layout.json` holds the rules and the named patterns every assembly uses, from a mark beside its words up to the
window; `layout.css` is generated from it for the canvas, and the screens follow the same file. The patterns: `pair`,
`row`, `controls`, `control`, `track`, `bar`, `stack-tight`, `stack`, `stack-block`, `stack-section`, `stack-page`,
`inset-card`, `inset-sheet`, `column`, `center`, `spacer`. Fine tuning uses the `l-*` utility classes, whose values
are tokens only (`l-px-3`, `l-gap-2`, `l-h-row`, `l-sq-tile-m`, `l-grow`). A screen writes no size of its own; a
value no token has, or a new arrangement, is added here first. Below 760 the structure stays and the frames narrow.

## Iconography

Lucide icons at a 1.6 stroke, drawn in the text color. `icon-s` 14 beside 12 and 13 text, `icon-m` 16 for bare icon
buttons and beside 15 text, `icon-l` 24 for empty, loading and error states. A source without its own icon gets a
monogram tile (`tile-s` 16, `tile-m` 18, `tile-l` 32) whose letter is 0.58 of the tile at weight 600. No emoji, no
illustration.

## Motion

Oriel is for reading, so motion stays out of the way of the text. It does two jobs only:

- **It keeps your place.** A popup scales out of the control that opened it, from 0.97 with a fade; a folder opens
  where it was; the timeline and the article keep their scroll position when panes, bars or the toolbar change.
- **It acknowledges the pointer.** State fills, text and link color, and the row actions fading in.

Only color, opacity, a popup's small scale and a fold change; nothing slides, and nothing in the reading column or
the list ever moves on its own. `motion-fast` (120 ms, ease-out) for color and opacity, `motion-base` (200 ms,
ease-out) for a fold or a toast, nothing longer except the busy spinner, which turns once per 900 ms. No springs.
Closing takes no longer than opening, and a change that starts mid-way starts from where the last one is. Keyboard
actions are immediate. With reduced motion, scale and folds are immediate and only fades of 120 ms or less remain; the
spinner keeps turning. The values are the `motion` tokens: `motion-fast`, `motion-base`, `motion-spin` and `motion-pop` (0.97).

## Logo

The `logo-*` tokens color the mark only and never appear in the interface. The drawings and how they are kept in
step with the design system are in [README.md](README.md#icons).
