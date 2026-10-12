# Contributing

Thanks for helping. Oriel is a single-user native reader, mostly built by the maintainer working with AI coding
agents; issues and pull requests from anyone are welcome. Read [AGENTS.md](AGENTS.md) for the full set of
repository rules.

## Languages

Issues, pull requests, commit messages and reviews may be in English or Simplified Chinese. Replies follow the
language of the thread.

## Before you start

- Search existing issues and pull requests first.
- Open an issue before a large change so the direction can be agreed before you write code. Small fixes can go
  straight to a pull request.
- The scope is in [docs/product.md](docs/product.md) (no multiple users, plugins or recommendations). A pull
  request outside it may be closed.
- Questions go to issues; there are no Discussions and no paid support.
- Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md).

## Development setup

Requirements: the Go version in `.go-version` and Python 3 for the helper scripts. No Node, Bun or WebView.

```bash
scripts/check.sh --cross     # gofmt, go mod tidy without diff, vet, tests, script tests, six cross-compiled targets
go test -race ./...
go tool mygo dev             # run the desktop app
ORIEL_PASSWORD=<password> go run . serve -data ./data -port 7470   # run headless
```

`scripts/check.sh --cross` and `go test -race ./...` must pass before merge.

## Commits, branches and pull requests

- Commit messages and pull request titles follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat|fix|docs|refactor|perf|test|build|ci|chore|revert(scope)!: subject`. The `pr-title` check enforces it.
- Name branches `<type>/<issue>-<slug>`, for example `feat/12-timeline`.
- One concern per pull request. Link the issue with `Closes #<n>`.
- The maintainer squash-merges, so the pull request title becomes the commit message. No DCO or sign-off is
  required.
- When behavior changes, update the existing tests for that area. Add user-visible changes under
  `## [Unreleased]` in `CHANGELOG.md`.
- Interface strings live in each screen's `copy.json`, in English and Simplified Chinese only; change both.
  Product behavior changes start in [docs/product.md](docs/product.md).

## UI changes

- A UI change starts from the mockup in `design/` and the design language in [docs/design.md](docs/design.md); the
  maintainer confirms the visual direction before UI code is written. Mockup, design notes and code go in the same
  pull request.
- Use only MyGo native controls and theme colors. If a component is missing, specify it and all its states in the
  docs first.
- Screenshots go in the pull request body (light and dark, one per state), not into the repository.

## AI-assisted changes

AI tools are fine. Say in the pull request which one you used and for what. You are responsible for every line
you submit: read it, understand it and run it.

## License

Contributions are licensed under the [MIT License](LICENSE).
