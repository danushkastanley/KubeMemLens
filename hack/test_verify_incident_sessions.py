import argparse
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from verify_incident_sessions import Walkthrough


class LiveAssertionTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.walk = Walkthrough(argparse.Namespace(
            output=str(Path(self.directory.name) / "new-evidence"),
            binary="fixture-binary", owner_kubeconfig="owner-config",
            owner_context="fixture", namespace="fixture", trace_report=None))

    def test_transport_failures_cannot_satisfy_denial(self):
        for code, out, error in [
            (1, "", "incident session service or response is unavailable"),
            (1, "", "incident action outcome could not be confirmed; it was not retried"),
            (1, "", ""),
            (0, "", "incident session access denied"),
            (1, "private evidence", "incident session access denied"),
        ]:
            with self.subTest(error=error, code=code, output=bool(out)):
                response = subprocess.CompletedProcess([], code, out, error)
                with patch("verify_incident_sessions.subprocess.run", return_value=response) as run:
                    with self.assertRaises(RuntimeError):
                        self.walk.command("owner", ["export", "session"],
                                          expect_error={"incident session access denied"})
                    self.assertEqual(run.call_count, 1)

    def test_timeout_never_retries_mutation(self):
        with patch("verify_incident_sessions.subprocess.run",
                   side_effect=subprocess.TimeoutExpired("fixture", 20)) as run:
            with self.assertRaisesRegex(RuntimeError, "not retried"):
                self.walk.command("owner", ["start"])
            self.assertEqual(run.call_count, 1)

    def test_private_evidence_does_not_replace_existing_file(self):
        self.walk.save("capture.json", "retained")
        with self.assertRaises(FileExistsError):
            self.walk.save("capture.json", "replacement")
        path = self.walk.output / "capture.json"
        self.assertEqual(path.read_text(), "retained")
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)

    def test_cleanup_does_not_claim_success_after_connection_failure(self):
        self.walk.sessions = {"owner": "session"}
        response = subprocess.CompletedProcess([], 1, "", "connection failed")
        with patch("verify_incident_sessions.subprocess.run", return_value=response):
            with self.assertRaisesRegex(RuntimeError, "cleanup could not be confirmed"):
                self.walk.cleanup()
        self.assertEqual(self.walk.checks, [])

    def test_unreadable_changed_report_cannot_satisfy_mismatch(self):
        report = Path(self.directory.name) / "report.json"
        report.write_text("{}")
        self.walk.options.trace_report = str(report)
        matched = subprocess.CompletedProcess([], 0,
            "Report bytes match the incident reference. Source authenticity remains unverified.\n", "")
        unreadable = subprocess.CompletedProcess([], 1, "", "cannot open trace report")
        with patch("verify_incident_sessions.subprocess.run", side_effect=[matched, unreadable]):
            with self.assertRaisesRegex(RuntimeError, "rejects a changed report"):
                self.walk.verify_trace()
        self.assertNotIn("offline verification rejects a changed report", self.walk.checks)


if __name__ == "__main__":
    unittest.main()
