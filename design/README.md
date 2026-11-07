# Design

Design values and the product icon for Oriel's native UI. The design language is
[docs/design.md](../docs/design.md); the values it uses are in [tokens.json](tokens.json) and
[layout.json](layout.json), explained in [tokens.md](tokens.md). The screens themselves are drawn outside the
repository, in two claude.ai Design artifacts kept by the maintainer. Mockups approximate the result; the app running
on a real device is what counts.

## Layout

| Path | Purpose |
|---|---|
| `tokens.json` | Every design value: colors in layers, the contrast audit, space, sizes, radii, text styles, the reading scale, motion; the one source and the target the code moves to (#522) |
| `layout.json` | Named layout patterns and the geometry of each part |
| `tokens.md` | Why the values are what they are, mapped to MyGo theme roles |
| `icon.svg`, `mark.svg`, `tray.svg` | The product icon, the mark and the menu bar template, copied from the design system's Logos |
| `export-icon.sh` | Renders the product icon and the tray images into `resources/` with Chrome |

## Design artifacts

| Artifact | What it holds |
|---|---|
| [Oriel design system](https://claude.ai/artifact/LBUfZ9m6MHwCsXp4D1d97w) | The tokens of `tokens.json` drawn, the brand book (principles, color layers, type, space, shape, layout, icons, motion, voice), the core components and the logo (Logos: the product icon, the mark and the menu bar template, the source of this folder's copies) |
| [Oriel screens](https://claude.ai/artifact/MVcNGTHthRZ9q4Ac8PGsWZ) | The screens canvas: one page per screen (`connect`, `timeline`, `subscriptions`, `reader`, `settings`) and one artboard per state, named like the state in `docs/design.md` |

The artifacts are private to the maintainer until shared. The canvas draws each state in the light scheme, in English,
at the default window size; dark, Simplified Chinese and the minimum and wide sizes follow from the tokens and the
rules in `docs/design.md`, and are checked in the app. The startup failure message has no mockup; it is a native
system error dialog (`internal/ui/startup/`).

## Screens

| Screen | Canvas page | Code |
|---|---|---|
| Connect | `connect` | `internal/ui/connect/` |
| Timeline | `timeline` | `internal/ui/timeline/` |
| Subscriptions | `subscriptions` | `internal/ui/subscriptions/` |
| Reader | `reader` | `internal/ui/reader/` |
| Settings | `settings` | `internal/ui/settings/` |

A canvas page maps to a package: the `<screen>` page is implemented by `internal/ui/<screen>/`. The text and states on
the page are the strings in that package's `copy.json` (English and Simplified Chinese) and the states its `Screen`
can be in; the test in the package covers each state.

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
