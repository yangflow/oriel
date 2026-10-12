import json
import shutil
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ScanTest(unittest.TestCase):
    def test_artifact_secret_is_redacted(self):
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "artifact"
            value = "gh" + "p_" + "A1b2C3d4" * 5
            artifact.write_text(value)
            r = subprocess.run(["python3", str(ROOT / "scripts/scan_secrets.py"), "--path", str(artifact)],
                               text=True, capture_output=True)
            self.assertEqual(r.returncode, 1, r.stderr)
            self.assertIn("github-token", r.stdout)
            self.assertNotIn(value, r.stdout + r.stderr)

    def test_public_key_does_not_hide_another_secret(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "scripts").mkdir()
            scanner = root / "scripts/scan_secrets.py"
            shutil.copyfile(ROOT / "scripts/scan_secrets.py", scanner)
            public = "AbCdEfGh" * 5 + "abc="
            (root / "mygo.json").write_text(json.dumps({"updates": {"publicKey": public}}))
            artifact = root / "artifact"
            artifact.write_text(public + " gh" + "p_" + "A1b2C3d4" * 5)
            r = subprocess.run(["python3", str(scanner), "--path", str(artifact)], text=True, capture_output=True)
            self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
            self.assertIn("github-token", r.stdout)

    def test_build_path_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "artifact"
            artifact.write_text("/Users/Builder/source/main.go")
            r = subprocess.run(["python3", str(ROOT / "scripts/scan_secrets.py"), "--path", str(artifact),
                                "--forbid", "/Users/"], text=True, capture_output=True)
            self.assertEqual(r.returncode, 1, r.stderr)
            self.assertIn("forbidden string", r.stdout)

    def test_missing_artifact_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = subprocess.run(["python3", str(ROOT / "scripts/scan_secrets.py"), "--path", str(Path(tmp) / "missing")],
                               text=True, capture_output=True)
            self.assertNotEqual(r.returncode, 0)

    def test_clean_artifact_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "artifact"
            artifact.write_text("ordinary native app content")
            r = subprocess.run(["python3", str(ROOT / "scripts/scan_secrets.py"), "--path", str(artifact)],
                               text=True, capture_output=True)
            self.assertEqual(r.returncode, 0, r.stderr)
