#!/usr/bin/env bash
# Local format, module, test and optional cross-platform checks. No publication.
set -euo pipefail
cd "$(dirname "$0")/.."
cross=0
race=
for arg in "$@"; do
  case "$arg" in
    --cross) cross=1 ;;
    --race) race=-race ;;
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
go test $race ./...
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
