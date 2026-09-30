"""Keep missing baseline evidence and changed evaluation methods explicit."""
import copy
import unittest

from compare_revision import compare


class ComparisonTests(unittest.TestCase):
    def report(self):
        case = {"id": "memory-example", "diagnosisMatched": True, "confidenceMatched": True,
                "abstentionMatched": True, "abstained": False, "falsePositives": [],
                "falseNegatives": [], "prohibitedAdvice": 0, "unreviewedAdvice": 0,
                "missingSafetyGuard": False, "unsupportedEvidence": 0, "copyMatched": True, "passed": True}
        return {"schemaVersion": 1, "taxonomyVersion": 1, "corpusSHA256": "a" * 64,
                "sources": {"synthetic": 1}, "cases": [case], "rules": {}, "passed": True}

    def test_changes_retain_before_and_after(self):
        before = self.report()
        after = copy.deepcopy(before)
        after["cases"][0].update(diagnosisMatched=False, passed=False)
        after["passed"] = False
        result = compare(before, after)
        self.assertTrue(result["beforePassed"])
        self.assertFalse(result["afterPassed"])
        self.assertEqual(result["changes"][0]["changes"]["diagnosisMatched"],
                         {"before": True, "after": False})
        self.assertEqual(compare(before, before)["changes"], [])

    def test_contract_changes_and_duplicate_cases_are_rejected(self):
        before = self.report()
        for key, value in (("taxonomyVersion", 2), ("corpusSHA256", "b" * 64),
                           ("sources", {"local-cluster": 1}), ("cases", []),
                           ("cases", before["cases"] * 2)):
            with self.subTest(key=key), self.assertRaises(ValueError):
                compare(before, before | {key: value})


if __name__ == "__main__":
    unittest.main()
