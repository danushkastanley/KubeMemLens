#!/usr/bin/env python3

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent


class EnvironmentValidationTests(unittest.TestCase):
    def test_live_environment_must_match_selected_profile(self):
        fixture = json.loads((ROOT / "fixtures" / "gke-cos-pass.json").read_text(encoding="utf-8"))
        environment = {"schemaVersion": 1, **fixture["environment"]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "environment.json"
            path.write_text(json.dumps(environment), encoding="utf-8")
            command = [
                sys.executable,
                str(ROOT / "validate_environment.py"),
                "--profile",
                str(ROOT / "gke-cos-containerd-amd64.json"),
                "--environment",
                str(path),
            ]
            passed = subprocess.run(command, check=False, capture_output=True, text=True)
            environment["runtime"] = "cri-o://1.36.0"
            path.write_text(json.dumps(environment), encoding="utf-8")
            failed = subprocess.run(command, check=False, capture_output=True, text=True)
        self.assertEqual(passed.returncode, 0, passed.stderr)
        self.assertEqual(json.loads(passed.stdout)["result"], "pass")
        self.assertEqual(failed.returncode, 1, failed.stderr)
        self.assertIn("environment.runtime", failed.stdout)


if __name__ == "__main__":
    unittest.main()
