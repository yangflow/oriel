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

Oriel's palette: warm neutrals and a teal accent, set by `theme.Apply` in `internal/ui/theme/theme.go`. The other
roles keep the MyGo v0.4.1 defaults. With high contrast on, `Border` is mixed 45% and `TextMuted` 40% toward `Text`,
and `Focus` is opaque. A soft fill (`theme.Soft`) is the role mixed toward `Background`, 88% in light, 84% in dark.

| Role | Light | Dark | Used for |
|---|---|---|---|
| `Background` | `#fbfaf7` | `#161513` | window fill, focused field |
| `Surface` | `#f2f0eb` | `#25231f` | control faces, fields |
| `SurfaceHover` | `#e9e6df` | `#2f2c28` | hovered control |
| `SurfacePressed` | `#dfdbd2` | `#3a3732` | pressed control |
| `Border` | `#dcd7cd` | `#3d3933` | outlines, dividers |
| `Text` | `#1f1d1a` | `#ede9e3` | primary text |
| `TextMuted` | `#6f6a64` | `#a8a29e` | hints, placeholders, secondary text, field icons |
| `Accent` | `#0f766e` | `#4fbfad` | primary button, focused field border, checked controls |
| `AccentHover` | `#115e59` | `#6dd0c0` | hovered primary button |
| `AccentPressed` | `#134e4a` | `#3aa596` | pressed primary button |
| `AccentText` | `#ffffff` | `#0b2522` | text on the accent |
| `Danger` / `Warning` / `Success` | `#c2410c` / `#a16207` / `#15803d` | `#fb923c` / `#facc15` / `#4ade80` | status, always with text |
| `Selection` | `rgba(15, 118, 110, 0.22)` | `rgba(79, 191, 173, 0.3)` | selected text |
| `Focus` | `rgba(15, 118, 110, 0.5)` | `rgba(79, 191, 173, 0.5)` | keyboard focus ring, 3 DIPs |
| `Inverse` / `InverseText` | zero: `Text` / `Background` | zero: `Text` / `Background` | tooltips and toasts |
| `Scrollbar` | `rgba(0, 0, 0, 0.32)` | `rgba(255, 255, 255, 0.35)` | scroll bar thumbs, 6 DIPs wide |

## Motion

| Token | Value |
|---|---|
| default | none: state changes take effect immediately |
| spinner | one turn in 900 ms, linear: the busy icon of a button |
| element transition | 200 ms, ease-out (MyGo default); only as an agreed exception, see docs/design.md |
| reduced motion | jump to the target (`Preferences().ReduceMotion`) |

## Keeping this file true

Change a token here, in `docs/design.md` and in the code together. Mockups in `screens/` copy the color values above
as CSS variables. When the MyGo version in `go.mod` changes, check that `internal/ui/theme` still sets every color
role its `ui/theme.go` has.
