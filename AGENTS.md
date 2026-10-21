# AGENTS.md

## What it is

Oriel is a personal mixed-timeline reader for RSS and RSSHub, written in pure Go with MyGo native UI (macOS 12+,
Windows 10/11, Linux with GTK 3; amd64 and arm64). One program is both the desktop app and its server; `oriel serve`
runs the server headless. MIT licensed.

Sources of truth:

- `docs/product.md`: product behavior, data model, flows. New rules go into the docs first, then into code.
- `docs/design.md`: the design language: layout, color, type, components, accessibility, localization.
- `design/tokens.md`: spacing, size and color values, mapped to MyGo theme roles.
- `design/`: the mockup of each screen and its states.
- GitHub milestones and issues: the plan and its order. `docs/roadmap.md` holds only the rules and finished history.
- `docs/release.md`: the release workflow, its secrets and the steps of a release.
- `docs/adr/0002-standalone-client-and-server.md`: the client reads and writes only through the HTTP API and never
  touches storage directly; without authentication configured the server listens on `127.0.0.1` only.

When docs and code disagree, decide which side is wrong: if the docs record a changed decision, fix the code; if
the docs still hold and the code quietly gave way, fix the code; fix the docs only when they would lead someone to
do the wrong thing.

## Layout

```
main.go                  dispatches desktop and headless (`oriel serve`) modes; lifecycle, menu, window, native updater
backend.go               opens the database, starts and stops the HTTP server; shared by both modes
internal/store/          SQLite and all queries; migrations in internal/store/migrations/
internal/fetch/          fetch loop, feed parsing, failure backoff, full-text extraction, retention pruning, proxied image fetch and its disk cache
internal/ai/             AI summaries and translations: settings, OpenAI-compatible calls, background queue
internal/tracker/        known tracking-picture addresses, shared by the reader and the image proxy
internal/server/         HTTP API and authentication
internal/ui/<screen>/    one package per screen: state, copy.json catalog, headless interaction tests
internal/ui/locale/      system language to UI language (Simplified Chinese, English), shared by all catalogs
internal/ui/startup/     copy for the native error dialog when desktop startup fails
resources/               shipped licenses and product assets
scripts/                 checks, packaging, secret scan
mygo.json                name, identifier, version, packaging
release-please-*.json    the release pull request: version and changelog from the merged titles
docs/, design/           product spec, design language, ADRs, mockups, tokens
```

## Commands

```sh
scripts/check.sh --cross     # gofmt, go mod tidy without diff, vet, tests, script tests, six cross-compiled targets
go test -race ./...
go tool mygo dev
go tool mygo build
ORIEL_PASSWORD=<password> go run . serve -data ./data -port 7470
```

## Conventions

- Only MyGo native views and the native updater plugin. No WebView, HTML frontend, cgo production dependency or
  frontend toolchain.
- No disk or network work on the drawing path. Run slow work in the background and change state back on the UI
  thread.
- Interface strings live in each screen package's `copy.json`, `en` and `zh-Hans` only; change both together. Do
  not add other languages.
- `internal/ui/<screen>/` matches `design/screens/<screen>.html`.
- The client only goes through `internal/server/`; UI code never imports `internal/store`.
- Migrations: only add new files under `internal/store/migrations/`; never edit a released one.
- Entry links are always fetched through `guardedClient` in `internal/fetch/`; never replace it with a plain
  `http.Client`.
- Releases: release-please keeps a release pull request open on `main` and computes the version from the merged
  titles, writing `mygo.json`, where the version lives only, and `CHANGELOG.md`; the `feat`, `fix` and `perf`
  titles become the release notes, so write them for users. Do not edit `CHANGELOG.md` or the version by hand; only
  the maintainer merges the release pull request, which tags the release (docs/release.md).
- The MyGo library and CLI are one module; update them together in `go.mod`. When dependencies change, update
  `THIRD_PARTY.md` and `resources/ThirdPartyLicenses/`.
- When behavior changes, update the existing tests for that area. Passing headless tests is not on-device
  acceptance.
- Do not split files just because they are long. Leave no temporary scripts, debug output or commented-out code.
- A UI change starts from the mockup in design/; the maintainer confirms the visual direction before UI code is
  written. Mockup, `docs/design.md` and code go in the same pull request; screenshots go in the pull request
  body, not into the repository.
- Keep `README.md` and `README.zh-CN.md` in sync; list only merged features, and mark plans as plans.
- Commit messages and PR titles use Conventional Commits:
  `feat|fix|docs|refactor|perf|test|build|ci|chore|revert(scope)!: subject`. Branches are `<type>/<issue>-<slug>`;
  pull requests are squash-merged. Commit with the GitHub noreply address
  `269347247+yangflow@users.noreply.github.com` (set `git config user.email` after cloning).

## Issues and pull requests

`gh issue create` and `gh pr create --body` skip the issue forms and the pull request template, so write their
structure yourself. Search open issues first; one issue is one problem.
Work starts from an issue in a milestone; a new idea gets an issue first and does not join the milestone in progress
unless the maintainer moves it there.

- Issue title: one short sentence that says the problem or the outcome, in English or Simplified Chinese. No type
  prefix such as `[feat]`; the label carries the type.
- Bug, `--label bug`: `## What happened`, `## What you expected`, `## Steps to reproduce`,
  `## Version and environment`.
- Feature or improvement, `--label enhancement`: `## Problem`, `## Proposed solution`,
  `## Alternatives considered` (when there were any), `## Done when`.
- Documentation, `--label documentation`: `## Where`, `## What is missing, unclear or wrong`, `## Suggested change`.
- Labels: `ui` marks work on a screen or a mockup, together with `enhancement` or `bug`; `accessibility` marks
  screen-reader, keyboard and contrast work.
- Pull request: the title is the Conventional Commits subject (it becomes the squash commit). The body has
  `## Summary` (what changes and why), `## Related issue` (`Closes #<n>`), `## How tested` (the commands run and
  their results, and what was not run) and `## Checklist`:
  `- [ ] PR title follows Conventional Commits`, `- [ ] Tests pass`,
  `- [ ] Documentation updated where behavior changed`, ticked only when true.

## Red lines

- Credentials (passwords, tokens, API keys, signing keys) never appear in code, PRs, issues, logs, screenshots or
  chat. `scripts/scan_secrets.py` scans the whole history in CI.
- Signing, releases and anything sent to an external service need the maintainer's confirmation.
- No blind `git add -A`; review the staged file list before committing.
- Personal e-mail addresses, local user names and local paths never go into commits, files or logs.
