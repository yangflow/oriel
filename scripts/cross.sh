#!/usr/bin/env bash
# Compiles the program for the six release targets, without cgo, and throws the binaries away. The one list of targets:
# check.sh --cross and the `cross` job of ci.yml both run this.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
for target in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
  echo "Compiling $target"
  CGO_ENABLED=0 GOOS="${target%/*}" GOARCH="${target#*/}" go build -trimpath -o "$work/${target//\//-}" .
done
