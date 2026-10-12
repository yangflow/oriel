import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class BumpTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / "scripts").mkdir()
        shutil.copy(ROOT / "scripts/bump.py", self.root / "scripts/bump.py")
        (self.root / "mygo.json").write_text('{"name":"Probe","version":"0.1.0"}\n')
        for name in ("CHANGELOG.md", "CHANGELOG.zh.md"):
            (self.root / name).write_text("# Changes\n\n## [Unreleased]\n\n### Added\n\n- Example.\n")

    def test_bump_preserves_notes(self):
        r = subprocess.run(["python3", "scripts/bump.py", "1.0.0"], cwd=self.root, capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(json.loads((self.root / "mygo.json").read_text())["version"], "1.0.0")
        for name in ("CHANGELOG.md", "CHANGELOG.zh.md"):
            text = (self.root / name).read_text()
            self.assertIn("## [Unreleased]\n\n## [1.0.0] - ", text)
            self.assertIn("- Example.", text)

    def test_bump_without_system_timezone_database(self):
        r = subprocess.run(["python3", "-S", "scripts/bump.py", "1.0.0"], cwd=self.root,
                           env={**os.environ, "PYTHONTZPATH": ""}, capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_invalid_or_lower_versions_leave_files_unchanged(self):
        paths = [self.root / name for name in ("mygo.json", "CHANGELOG.md", "CHANGELOG.zh.md")]
        before = [p.read_bytes() for p in paths]
        for version in ("0.0.9", "0.1.0", "01.0.0", "1.0", "v1.0.0"):
            r = subprocess.run(["python3", "scripts/bump.py", version], cwd=self.root, capture_output=True)
            self.assertNotEqual(r.returncode, 0)
            self.assertEqual([p.read_bytes() for p in paths], before)

    def test_missing_unreleased_is_rejected(self):
        (self.root / "CHANGELOG.zh.md").write_text("# Changes\n")
        before = (self.root / "mygo.json").read_bytes()
        r = subprocess.run(["python3", "scripts/bump.py", "1.0.0"], cwd=self.root, capture_output=True)
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual((self.root / "mygo.json").read_bytes(), before)
