import importlib.util
import json
import pathlib
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("validate_evidence.py")
SPEC = importlib.util.spec_from_file_location("validate_evidence", MODULE_PATH)
VALIDATE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(VALIDATE)


class ValidateEvidenceTest(unittest.TestCase):
    def test_minimal_complete_bundle_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            bundle = pathlib.Path(directory)
            result = {
                "outcome": "passed",
                "run": {"requestedDurationSeconds": 1800},
                "checks": {"restored": True},
                "privacy": {
                    "rawTerminalOutputRetained": False,
                    "credentialPathsRetained": False,
                    "clusterIdentifiersRetained": False,
                },
                "terminalSizesCoveredByLivePTY": ["80x24", "120x30", "180x50"],
            }
            (bundle / "result.json").write_text(json.dumps(result), encoding="utf-8")
            png = b"\x89PNG\r\n\x1a\n" + b"x" * 5000
            (bundle / "frame.png").write_bytes(png)
            rows = []
            for row_id in sorted(VALIDATE.EXPECTED_ROWS):
                row = {"id": row_id, "outcome": "unqualified"}
                if row_id in VALIDATE.QUALIFIED_ROWS:
                    row.update({"outcome": "qualified", "evidence": ["result.json"]})
                if row_id in {"xterm-linux", "kitty-linux", "alacritty-linux"}:
                    row["sizes"] = ["80x24", "120x30", "180x50"]
                rows.append(row)
            matrix = {
                "schemaVersion": 1,
                "applicationSourceCommit": "a" * 40,
                "binaries": {"darwinArm64Sha256": "b" * 64, "linuxArm64Sha256": "c" * 64},
                "rows": rows,
                "ptySoakEvidence": "result.json",
                "emulatorSoakEvidence": "result.json",
                "liveSmokeEvidence": "result.json",
                "screenshots": {"80x24": "frame.png", "120x30": "frame.png", "180x50": "frame.png"},
            }
            (bundle / "terminal-matrix.json").write_text(json.dumps(matrix), encoding="utf-8")
            (bundle / "bundle.sha256").write_text(VALIDATE.bundle_digest(bundle) + "\n", encoding="ascii")
            self.assertEqual(VALIDATE.validate_bundle(bundle), matrix)

    def test_private_path_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "private.json"
            path.write_text('{"value":"/Users/operator/key"}', encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "private runtime data"):
                VALIDATE.load_json(path)


if __name__ == "__main__":
    unittest.main()
