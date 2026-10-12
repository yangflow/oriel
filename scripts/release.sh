#!/usr/bin/env bash
# Build local packages. This script never uploads or publishes.
set -euo pipefail
cd "$(dirname "$0")/.."
case "${1:-}" in
  --check) [[ $# -eq 1 ]] || exit 2; exec scripts/check.sh ;;
  '')
    go tool mygo build
    exec python3 scripts/scan_secrets.py --path build --forbid '/Users/' --forbid '/home/'
    ;;
  *) echo 'usage: scripts/release.sh [--check]' >&2; exit 2 ;;
esac
