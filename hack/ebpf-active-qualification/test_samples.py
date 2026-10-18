import copy
import unittest

from samples import scheduling_deltas, validate_window
from resources import resource_summary


def fixture(seconds=3):
    rows = []
    for i in range(seconds + 1):
        task = {"state": "observed", "source": "/proc/pid/task/tid/schedstat", "cohort": "a" * 64,
                "runtimeNanos": 1000 + i * 100, "runqueueWaitNanos": 10 + i, "timeslices": 1 + i}
        group = {"cpu": {"usage_usec": 1000 + i * 5000, "user_usec": 500, "system_usec": 500,
                         "nr_periods": i, "nr_throttled": 0, "throttled_usec": 0},
                 "memoryCurrent": 20 << 20, "memory": {"anon": 10 << 20, "file": 10 << 20, "shmem": 0, "inactive_file": 2 << 20},
                 "memoryEvents": {"oom": 0, "oom_kill": 0, "max": 0, "high": 0}, "pids": 2,
                 "process": {"state": "observed", "rssBytes": 10 << 20, "count": 1, "tasks": 2, "scheduling": task}}
        node = {"cpuTicks": [i * 10] * 10, "memoryAvailable": 1 << 30,
                "observerCPUUsec": i * 1000, "observerPeakRSSBytes": 4 << 20,
                "pressure": {kind: {"some": i, "full": i} for kind in ("cpu", "memory", "io")},
                "scheduling": {"state": "unsupported", "source": "/proc/schedstat"}}
        rows.append({"schemaVersion": 2, "index": i, "elapsedNanos": i * 1000000000,
                     "wallNanos": 1700000000000000000 + i * 1000000000, "readNanos": 1000,
                     "groups": {role: copy.deepcopy(group) for role in ("node", "api")}, "node": node})
    return rows


class SamplesTests(unittest.TestCase):
    def test_complete_and_unavailable_sources_remain_distinct(self):
        rows = fixture()
        validate_window(rows, 3, {"node", "api"})
        self.assertTrue(all(r["state"] == "unavailable" and "runtimeNanos" not in r
                            for r in scheduling_deltas(rows)))
        self.assertEqual(scheduling_deltas(rows, "node")[0]["runqueueWaitNanos"], 1)
        rows[1]["groups"]["node"]["process"] = {"state": "unavailable"}
        validate_window(rows, 3, {"node", "api"})
        self.assertEqual([r["state"] for r in scheduling_deltas(rows, "node")],
                         ["unavailable", "unavailable", "observed"])

    def test_changed_cohort_and_reset_do_not_create_negative_or_zero_deltas(self):
        rows = fixture()
        rows[1]["groups"]["node"]["process"]["scheduling"]["cohort"] = "b" * 64
        self.assertEqual(scheduling_deltas(rows, "node")[0]["state"], "cohort-changed")
        rows = fixture()
        rows[1]["groups"]["node"]["process"]["scheduling"]["timeslices"] = 0
        self.assertEqual(scheduling_deltas(rows, "node")[0]["state"], "counter-reset")

    def test_missing_counters_fabricated_availability_and_time_gaps_fail(self):
        for kind in ("missing-counter", "unknown-is-zero", "wrong-version", "gap", "clock", "oom", "pid-disclosure"):
            rows = fixture()
            if kind == "missing-counter":
                del rows[1]["groups"]["node"]["cpu"]["usage_usec"]
            elif kind == "unknown-is-zero":
                rows[1]["node"]["scheduling"]["runtimeNanos"] = 0
            elif kind == "wrong-version":
                rows[1]["schemaVersion"] = 1
            elif kind == "gap":
                rows[1]["elapsedNanos"] += 200000000
            elif kind == "clock":
                rows[1]["wallNanos"] += 200000000
            elif kind == "oom":
                rows[1]["groups"]["node"]["memoryEvents"]["oom"] = 1
            else:
                rows[1]["groups"]["node"]["process"]["pid"] = 100
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                validate_window(rows, 3, {"node", "api"})

    def test_cpu_boundary_is_inclusive_and_gaps_never_dilute_cost(self):
        rows = fixture()
        result = resource_summary(rows, [True] * 3, 3)
        self.assertEqual(result["allCostPerObservedActiveTimeCPUMilli"], 10)
        self.assertTrue(result["normalCPUBudgetPassed"])
        result = resource_summary(rows, [True, False, True], 2)
        self.assertEqual(result["wholeWindowMeanCPUMilli"], 10)
        self.assertEqual(result["allCostPerObservedActiveTimeCPUMilli"], 15)
        self.assertFalse(result["normalCPUBudgetPassed"])
        self.assertNotIn("memoryBudgetPassed", result)
        with self.assertRaises(ValueError):
            resource_summary(rows, [True, False, True], 3)
        with self.assertRaises(ValueError):
            resource_summary(rows, [1, True, True], 3)


if __name__ == "__main__":
    unittest.main()
