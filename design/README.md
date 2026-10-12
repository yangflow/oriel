# Design

Mockups and design notes for Oriel's native UI. The design language is [docs/design.md](../docs/design.md); the
values it uses are in [tokens.md](tokens.md). Mockups approximate the result; the app running on a real device is
what counts.

## Layout

| Path | Purpose |
|---|---|
| `screens/<screen>.html` | One self-contained mockup per screen: static HTML and CSS, no external resources |
| `screens/example-appearance.html` | The example screen side by side in light and dark, with focus rings |
| `tokens.md` | Spacing, size, color and motion values, mapped to MyGo theme roles |

Today the only screen is the counter example (`screens/example.html`), which goes away when the first product screen
lands. The startup failure message has no mockup; it is a native system error dialog (`internal/ui/startup/`).

## Preview

Open a file in `screens/` in a browser. States and color schemes are switched with URL parameters, for example
`screens/example.html?state=zero|positive|negative&scheme=light|dark`. New mockups use URL parameters the same way,
so every state can be seen. `screens/example-appearance.html` needs no parameters.

## Screens

| Screen | Design file | Code |
|---|---|---|
| Example | `screens/example.html`, `screens/example-appearance.html` | `internal/ui/example/` |

A mockup maps to a package: `screens/<screen>.html` is implemented by `internal/ui/<screen>/`. The text and states in
the mockup are the strings in that package's `copy.json` (English and Simplified Chinese) and the states its `Screen`
can be in; the test in the package covers each state.

## Changing the UI

A UI change starts from the mockup in design/; the maintainer confirms the visual direction before UI code is
written.

1. Edit or add the mockup in `screens/` (light and dark, every state, the longer translation if layout is at risk),
   and update `tokens.md` and `docs/design.md` if a rule changes.
2. The maintainer confirms the visual direction.
3. Write the native view in `internal/ui/<screen>/` with theme roles and `Space`/`Rem` sizes, localized copy and
   accessible names, plus tests.
4. Run `scripts/check.sh`, then the design acceptance checklist in `docs/design.md` on real devices. A headless test
   is not device acceptance.

Mockup, design notes and code go in the same pull request.

## Icons

There are no icon sources yet; the product icon is still to be designed. When it lands, its source and the script
that exports it will live here.

Screenshots go in the pull request body, not into the repository; `design/screenshots/` is ignored by git.
