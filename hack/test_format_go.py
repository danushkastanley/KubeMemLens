import sys
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("format_go.py")


class FormatGoTests(unittest.TestCase):
    def test_formats_tracked_and_new_sources_without_changing_frozen_cache(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", directory], check=True)
            (root / ".gitignore").write_text(".sdk/\n")
            original = "package fixture\nfunc sample( ) {}\n"
            (root / "tracked.go").write_text(original)
            (root / "deleted.go").write_text(original)
            subprocess.run(["git", "-C", directory, "add", "."], check=True)
            (root / "deleted.go").unlink()
            (root / "new file.go").write_text(original)
            (root / ".sdk").mkdir()
            frozen = root / ".sdk/upstream.go"
            frozen.write_text(original)
            command = [sys.executable, str(SCRIPT)]
            checked = subprocess.run(command, cwd=root, capture_output=True, text=True)
            self.assertEqual(checked.returncode, 1)
            self.assertEqual(set(checked.stdout.splitlines()), {"tracked.go", "new file.go"})
            subprocess.run(command + ["--write"], cwd=root, check=True)
            subprocess.run(command, cwd=root, check=True)
            self.assertEqual(frozen.read_text(), original)
            self.assertNotEqual((root / "tracked.go").read_text(), original)
            self.assertNotEqual((root / "new file.go").read_text(), original)
            (root / "malformed.go").write_text("package\n")
            malformed = subprocess.run(command, cwd=root, capture_output=True, text=True)
            self.assertNotEqual(malformed.returncode, 0)
            self.assertIn("malformed.go", malformed.stderr)


if __name__ == "__main__":
    unittest.main()
