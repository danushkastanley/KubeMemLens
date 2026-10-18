import hashlib
from pathlib import Path
import unittest
from unittest.mock import patch

from common import ContractError, privacy
from failure_location import locate
from source_summary import summarise


class FailureLocationTest(unittest.TestCase):
    def failure(self):
        try:
            summarise([], ["private-node"])
        except ContractError as error:
            return error
        self.fail("the source guard did not reject incomplete observations")

    def test_reports_the_guard_caller_instead_of_the_shared_require_function(self):
        location = locate(self.failure())
        source = Path(__file__).with_name("source_summary.py").read_bytes()
        self.assertEqual(location["moduleDigest"], "sha256:" + hashlib.sha256(source).hexdigest())
        self.assertIn(b"require(len(observations)", source.splitlines()[location["line"] - 1])
        self.assertEqual(set(location), {"moduleDigest", "line"})
        privacy(location)

    def test_does_not_export_external_paths_messages_or_local_values(self):
        try:
            exec(compile("raise ValueError('private-credential')", "/private/tenant/secret.py", "exec"))
        except ValueError as error:
            with patch("failure_location.ROOT", Path("/unrelated/qualification")):
                self.assertIsNone(locate(error))

    def test_missing_or_overlarge_source_does_not_mask_the_original_failure(self):
        error = self.failure()
        with patch.object(Path, "resolve", side_effect=OSError("private-path")):
            self.assertIsNone(locate(error))
        with patch.object(Path, "open", side_effect=OSError("private-path")):
            self.assertIsNone(locate(error))
        from io import BytesIO
        with patch.object(Path, "open", return_value=BytesIO(b"x" * (512 * 1024 + 1))):
            self.assertIsNone(locate(error))


if __name__ == "__main__":
    unittest.main()
