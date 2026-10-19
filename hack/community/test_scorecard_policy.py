"""Exercise release score thresholds independently of live GitHub settings."""

import copy
import json
from pathlib import Path
import subprocess
import unittest


PREDICATE = Path(__file__).with_name("scorecard_policy.jq")
BOUNDARY = {
    "score": 7.0,
    "checks": [
        {"name": "Branch-Protection", "score": 4},
        {"name": "Token-Permissions", "score": 8},
        {"name": "Dangerous-Workflow", "score": 8},
        {"name": "Pinned-Dependencies", "score": 8},
        {"name": "Signed-Releases", "score": 8},
        {"name": "Vulnerabilities", "score": 7},
    ],
}


class ScorecardPolicyTest(unittest.TestCase):
    def accepted(self, report):
        result = subprocess.run(
            ["jq", "-e", "-f", str(PREDICATE)],
            input=json.dumps(report), text=True, capture_output=True, check=False,
        )
        self.assertIn(result.returncode, (0, 1), result.stderr)
        return result.returncode == 0

    def test_checks_only_boundary_is_accepted(self):
        self.assertTrue(self.accepted(BOUNDARY))

    def test_each_check_below_its_minimum_is_rejected(self):
        for index, check in enumerate(BOUNDARY["checks"]):
            with self.subTest(check=check["name"]):
                report = copy.deepcopy(BOUNDARY)
                report["checks"][index]["score"] -= 1
                self.assertFalse(self.accepted(report))

    def test_overall_score_below_minimum_is_rejected(self):
        report = copy.deepcopy(BOUNDARY)
        report["score"] = 6.9
        self.assertFalse(self.accepted(report))

    def test_missing_required_check_is_rejected(self):
        for index, check in enumerate(BOUNDARY["checks"]):
            with self.subTest(check=check["name"]):
                report = copy.deepcopy(BOUNDARY)
                del report["checks"][index]
                self.assertFalse(self.accepted(report))

    def test_high_overall_score_does_not_waive_vulnerability_findings(self):
        report = copy.deepcopy(BOUNDARY)
        report["score"] = 8.0
        report["checks"][-1]["score"] = 4
        self.assertFalse(self.accepted(report))


if __name__ == "__main__":
    unittest.main()
