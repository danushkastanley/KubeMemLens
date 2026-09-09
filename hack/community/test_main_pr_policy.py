"""Exercise the same PR-policy predicate used for live GitHub readback."""

import copy
import json
from pathlib import Path
import subprocess
import unittest


PREDICATE = Path(__file__).with_name("main_pr_policy.jq")
POLICY = [{
    "type": "pull_request",
    "parameters": {
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": True,
        "require_code_owner_review": False,
        "require_last_push_approval": False,
        "required_review_thread_resolution": True,
    },
}]


class MainPRPolicyTest(unittest.TestCase):
    def accepted(self, rules):
        result = subprocess.run(
            ["jq", "-e", "-f", str(PREDICATE)],
            input=json.dumps(rules), text=True, capture_output=True, check=False,
        )
        self.assertIn(result.returncode, (0, 1), result.stderr)
        return result.returncode == 0

    def test_checks_only_policy_is_accepted(self):
        self.assertTrue(self.accepted(POLICY))

    def test_missing_pr_requirement_is_rejected(self):
        self.assertFalse(self.accepted([]))
        self.assertFalse(self.accepted([{"type": "deletion"}]))

    def test_human_review_gates_are_rejected(self):
        for key, value in {
            "required_approving_review_count": 1,
            "require_code_owner_review": True,
            "require_last_push_approval": True,
        }.items():
            with self.subTest(key=key):
                rules = copy.deepcopy(POLICY)
                rules[0]["parameters"][key] = value
                self.assertFalse(self.accepted(rules))

    def test_conversation_and_stale_review_controls_are_required(self):
        for key in ("required_review_thread_resolution", "dismiss_stale_reviews_on_push"):
            with self.subTest(key=key):
                rules = copy.deepcopy(POLICY)
                rules[0]["parameters"][key] = False
                self.assertFalse(self.accepted(rules))

    def test_missing_parameters_fail_closed(self):
        for key in POLICY[0]["parameters"]:
            with self.subTest(key=key):
                rules = copy.deepcopy(POLICY)
                del rules[0]["parameters"][key]
                self.assertFalse(self.accepted(rules))


if __name__ == "__main__":
    unittest.main()
