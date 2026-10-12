# Design tokens

The values Oriel's UI uses, in one place. They are not new tokens: each is a role or a unit of the MyGo theme
(`c.Theme()`), so the code reads the theme and never copies the numbers. The rules behind them are in
[docs/design.md](../docs/design.md).

## Spacing and size

The unit is `Theme.Spacing`: 4 DIPs by default (3 compact, 5 roomy). In code, `t.Space(n)` is `n` units and
`t.Rem(n)` is `n` times the theme font size (13 on macOS, 14 elsewhere, scaled by the desktop's text size).
Mockups show the 4 DIP unit and 13 px text.

| Token | Code | Value at defaults (macOS / elsewhere) | Used for |
|---|---|---|---|
| space-1 | `t.Space(1)` | 4 | label to control |
| space-2 | `t.Space(2)` | 8 | gap between related controls (button row) |
| space-4 | `t.Space(4)` | 16 | gap between groups (column gap) |
| space-6 | `t.Space(6)` | 24 | outer window padding |
| symbol-button | `t.Space(11)` | 44 | width of square symbol buttons |
| radius | `Theme.Radius` | 6 | corners of controls |
| title | `t.Rem(1.8)` bold | about 23 / 25 | screen title, a level 1 heading |
| display | `t.Rem(3.6)` bold | about 47 / 50 | the example's count |
| secondary | `t.Rem(0.9)` | about 12 / 13 | hints, in `TextMuted` |
| body | theme `FontSize` | 13 / 14 | everything else, and all widget text |
| window | `Width`, `Height` | 480 x 320 | main window |
| window-min | `MinWidth`, `MinHeight` | 360 x 280 | smallest supported size |

## Color roles

Values are the defaults of `ui.LightTheme()` and `ui.DarkTheme()` in MyGo v0.3.6. The app also follows the user's
accent color and contrast settings, so mockups approximate it.

| Role | Light | Dark | Used for |
|---|---|---|---|
| `Background` | `#ffffff` | `#18181b` | window fill |
| `Surface` | `#f4f4f5` | `#27272a` | control faces |
| `SurfaceHover` | `#e9e9ec` | `#323236` | hovered control |
| `SurfacePressed` | `#dddde1` | `#3c3c41` | pressed control |
| `Border` | `#d9d9de` | `#3f3f46` | outlines, dividers |
| `Text` | `#18181b` | `#f4f4f5` | primary text |
| `TextMuted` | `#71717a` | `#a1a1aa` | hints, placeholders, secondary text |
| `Accent` | `#2563eb` | `#3b82f6` | primary button (`+`), checked controls |
| `AccentHover` | `#1d4ed8` | `#60a5fa` | hovered primary button |
| `AccentPressed` | `#1e40af` | `#2563eb` | pressed primary button |
| `AccentText` | `#ffffff` | `#ffffff` | text on the accent |
| `Danger` / `Warning` / `Success` | `#dc2626` / `#d97706` / `#16a34a` | `#ef4444` / `#f59e0b` / `#22c55e` | status, always with text |
| `Selection` | `rgba(37, 99, 235, 0.25)` | `rgba(59, 130, 246, 0.4)` | selected text |
| `Focus` | `rgba(37, 99, 235, 0.55)` | `rgba(96, 165, 250, 0.6)` | keyboard focus ring |
| `Inverse` / `InverseText` | zero: `Text` / `Background` | zero: `Text` / `Background` | tooltips and toasts |
| `Scrollbar` | `rgba(0, 0, 0, 0.32)` | `rgba(255, 255, 255, 0.35)` | scroll bar thumbs, 6 DIPs wide |

## Motion

| Token | Value |
|---|---|
| default | none: state changes take effect immediately |
| element transition | 200 ms, ease-out (MyGo default); only as an agreed exception, see docs/design.md |
| reduced motion | jump to the target (`Preferences().ReduceMotion`) |

## Keeping this file true

Change a token here, in `docs/design.md` and in the code together. Mockups in `screens/` copy the color values above
as CSS variables. When the MyGo version in `go.mod` changes, compare the color values with its `ui/theme.go`.
