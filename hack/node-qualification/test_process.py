import sys
import time
import unittest

from common import ContractError
from process import execute


class BoundedProcessTest(unittest.TestCase):
    def test_input_and_output_are_exact(self):
        value = execute([sys.executable, "-c", "import sys;sys.stdout.buffer.write(sys.stdin.buffer.read())"], data=b"fixture")
        self.assertEqual(value, "fixture")

    def test_large_stdout_stderr_and_timeouts_are_bounded(self):
        for program in ("import sys;sys.stdout.write('x'*1000000)",
                        "import sys;sys.stderr.write('private'*100000)",
                        "import time;time.sleep(60)"):
            started = time.monotonic()
            with self.assertRaises(ContractError) as caught:
                execute([sys.executable, "-c", program], timeout=.3, maximum=100)
            self.assertLess(time.monotonic() - started, 3)
            self.assertNotIn("private", str(caught.exception))

    def test_failure_does_not_expose_diagnostics(self):
        with self.assertRaisesRegex(ContractError, "qualification command failed") as caught:
            execute([sys.executable, "-c", "import sys;sys.stderr.write('private-value');sys.exit(2)"])
        self.assertNotIn("private-value", str(caught.exception))

    def test_invalid_input_never_starts_command(self):
        with self.assertRaises(ContractError):
            execute(["nonexistent-command"], data=b"x" * (512 * 1024 + 1))

    def test_known_failure_exit_code_preserves_category_without_stderr(self):
        with self.assertRaisesRegex(ContractError, "qualification API read failed: rate-limited") as caught:
            execute([sys.executable, "-c", "import sys;sys.stderr.write('private-value');sys.exit(13)"],
                    failure_codes={13: "qualification API read failed: rate-limited"})
        self.assertNotIn("private-value", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
