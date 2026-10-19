# Design tokens

The values Oriel's UI uses, in one place. Each is a role or a unit of the MyGo theme (`c.Theme()`), so screens read
the theme and never copy the numbers; the colors and the radius are set on the theme by `internal/ui/theme`. The rules behind them are in
[docs/design.md](../docs/design.md).

## Spacing and size

The unit is `Theme.Spacing`: 4 DIPs by default (3 compact, 5 roomy). In code, `t.Space(n)` is `n` units and
`t.Rem(n)` is `n` times the theme font size (13 on macOS, 14 elsewhere, scaled by the desktop's text size).
Mockups show the 4 DIP unit and 13 px text.

| Token | Code | Value at defaults (macOS / elsewhere) | Used for |
|---|---|---|---|
| space-1 | `t.Space(1)` | 4 | label to control |
| space-1.5 | `t.Space(1.5)` | 6 | field to its error line |
| space-2 | `t.Space(2)` | 8 | gap between related controls (button row, fields) |
| space-4 | `t.Space(4)` | 16 | gap between groups (column gap) |
| space-6 | `t.Space(6)` | 24 | outer window padding, gap between the parts of a sheet |
| control | `t.Space(8)` | 32 | height of fields and buttons drawn by a screen |
| sheet | `t.Space(80)` | 320 | widest a centered sheet (the connect screen) grows |
| symbol-button | `t.Space(11)` | 44 | width of square symbol buttons |
| radius | `Theme.Radius` | 8 | corners of controls |
| toolbar | `t.Space(13)` | 52 | the toolbar row; the window buttons are centered in it |
| capsule | `t.Space(8)`, radius 16 | 32 | glass toolbar controls: segments, search, button groups |
| pane-inset | `t.Space(2)` | 8 | the glass sidebar's distance from the window's edges |
| pane-radius | 16 | 16 | corners of the glass sidebar |
| icon | `t.Rem(1.25)` | about 16 / 18 | icons in fields and buttons; 1.05 rem in error lines |
| title | `t.Rem(1.8)` bold | about 23 / 25 | screen title, a level 1 heading |
| secondary | `t.Rem(0.9)` | about 12 / 13 | hints, in `TextMuted` |
| body | theme `FontSize` | 13 / 14 | everything else, and all widget text |
| window | `Width`, `Height` | 960 x 640 | main window |
| window-min | `MinWidth`, `MinHeight` | 640 x 480 | smallest supported size |

## Color roles

Oriel's palette, paper and ink: warm paper neutrals, a softened ink and a muted teal accent, set by `theme.Apply`
in `internal/ui/theme/theme.go`. Body text stays near 13:1 rather than black on white, the surface ladder steps
evenly, and every text role holds 4.5:1 on `Background`, `Surface` and `SurfaceHover`. The other roles keep the
MyGo defaults. With high contrast on, `Border` is mixed 45% and `TextMuted` 40% toward `Text`,
and `Focus` is opaque. A soft fill (`theme.Soft`) is the role mixed toward `Background`, 88% in light, 84% in dark.
The chosen fill in navigation (`theme.Chosen`) is neutral: `Text` at 7% alpha in light, 9% in dark.

| Role | Light | Dark | Used for |
|---|---|---|---|
| `Background` | `#f8f6f1` | `#1d1c1a` | window fill, focused field |
| `Surface` | `#efebe3` | `#262422` | control faces, fields |
| `SurfaceHover` | `#e7e2d8` | `#302d2a` | hovered control |
| `SurfacePressed` | `#ded8cc` | `#3a3733` | pressed control |
| `Border` | `#e3ddd2` | `#36332f` | outlines, dividers |
| `Text` | `#2b2824` | `#e4ded5` | primary text |
| `TextMuted` | `#68625a` | `#a09a91` | hints, placeholders, secondary text, field icons |
| `Accent` | `#2b6f66` | `#6cbcae` | primary button, focused field border, checked controls |
| `AccentHover` | `#245e57` | `#86cbbf` | hovered primary button |
| `AccentPressed` | `#1f4f49` | `#58a597` | pressed primary button |
| `AccentText` | `#ffffff` | `#10241f` | text on the accent |
| `Danger` / `Warning` / `Success` | `#c2410c` / `#a16207` / `#15803d` | `#fb923c` / `#facc15` / `#4ade80` | status, always with text |
| `Selection` | `rgba(43, 111, 102, 0.22)` | `rgba(108, 188, 174, 0.3)` | selected text |
| `Focus` | `rgba(43, 111, 102, 0.5)` | `rgba(108, 188, 174, 0.5)` | keyboard focus ring, 3 DIPs |
| `Inverse` / `InverseText` | zero: `Text` / `Background` | zero: `Text` / `Background` | tooltips and toasts |
| `Scrollbar` | `rgba(0, 0, 0, 0.32)` | `rgba(255, 255, 255, 0.35)` | scroll bar thumbs, 6 DIPs wide |

## Motion

| Token | Value |
|---|---|
| default | none: state changes take effect immediately |
| state fill | 140 ms, ease-out, colors only: the hover and chosen fills of rows (`theme.Fade`) |
| spinner | one turn in 900 ms, linear: the busy icon of a button |
| element transition | 200 ms, ease-out (MyGo default); only as an agreed exception, see docs/design.md |
| reduced motion | nothing moves (`Preferences().ReduceMotion`); color fades still play |

## Keeping this file true

Change a token here, in `docs/design.md` and in the code together. Mockups in `screens/` copy the color values above
as CSS variables. When the MyGo version in `go.mod` changes, check that `internal/ui/theme` still sets every color
role its `ui/theme.go` has.
