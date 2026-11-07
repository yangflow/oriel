#!/usr/bin/env bash
# Renders design/icon.svg to resources/icon.png, 1024 x 1024 on transparent, with headless Chrome: MyGo draws no SVG
# filters, and mygo build makes every platform's icon from that PNG. It also renders the tray images: design/tray.svg
# to resources/trayTemplate@2x.png (36 x 36, the macOS menu bar template) and the icon scaled to
# resources/trayIcon.png (64 x 64, Windows and Linux), which needs macOS's sips. Set CHROME to the browser when it is
# elsewhere.
set -euo pipefail
cd "$(dirname "$0")/.."
chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
[[ -x "$chrome" ]] || { echo "Chrome not found; set CHROME to its executable." >&2; exit 1; }
command -v sips >/dev/null || { echo "sips not found; it ships with macOS." >&2; exit 1; }
render() { # source, output, size
  "$chrome" --headless=new --hide-scrollbars --default-background-color=00000000 --force-device-scale-factor=1 \
    --window-size="$3,$3" --screenshot="$PWD/$2" "file://$PWD/$1" >/dev/null 2>&1
}
render design/icon.svg resources/icon.png 1024
render design/tray.svg resources/trayTemplate@2x.png 36
sips -z 64 64 resources/icon.png --out resources/trayIcon.png >/dev/null
python3 - <<'PY'
import struct
for path, size in (('resources/icon.png', 1024), ('resources/trayTemplate@2x.png', 36), ('resources/trayIcon.png', 64)):
    with open(path, 'rb') as f:
        head = f.read(26)
    width, height = struct.unpack('>II', head[16:24])
    assert head[:8] == b'\x89PNG\r\n\x1a\n' and (width, height) == (size, size), (path, width, height)
    assert head[25] == 6, f'{path}: expected RGBA'
PY
echo "Wrote resources/icon.png, resources/trayTemplate@2x.png and resources/trayIcon.png"
