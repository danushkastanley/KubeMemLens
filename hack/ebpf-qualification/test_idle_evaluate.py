"""Meaningful rejection tests; generated values are never runtime evidence."""

from copy import deepcopy
from pathlib import Path
import unittest
import json
import tempfile

from idle_evaluate import EXPECTED_PROFILE, evaluate_pair, load_profile, strict_json, validate_window


def group():
    return {"cpu": {"usage_usec": 0, "user_usec": 0, "system_usec": 0,
                    "nr_periods": 0, "nr_throttled": 0, "throttled_usec": 0},
            "memoryCurrent": 40 * 1024 * 1024, "rssBytes": 123, "processes": 1, "pids": 3,
            "memory": {"anon": 123, "file": 0, "shmem": 0, "inactive_file": 0},
            "memoryEvents": {"oom": 0, "oom_kill": 0, "max": 0, "high": 0}}


def window(enabled):
    rows = []
    for i in range(901):
        groups = {role: group() for role in ("node", "api")} if enabled else {}
        for role, g in groups.items():
            g["cpu"]["usage_usec"] = i * 5000 if role == "node" else 0
            g["memoryCurrent"] = (39 if role == "node" else 1) * 1024 * 1024
        rows.append({"schemaVersion": 1, "index": i, "elapsedNanos": i * 10**9,
                     "wallNanos": 1_789_000_000 * 10**9 + i * 10**9,
                     "readNanos": 1000, "groups": groups,
                     "node": {"cpuTicks": [i] * 10, "memoryAvailable": 100,
                              "pressure": {k: {"some": i, "full": 0} for k in ("cpu", "memory", "io")},
                              "observerCPUUsec": i, "observerPeakRSSBytes": 123}})
    return rows


class IdleEvidenceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.control, cls.enabled = window(False), window(True)

    def test_source_profile_has_frozen_thresholds(self):
        self.assertEqual(load_profile(Path(__file__).with_name("idle_profile.json")), EXPECTED_PROFILE)

    def test_duplicate_or_nonfinite_evidence_cannot_hide_payloads(self):
        for value in ('{"usage_usec":"unreviewed","usage_usec":1}', '{"nested":{"a":1,"a":2}}',
                      '{"number":NaN}', '{"number":Infinity}'):
            with self.assertRaises(ValueError):
                strict_json(value)

    def test_changed_budget_cannot_qualify(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "profile.json"
            for key in ("idleMeanCPUMilli", "idlePeakWorkingSetBytes", "windowSeconds", "repetitions"):
                changed = dict(EXPECTED_PROFILE)
                changed[key] += 1
                path.write_text(json.dumps(changed))
                with self.assertRaises(ValueError):
                    load_profile(path)

    def test_inclusive_budgets_and_single_core_denominator(self):
        result = evaluate_pair(self.control, self.enabled)
        self.assertTrue(result["idleBudgetPassed"])
        self.assertEqual(result["node"]["meanCPUMilli"], 5)
        self.assertEqual(result["node"]["p99CPUMilli"], 5)
        self.assertEqual(result["installationMeanCPUMilli"], 5)
        self.assertEqual(result["installationPeakWorkingSetBytes"], 40 * 1024 * 1024)

    def test_api_cost_can_fail_combined_budget(self):
        rows = deepcopy(self.enabled)
        rows[450]["groups"]["api"]["memoryCurrent"] += 1
        result = evaluate_pair(self.control, rows)
        self.assertTrue(result["nodeBudgetPassed"])
        self.assertFalse(result["idleBudgetPassed"])

    def test_complete_over_budget_run_is_failure_not_invalid(self):
        rows = deepcopy(self.enabled)
        rows[450]["groups"]["node"]["memoryCurrent"] += 1
        self.assertFalse(evaluate_pair(self.control, rows)["idleBudgetPassed"])
        rows = deepcopy(self.enabled)
        rows[-1]["groups"]["node"]["cpu"]["usage_usec"] += 1
        self.assertFalse(evaluate_pair(self.control, rows)["idleBudgetPassed"])

    def test_does_not_subtract_shmem_or_api(self):
        rows = deepcopy(self.enabled)
        rows[450]["groups"]["node"]["memoryCurrent"] += 100
        rows[450]["groups"]["node"]["memory"]["shmem"] = 100
        self.assertFalse(evaluate_pair(self.control, rows)["idleBudgetPassed"])
        del rows[0]["groups"]["api"]
        with self.assertRaises(ValueError):
            evaluate_pair(self.control, rows)

    def test_short_missing_reordered_and_gapped_windows_rejected(self):
        cases = [self.enabled[:-1], self.enabled[1:], self.enabled[:400] + self.enabled[401:]]
        for value in (900_000_001, -1, 100_000_001):
            rows = deepcopy(self.enabled)
            rows[450]["readNanos"] = value
            cases.append(rows)
        rows = deepcopy(self.enabled)
        rows[450]["elapsedNanos"] += 110_000_000
        cases.append(rows)
        for rows in cases:
            with self.assertRaises(ValueError):
                validate_window(rows, True)

    def test_null_false_zero_reset_oom_and_extra_identity_rejected(self):
        mutations = [
            lambda r: r[2]["groups"]["node"]["cpu"].pop("usage_usec"),
            lambda r: r[2]["groups"]["node"]["cpu"].update(usage_usec=None),
            lambda r: r[2]["groups"]["node"]["cpu"].update(usage_usec=False),
            lambda r: r[2]["groups"]["node"].update(rssBytes=0),
            lambda r: r[2]["groups"]["node"]["cpu"].update(usage_usec=0),
            lambda r: r[2]["groups"]["node"]["memoryEvents"].update(oom_kill=1),
            lambda r: r[2].update(podName="must-not-retain"),
            lambda r: r[2].update(wallNanos=r[2]["wallNanos"] + 1_000_000_000),
            lambda r: r[2]["groups"]["node"].update(processes=2),
        ]
        for mutate in mutations:
            rows = deepcopy(self.enabled)
            mutate(rows)
            with self.assertRaises(ValueError):
                validate_window(rows, True)

    def test_control_cannot_contain_optional_services(self):
        with self.assertRaises(ValueError):
            evaluate_pair(self.enabled, self.enabled)


if __name__ == "__main__":
    unittest.main()
