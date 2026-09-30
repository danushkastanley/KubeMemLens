import json
import unittest

from workload import compare_workloads
from test_activity import COUNTS, witness


def series(elapsed=1000000):
    start = {"type": "series-start", "schemaVersion": 1, "count": 5,
             "periodNanos": 1000000000, "monotonicBeforeNanos": 1000000000,
             "monotonicAfterNanos": 1000000001, "wallNanos": 1700000000000000000,
             "firstDueNanos": 6000000001}
    rows = [start]
    for i in range(5):
        due = start["firstDueNanos"] + i * start["periodNanos"]
        rows.append({"sequence": i, "dueMonotonicNanos": due, "observation": {
            "mode": "cached", "fileBytes": 8388608, "pageBytes": 4096,
            "residentPagesBefore": 2048, "residentPagesAfter": 2048,
            "readBytes": 8388608, "writeBytes": 0,
            "operationStartedMonotonicNanos": due + 1000000,
            "operationEndedMonotonicNanos": due + 1000000 + elapsed,
            "operationNanos": elapsed}})
    return "".join(json.dumps(row) + "\n" for row in rows)


def compare(control, enabled, activity=None, minimum=5):
    return compare_workloads(control, enabled, mode="cached", count=5, period_ms=1000,
                             witness=activity or witness(12), seconds=12,
                             expected_objects=COUNTS, minimum_active_operations=minimum)


class WorkloadTests(unittest.TestCase):
    def test_complete_pair_and_paced_rate_are_explicit(self):
        result = compare(series(), series())
        self.assertTrue(result["normalSelectedLatencyBudgetPassed"])
        self.assertEqual(result["achievedScheduledBytesPerSecond"], 8388608)
        self.assertIsNone(result["unprovenOrTransitionOperations"])
        self.assertNotIn("eventLatency", result)

    def test_exact_two_percent_latency_regression_fails(self):
        self.assertTrue(compare(series(), series(1019999))["normalSelectedLatencyBudgetPassed"])
        self.assertFalse(compare(series(), series(1020000))["normalSelectedLatencyBudgetPassed"])
        self.assertFalse(compare(series(), series(2000000))["normalSelectedLatencyBudgetPassed"])

    def test_unproven_operations_are_retained_and_not_counted_as_active(self):
        activity = witness(12)
        activity[7]["snapshot"]["activeControls"] = 0
        result = compare(series(), series(), activity, minimum=1)
        self.assertEqual(result["allOperations"]["control"]["operations"], 5)
        self.assertGreater(result["unprovenOrTransitionOperations"]["control"]["operations"], 0)
        self.assertLess(result["fullyBracketedOperations"]["enabled"]["operations"], 5)
        with self.assertRaises(ValueError):
            compare(series(), series(), activity, minimum=5)

    def test_incomplete_wrong_bytes_or_retimed_schedule_fails(self):
        for enabled in (series().replace('"wallNanos": 1700000000000000000', '"wallNanos": 1700000010000000000'),
                        "\n".join(series().splitlines()[:-1]) + "\n",
                        series().replace('"readBytes": 8388608', '"readBytes": 1'),
                        series().replace('"sequence": 1', '"sequence": 0'),
                        series().replace('"periodNanos": 1000000000', '"periodNanos": 2000000000')):
            with self.assertRaises(ValueError):
                compare(series(), enabled)


if __name__ == "__main__":
    unittest.main()
