#!/usr/bin/env python3
"""Advance the single app version and date the unreleased notes.

The current version may be given again while CHANGELOG.md has no section for it, so the first release dates its
notes without skipping a version."""
import argparse
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import re

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("version")
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
pattern = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
if not re.fullmatch(pattern, args.version):
    parser.error("version must be X.Y.Z without a prefix or leading zeros")
config = json.loads((root / "mygo.json").read_text())
text = (root / "CHANGELOG.md").read_text()
if text.count("\n## [Unreleased]\n") != 1:
    parser.error("CHANGELOG.md must have one Unreleased section")
if f"\n## [{args.version}]" in text:
    parser.error("CHANGELOG.md already contains this version")
if tuple(map(int, args.version.split("."))) < tuple(map(int, config["version"].split("."))):
    parser.error("version must not decrease")
changes = {}
date = datetime.now(timezone(timedelta(hours=8))).date().isoformat()
changes[root / "CHANGELOG.md"] = text.replace("\n## [Unreleased]\n", f"\n## [Unreleased]\n\n## [{args.version}] - {date}\n", 1)
config["version"] = args.version
changes[root / "mygo.json"] = json.dumps(config, indent=2, ensure_ascii=False) + "\n"
for path, text in changes.items():
    path.write_text(text)
print(f"Version {args.version}. Review and commit the two changed files before tagging v{args.version}.")
