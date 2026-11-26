import json
import subprocess
import unittest
from unittest.mock import patch

from sdk_download import download, transient_failure


class SDKDownloadTests(unittest.TestCase):
    def result(self, code, output):
        return subprocess.CompletedProcess([], code, output, "")

    @patch("sdk_download.time.sleep")
    @patch("sdk_download.subprocess.run")
    def test_transient_fetch_recovers_with_same_module_and_total_deadline(self, run, sleep):
        expected = {"Version": "v0.56.0", "Sum": "expected", "Dir": "source"}
        run.side_effect = [self.result(1, '{"Error":"503 Service Unavailable"}'),
                           self.result(0, json.dumps(expected))]
        self.assertEqual(download("module@v0.56.0", "stage", {"GOWORK": "off"}), expected)
        self.assertEqual(run.call_count, 2)
        self.assertEqual(run.call_args_list[0].args, run.call_args_list[1].args)
        self.assertLessEqual(run.call_args_list[1].kwargs["timeout"], run.call_args_list[0].kwargs["timeout"])
        self.assertLessEqual(run.call_args_list[0].kwargs["timeout"], 180)
        sleep.assert_called_once()

    @patch("sdk_download.time.sleep")
    @patch("sdk_download.subprocess.run")
    def test_attempt_count_is_bounded(self, run, sleep):
        run.return_value = self.result(1, '{"Error":"connection reset"}')
        with self.assertRaisesRegex(ValueError, "bounded attempts"):
            download("module", "stage", {})
        self.assertEqual(run.call_count, 3)
        self.assertEqual(sleep.call_count, 2)

    @patch("sdk_download.time.sleep")
    @patch("sdk_download.subprocess.run")
    def test_integrity_and_unknown_failures_do_not_retry_or_echo_private_data(self, run, sleep):
        for failure in ("SECURITY ERROR: checksum mismatch; connection reset private-token",
                        "unknown private-token"):
            run.reset_mock()
            run.return_value = self.result(1, failure)
            with self.assertRaises(ValueError) as caught:
                download("module", "stage", {})
            self.assertNotIn("private", str(caught.exception))
            self.assertEqual(run.call_count, 1)
        sleep.assert_not_called()

    @patch("sdk_download.subprocess.run")
    def test_process_timeout_does_not_restart_the_deadline(self, run):
        run.side_effect = subprocess.TimeoutExpired("go", 180)
        with self.assertRaisesRegex(ValueError, "deadline"):
            download("module", "stage", {})
        self.assertEqual(run.call_count, 1)

    @patch("sdk_download.time.monotonic", side_effect=[0, 0, 179, 180])
    @patch("sdk_download.time.sleep")
    @patch("sdk_download.subprocess.run")
    def test_elapsed_window_prevents_another_attempt(self, run, sleep, clock):
        run.return_value = self.result(1, '{"Error":"i/o timeout"}')
        with self.assertRaisesRegex(ValueError, "bounded attempts"):
            download("module", "stage", {})
        self.assertEqual(run.call_count, 1)
        sleep.assert_called_once_with(1)

    @patch("sdk_download.subprocess.run")
    def test_malformed_or_error_metadata_is_rejected(self, run):
        for output in ("[]", '{"Error":"failure"}', "not json"):
            run.return_value = self.result(0, output)
            with self.assertRaises(ValueError):
                download("module", "stage", {})

    def test_transient_reasons_cannot_override_integrity_failure(self):
        self.assertTrue(transient_failure("", "i/o timeout"))
        self.assertFalse(transient_failure("checksum mismatch", "i/o timeout"))


if __name__ == "__main__":
    unittest.main()
