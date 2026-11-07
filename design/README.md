# Design

Design values and the product icon for Oriel's native UI. The design language is
[docs/design.md](../docs/design.md); the values it uses are in [tokens.json](tokens.json) and
[layout.json](layout.json), explained in [tokens.md](tokens.md). The screens themselves are drawn outside the
repository, in two claude.ai Design artifacts kept by the maintainer. Mockups approximate the result; the app running
on a real device is what counts.

## Layout

| Path | Purpose |
|---|---|
| `tokens.json` | Every design value: colors in layers with the MyGo roles they set, the contrast audit, spacing, radii, shadows, opacities, sizes, text styles, the reading scale and choices, motion |
| `layout.json` | The layout rules, the named patterns and the utilities every assembly uses |
| `tokens.md` | Why the values are what they are, and how the code reads them |
| `icon.svg`, `mark.svg`, `tray.svg` | The product icon, the mark and the menu bar template, copied from the design system's Logos |
| `export-icon.sh` | Renders the product icon and the tray images into `resources/` with Chrome |

## Design artifacts

| Artifact | What it holds |
|---|---|
| [Oriel design system](https://claude.ai/artifact/LBUfZ9m6MHwCsXp4D1d97w) | The tokens of `tokens.json` and `layout.json` drawn, the brand book (principles, color, type, space and shape, layout, icons, logo, motion, voice), the components from level 1 to level 3 and Logos (the product icon, the mark and the menu bar template, the source of this folder's copies) |
| [Oriel 设计稿](https://claude.ai/artifact/M1YW6F7vwnQm2Ze8BJnYcb) | The canvas, in pages by level: `1 小组件`, `2 控件`, `3 大组件`, `4 页面` (one artboard per state of each screen), `5 桌面` (the window on macOS, Windows and Linux, the menu bar menu, the app menu, the startup failure), and `规范`, `配色` and `品牌` |

The artifacts are private to the maintainer until shared. The canvas draws each state in Simplified Chinese, in the
light scheme unless the artboard says dark, at the default window size unless it says narrow; English, the other
scheme and the minimum and wide sizes follow from the tokens and the rules in `docs/design.md`, and are checked in the
app.

## Screens

Each screen's artboards are on the `4 页面` page, titled `4 页面：<screen> <state>`.

| Screen | Artboards | Code |
|---|---|---|
| Connect | 连接服务端 | `internal/ui/connect/` |
| Timeline | 时间线, 社交, 视频, 图片, the empty states, 离线, 筛选订阅, 侧栏收起, 窄窗口 | `internal/ui/timeline/` |
| Reader | 阅读器, 沉浸阅读, 看图 | `internal/ui/reader/` |
| Subscriptions | 设置 订阅, 添加订阅, 编辑订阅, 确认退订 | `internal/ui/subscriptions/` |
| Settings | 设置 外观, 抓取, AI, 活动与存储, 导入 | `internal/ui/settings/` |

## Changing the UI

A UI change starts from the screens canvas; the maintainer confirms the visual direction before UI code is written.

1. Draw the change on the screens canvas (every state it touches, the longer translation if layout is at risk), and
   update `tokens.json`, `layout.json`, `tokens.md` and `docs/design.md` if a value or rule changes. A changed value
   is published to the design system too.
2. The maintainer confirms the visual direction.
3. Write the native view in `internal/ui/<screen>/` with theme roles and `Space`/`Rem` sizes, localized copy and
   accessible names, plus tests. On a screen that has moved to the tokens (#522), use only the token names.
4. Run `scripts/check.sh`, then the design acceptance checklist in `docs/design.md` on real devices. A headless test
   is not device acceptance.

Design notes and code go in the same pull request; the pull request body links the canvas artboards it follows and
carries the screenshots.

## Icons

Interface icons are Lucide, kept in `internal/ui/icons/lucide/`; the canvas uses the same SVG paths.

The logo is managed in the design system's **Logos** group, which holds three SVG files, each kept in this folder
under a shorter name:

| Logos | Here | What |
|---|---|---|
| `oriel-icon.svg` | `icon.svg` | The product icon: the mark on the ivory tile, on the macOS grid |
| `oriel-mark.svg` | `mark.svg` | The mark alone, in `logo-body` and `logo-piece` |
| `oriel-tray.svg` | `tray.svg` | The menu bar template: the mark in black on transparent |

A logo change is drawn and approved in the design system first, together with the `logo-*` tokens, and then copied
here as the same drawing. The copies here keep comments, and the asset store rewrites an SVG on upload (dropping
comments, expanding empty tags), so the two sides match in what they draw, not byte for byte. Upload the new copies
back to Logos when this side changes. After copying, run `design/export-icon.sh` and commit the three PNGs it writes.
The script needs Google Chrome, or a Chromium-based browser given as `CHROME`, and macOS's `sips`. It writes:

- `resources/icon.png`: `mygo build` makes every platform's icon from it.
- `resources/trayTemplate@2x.png`: the macOS menu bar.
- `resources/trayIcon.png`: the tray on Windows and Linux.

MyGo does not draw SVG filters, which is why the app uses PNGs.

Screenshots go in the pull request body, not into the repository; `design/screenshots/` is ignored by git.
