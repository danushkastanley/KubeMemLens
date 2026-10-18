import copy
import unittest

from memory import paired_memory
from test_samples import fixture


ROLES = {"selected", "agent", "collector"}


def pair():
    enabled = fixture()
    for row in enabled:
        for role in ROLES:
            row["groups"][role] = copy.deepcopy(row["groups"]["node"])
        for value in row["groups"].values():
            value["memoryCurrent"] = 32 << 20
            value["memory"]["inactive_file"] = 0
    control = copy.deepcopy(enabled)
    for row in control:
        for role in ("node", "api"):
            del row["groups"][role]
    return control, enabled


class PairedMemoryTests(unittest.TestCase):
    def test_inclusive_boundary_and_single_byte_excess(self):
        control, enabled = pair()
        result = paired_memory(control, enabled, 3, ROLES)
        self.assertEqual(result["conservativePeakIncrementalWorkingSetBytes"], 64 << 20)
        self.assertTrue(result["normalObservedWorkingSetBudgetPassed"])
        # A startup spike is retained even if all later samples fit the budget.
        enabled[0]["groups"]["node"]["memoryCurrent"] += 1
        self.assertFalse(paired_memory(control, enabled, 3, ROLES)["normalObservedWorkingSetBudgetPassed"])

    def test_workload_decrease_cannot_pay_for_tracer_or_collector_growth(self):
        control, enabled = pair()
        for row in enabled:
            row["groups"]["selected"]["memoryCurrent"] = 1
            row["groups"]["collector"]["memoryCurrent"] += 1024
        result = paired_memory(control, enabled, 3, ROLES)
        self.assertEqual(result["conservativePeakIncrementalWorkingSetBytes"], (64 << 20) + 1024)
        self.assertFalse(result["normalObservedWorkingSetBudgetPassed"])

    def test_control_peak_is_not_subtracted_and_inactive_file_is_not_rss(self):
        control, enabled = pair()
        control[1]["groups"]["selected"]["memoryCurrent"] += 100 << 20
        for row in enabled:
            row["groups"]["node"]["memory"]["inactive_file"] = 4 << 20
            row["groups"]["node"]["process"]["rssBytes"] = 1
        result = paired_memory(control, enabled, 3, ROLES)
        self.assertEqual(result["controlWorkingSetFloorBytes"]["selected"], 32 << 20)
        self.assertEqual(result["conservativePeakIncrementalWorkingSetBytes"], 60 << 20)

    def test_role_peaks_are_summed_at_each_observation(self):
        control, enabled = pair()
        for row in enabled:
            row["groups"]["node"]["memoryCurrent"] = 10 << 20
            row["groups"]["api"]["memoryCurrent"] = 10 << 20
        enabled[0]["groups"]["node"]["memoryCurrent"] += 10 << 20
        enabled[1]["groups"]["api"]["memoryCurrent"] += 10 << 20
        self.assertEqual(paired_memory(control, enabled, 3, ROLES)["installationPeakWorkingSetBytes"], 30 << 20)

    def test_incomplete_or_changed_pair_cannot_claim_memory_result(self):
        for failure in ("tracer-in-control", "missing-tracer", "missing-shared", "short", "oom", "reset", "clock"):
            control, enabled = pair()
            if failure == "tracer-in-control":
                control[0]["groups"]["node"] = copy.deepcopy(enabled[0]["groups"]["node"])
            elif failure == "missing-tracer":
                del enabled[1]["groups"]["api"]
            elif failure == "missing-shared":
                del enabled[1]["groups"]["collector"]
            elif failure == "short":
                control.pop()
            elif failure == "oom":
                enabled[1]["groups"]["node"]["memoryEvents"]["oom_kill"] = 1
            elif failure == "reset":
                control[2]["groups"]["selected"]["cpu"]["usage_usec"] = 0
            else:
                enabled[1]["wallNanos"] += 200000000
            with self.subTest(failure=failure), self.assertRaises(ValueError):
                paired_memory(control, enabled, 3, ROLES)
        for roles in ({"agent"}, {"selected", "node"}, ["selected"]):
            with self.subTest(roles=roles), self.assertRaises(ValueError):
                paired_memory(*pair(), 3, roles)


if __name__ == "__main__":
    unittest.main()
