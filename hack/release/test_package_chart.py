from __future__ import annotations

import hashlib
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PACKAGER = ROOT / "hack/release/package_chart.py"
CHART = ROOT / "charts/kube-memlens"


class PackageChartTests(unittest.TestCase):
    def test_identical_inputs_produce_identical_archives(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output_one = Path(temporary) / "one.tgz"
            output_two = Path(temporary) / "two.tgz"
            version = self._field("version")
            app_version = self._field("appVersion")

            for output in (output_one, output_two):
                subprocess.run(
                    [
                        "python3",
                        str(PACKAGER),
                        str(CHART),
                        str(output),
                        "--version",
                        version,
                        "--app-version",
                        app_version,
                        "--epoch",
                        "1704067200",
                    ],
                    check=True,
                )

            self.assertEqual(self._sha256(output_one), self._sha256(output_two))
            with tarfile.open(output_one, "r:gz") as archive:
                members = archive.getmembers()
                self.assertTrue(members)
                self.assertTrue(all(member.mtime == 1704067200 for member in members))
                self.assertTrue(all(member.uid == 0 and member.gid == 0 for member in members))

    def test_rejects_version_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            result = subprocess.run(
                [
                    "python3",
                    str(PACKAGER),
                    str(CHART),
                    str(Path(temporary) / "chart.tgz"),
                    "--version",
                    "9.9.9",
                    "--app-version",
                    self._field("appVersion"),
                    "--epoch",
                    "1704067200",
                ],
                capture_output=True,
                check=False,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("expected 9.9.9", result.stderr)

    @staticmethod
    def _sha256(path: Path) -> str:
        return hashlib.sha256(path.read_bytes()).hexdigest()

    @staticmethod
    def _field(name: str) -> str:
        for line in (CHART / "Chart.yaml").read_text(encoding="utf-8").splitlines():
            key, separator, value = line.partition(":")
            if separator and key == name:
                return value.strip().strip('"')
        raise AssertionError(f"missing Chart.yaml field: {name}")


if __name__ == "__main__":
    unittest.main()
