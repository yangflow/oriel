# Design

Mockups and design notes for Oriel's native UI. The design language is [docs/design.md](../docs/design.md); the
values it uses are in [tokens.md](tokens.md). Mockups approximate the result; the app running on a real device is
what counts.

## Layout

| Path | Purpose |
|---|---|
| `screens/<screen>.html` | One self-contained mockup per screen: static HTML and CSS, no external resources |
| `tokens.md` | Spacing, size, color and motion values, mapped to MyGo theme roles |

The connect screen (`screens/connect.html`), the timeline (`screens/timeline.html`, home), the subscription dialogs
over it (`screens/subscriptions.html`), the reader (`screens/reader.html`) and the settings (`screens/settings.html`)
are the product screens so far. The startup failure message has no mockup; it is a native system error dialog
(`internal/ui/startup/`).

## Preview

Open a file in `screens/` in a browser. States and color schemes are switched with URL parameters, for example
`screens/timeline.html?state=list&scope=video&scheme=dark`. New mockups use URL parameters the same way, so every
state can be seen.

## Screens

| Screen | Design file | Code |
|---|---|---|
| Connect | `screens/connect.html` (`?state=…&scheme=light|dark&lang=en|zh-Hans&size=default|min`) | `internal/ui/connect/` |
| Timeline | `screens/timeline.html` (`?state=…&scope=all|article|video|social|image|source&scheme=light|dark&lang=en|zh-Hans&size=default|min`) | `internal/ui/timeline/` |
| Subscriptions | `screens/subscriptions.html` (`?state=…&scheme=light|dark&lang=en|zh-Hans&size=default|min`) | `internal/ui/subscriptions/` |
| Reader | `screens/reader.html` (`?state=full|feed|extracting|extracting-empty|failed|loading|error|summarizing|summary|summary-failed|translating|translation|translation-failed|ai-off&scheme=light|dark&lang=en|zh-Hans&size=default|min`) | `internal/ui/reader/` |
| Settings | `screens/settings.html` (`?state=fetching|edited|invalid|saved|opml|importing|imported|import-error|exported|devices|devices-local|revoke|ai|ai-configured|ai-edited|ai-invalid|ai-testing|ai-tested|ai-test-failed|loading|error&scheme=light|dark&lang=en|zh-Hans&size=default|min`) | `internal/ui/settings/` |

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

Interface icons are Lucide, kept in `internal/ui/icons/lucide/`; mockups inline the same SVG paths. The product icon
is still to be designed. When it lands, its source and the script that exports it will live here.

Screenshots go in the pull request body, not into the repository; `design/screenshots/` is ignored by git.
