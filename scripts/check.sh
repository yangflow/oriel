#!/usr/bin/env bash
# Local format, module, test and optional cross-platform checks. No publication.
#
# The default is what a pull request runs (the `check` job of ci.yml). The race detector makes the UI screen packages
# about 18 times slower (ui/timeline 277 s against 15 s), so a pull request runs it only on the packages that start
# goroutines or share state between them; the others are plain tests. --race runs it on everything, and ci.yml does
# that on every push to main and daily. --cross adds the six cross-compiled targets, run there as well.
set -euo pipefail
cd "$(dirname "$0")/.."
# The one list of packages tested with -race by default: the root package (server lifecycle, desktop host), the fetch
# loop, the store, the HTTP server, the AI queue, the diagnostics log, the picture queue and the backup runner. Add a
# package here when it starts goroutines or takes a lock.
race_packages=(
  .
  ./internal/ai
  ./internal/autobackup
  ./internal/diag
  ./internal/fetch
  ./internal/server
  ./internal/store
  ./internal/ui/pictures
)
cross=0
race_all=0
for arg in "$@"; do
  case "$arg" in
    --cross) cross=1 ;;
    --race) race_all=1 ;;
    *) echo 'usage: scripts/check.sh [--cross] [--race]' >&2; exit 2 ;;
  esac
done
[[ -z "$(gofmt -l .)" ]] || { echo 'Run gofmt on the Go sources.' >&2; exit 1; }
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp go.mod go.sum "$work/"
go mod tidy
diff -u "$work/go.mod" go.mod
diff -u "$work/go.sum" go.sum
[[ "$(cat .go-version)" == "$(awk '/^go / { print $2 }' go.mod)" ]]
go vet ./...
if [[ "$race_all" == 1 ]]; then
  go test -race ./...
else
  go test ./...
  go test -race "${race_packages[@]}"
fi
# Installed copies refuse a tampered update, one signed with another key and plain HTTP: MyGo's own tests, run
# against the version in go.mod.
go test -run '^(TestSignatures|TestUpdater)$' github.com/egoist/mygo github.com/egoist/mygo/internal/update
python3 -m unittest discover -s scripts -p 'test_*.py'
if [[ "$cross" == 1 ]]; then
  for target in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
    echo "Compiling $target"
    CGO_ENABLED=0 GOOS="${target%/*}" GOARCH="${target#*/}" go build -trimpath -o "$work/${target//\//-}" .
  done
fi
