# Design

HTML mockups of Oriel's screens. They approximate the result; the app running on a real device is what counts.

## What is here

- `screens/<screen>.html`: one mockup per screen, matching the package `internal/ui/<screen>/`. Today there is only
  `example.html`, the template's counter example, which goes away when the first product screen lands.
- The startup failure message has no mockup; it is a native system error dialog (`internal/ui/startup/`).
- Colors, type, spacing and components are specified in [docs/design-system.md](../docs/design-system.md).

## Preview

Open a mockup in a browser. States and color schemes are switched with URL parameters, for example
`screens/example.html?state=zero|positive|negative&scheme=light|dark`. New mockups use URL parameters the same way,
so every state can be seen.

## Icons

There are no icon sources yet; the product icon is still to be designed. When it lands, its source and the script
that exports it will live here.

Screenshots go in the pull request body, not into the repository; `design/screenshots/` is ignored by git.
