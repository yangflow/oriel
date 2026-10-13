#!/usr/bin/env python3
"""Scan the whole git history (and optionally a build artifact) for credential-like material.

    python3 scripts/scan_secrets.py                 # every added line in every commit of every ref
    python3 scripts/scan_secrets.py --path build/release/Oriel.app --forbid "$HOME"   # a built app

Matches are never printed in full: only commit, file, line and a redacted length and fingerprint, so the report itself
cannot leak a secret. Exit status 1 when anything is found. This is a pattern scan, not a proof that
no secret exists: a clean result is not a statement that the project is secure.

The update public key is read from mygo.json; private keys are never allowed.
"""

from __future__ import annotations

import argparse
import hashlib
import math
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]



# An update verification key is public and is intentionally embedded in the app.
config_file = ROOT / "mygo.json"
config = json.loads(config_file.read_text()) if config_file.is_file() else {}
public_key = (config.get("updates") or {}).get("publicKey", "")
ALLOW_SUBSTRINGS = (public_key,) if public_key else ()
ALLOW_PATHS = ()

PATTERNS: dict[str, re.Pattern[str]] = {
    "github-token": re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})\b"),
    "gitlab-token": re.compile(r"\bglpat-[A-Za-z0-9_\-]{18,}\b"),
    "gitea-or-generic-hex-token": re.compile(r"(?i)\b(?:token|secret|password|passwd|api[_-]?key)\b\s*[:=]\s*[\"']?[0-9a-f]{40}\b"),
    "cloudflare-token": re.compile(r"\bcf(?:ut|at|k)_[A-Za-z0-9]{20,}\b"),
    "dodo-api-key": re.compile(r"\b[A-Za-z0-9]{16}\.[A-Za-z0-9_\-]{40,}\b"),
    "aws-access-key": re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
    "slack-token": re.compile(r"\bxox[abprs]-[A-Za-z0-9-]{10,}\b"),
    "private-key-block": re.compile(r"-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY"),
    "credential-in-url": re.compile(r"https?://[^/\s:@]+:[^/\s@]{6,}@"),
    "assigned-secret": re.compile(
        r"(?i)\b(?:secret|password|passwd|token|api[_-]?key|access[_-]?key)[A-Za-z_]*\b\s*[:=]\s*[\"']([A-Za-z0-9+/=_\-]{24,})[\"']"
    ),
    "base64-private-key-like": re.compile(r"(?i)ed25519[^\n]{0,40}[\"']([A-Za-z0-9+/]{43}=)[\"']"),
}
# Personal data worth knowing about before a repository leaves private (reported separately, lower severity).
PERSONAL = {
    "home-path": re.compile(r"/Users/(?!Shared\b|<)[A-Za-z0-9._-]+/"),
    "email": re.compile(r"\b[A-Za-z0-9._%+-]+@(?!example\.|users\.noreply\.github\.com)[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b"),
}


def entropy(text: str) -> float:
    if not text:
        return 0.0
    counts = {c: text.count(c) for c in set(text)}
    return -sum(n / len(text) * math.log2(n / len(text)) for n in counts.values())


def redact(value: str) -> str:
    """Length and a short SHA-256 fingerprint only: no character of the value is ever shown."""
    return f"<redacted {len(value)} chars, sha256:{hashlib.sha256(value.encode()).hexdigest()[:8]}>"


def scan_line(line: str) -> list[tuple[str, str]]:
    for allowed in ALLOW_SUBSTRINGS:
        line = line.replace(allowed, "[public-key]")
    hits = []
    for name, pattern in PATTERNS.items():
        match = pattern.search(line)
        if not match:
            continue
        value = match.group(1) if match.groups() else match.group(0)
        if name == "assigned-secret" and entropy(value) < 3.5:
            continue  # placeholders such as "xxxxxxxx…"
        hits.append((name, redact(value)))
    return hits


def scan_history() -> tuple[list[str], list[str]]:
    findings: list[str] = []
    personal: dict[str, set[str]] = {}
    proc = subprocess.Popen(
        ["git", "log", "--all", "-p", "--no-color", "--format=@@@%h", "-U0", "--no-ext-diff", "--text"],
        cwd=ROOT, stdout=subprocess.PIPE, text=True, errors="replace",
    )
    commit, path, line_no = "", "", 0
    assert proc.stdout is not None
    for raw in proc.stdout:
        line = raw.rstrip("\n")
        if line.startswith("@@@"):
            commit = line[3:]
        elif line.startswith("+++ b/"):
            path = line[6:]
        elif line.startswith("@@"):
            m = re.search(r"\+(\d+)", line)
            line_no = int(m.group(1)) - 1 if m else 0
        elif line.startswith("+") and not line.startswith("+++"):
            line_no += 1
            for name, shown in scan_line(line[1:]):
                if path.startswith(ALLOW_PATHS):
                    continue
                findings.append(f"{commit} {path}:{line_no} {name} {shown}")
            for name, pattern in PERSONAL.items():
                for m in pattern.finditer(line[1:]):
                    personal.setdefault(name, set()).add(f"{path}")
    if proc.wait() != 0:
        raise RuntimeError("git history scan failed")
    summary = [f"{name}: {len(files)} file(s): " + ", ".join(sorted(files)[:6]) + ("…" if len(files) > 6 else "")
               for name, files in sorted(personal.items())]
    return sorted(set(findings)), summary


def printable_strings(data: bytes) -> list[str]:
    return [chunk.decode("ascii") for chunk in re.findall(rb"[\x20-\x7e]{8,}", data)]


def scan_path(target: Path, forbidden: list[str]) -> list[str]:
    """Credential-like strings and forbidden substrings (build machine paths, demo code) in an artifact."""
    findings: list[str] = []
    files = [target] if target.is_file() else [p for p in target.rglob("*") if p.is_file() and not p.is_symlink()]
    for file in files:
        name = str(file.relative_to(target)) if target.is_dir() else file.name
        data = file.read_bytes()
        for index, line in enumerate(printable_strings(data), 1):
            for kind, shown in scan_line(line):
                findings.append(f"{name} string#{index} {kind} {shown}")
        for needle in forbidden:
            if needle.encode() in data:
                findings.append(f"{name}: contains forbidden string {needle!r}")
    return sorted(set(findings))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--path", type=Path, help="scan the printable strings of a built artifact instead of history")
    parser.add_argument("--forbid", action="append", default=[], metavar="TEXT",
                        help="with --path: also fail when this text appears anywhere in the artifact (repeatable)")
    args = parser.parse_args()
    if args.path:
        if not args.path.exists():
            parser.error("artifact path does not exist")
        findings, notes = scan_path(args.path, args.forbid), []
        label = f"artifact {args.path}"
    else:
        findings, notes = scan_history()
        label = "git history (all refs)"
    print(f"Scanned {label}.")
    for item in notes:
        print("note:", item)
    if findings:
        print(f"FOUND {len(findings)} credential-like match(es) (values redacted):")
        for item in findings:
            print(" ", item)
        return 1
    print("No credential-like patterns found (pattern scan only).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
