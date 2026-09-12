import unittest
from types import SimpleNamespace

from check_api_reads import observe
from common import ContractError


class APIReadDiagnosticTest(unittest.TestCase):
    def test_incomplete_mapping_is_not_a_passing_read(self):
        for counts, expected in (((32, 32), "passed"), ((32, 31), "incomplete-mapping"), ((31, 31), "incomplete-mapping")):
            runtime = SimpleNamespace(workload=lambda: counts)
            self.assertEqual(observe(runtime, 32), expected)

    def test_only_fixed_failure_categories_enter_evidence(self):
        for message, expected in (("qualification API read failed: rate-limited", "rate-limited"),
                                  ("qualification API read failed: private-host", "observation-failed"),
                                  ("private-credential", "observation-failed")):
            def fail():
                raise ContractError(message)
            self.assertEqual(observe(SimpleNamespace(workload=fail), 32), expected)


if __name__ == "__main__":
    unittest.main()
