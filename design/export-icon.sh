#!/usr/bin/env bash
# Renders design/icon.svg to resources/icon.png, 1024 x 1024 on transparent, with headless Chrome: MyGo draws no SVG
# filters, and mygo build makes every platform's icon from that PNG. Set CHROME to the browser when it is elsewhere.
set -euo pipefail
cd "$(dirname "$0")/.."
chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
[[ -x "$chrome" ]] || { echo "Chrome not found; set CHROME to its executable." >&2; exit 1; }
"$chrome" --headless=new --hide-scrollbars --default-background-color=00000000 --force-device-scale-factor=1 \
  --window-size=1024,1024 --screenshot="$PWD/resources/icon.png" "file://$PWD/design/icon.svg" >/dev/null 2>&1
python3 - <<'PY'
import struct
with open('resources/icon.png', 'rb') as f:
    head = f.read(26)
width, height = struct.unpack('>II', head[16:24])
assert head[:8] == b'\x89PNG\r\n\x1a\n' and (width, height) == (1024, 1024), (width, height)
assert head[25] == 6, 'expected RGBA'
PY
echo "Wrote resources/icon.png"
