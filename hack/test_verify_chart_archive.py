#!/usr/bin/env python3

import io
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

from verify_chart_archive import ArchiveError, verify_archive  # noqa: E402


class ChartArchiveTests(unittest.TestCase):
    def make_archive(self, root, entries):
        archive = root / "chart.tgz"
        with tarfile.open(archive, "w:gz") as destination:
            for name, content in entries:
                info = tarfile.TarInfo(name)
                info.size = len(content)
                destination.addfile(info, io.BytesIO(content))
        return archive

    def test_exact_archive_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            chart = root / "chart"
            chart.mkdir()
            (chart / "Chart.yaml").write_text("name: chart\n", encoding="utf-8")
            (chart / "values.yaml").write_text("{}\n", encoding="utf-8")
            archive = self.make_archive(root, [
                ("chart/Chart.yaml", b"name: chart\n"),
                ("chart/values.yaml", b"{}\n"),
            ])
            verify_archive(archive, chart)

    def test_content_file_set_and_unsafe_paths_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            chart = root / "chart"
            chart.mkdir()
            (chart / "Chart.yaml").write_text("name: chart\n", encoding="utf-8")
            (chart / "values.yaml").write_text("{}\n", encoding="utf-8")
            cases = (
                [("chart/Chart.yaml", b"name: chart\n"), ("chart/values.yaml", b"changed\n")],
                [("chart/values.yaml", b"{}\n")],
                [("chart/../outside", b"unsafe")],
            )
            for entries in cases:
                with self.subTest(entries=entries), self.assertRaises(ArchiveError):
                    verify_archive(self.make_archive(root, entries), chart)


if __name__ == "__main__":
    unittest.main()
