import copy
import unittest

from mixed_workload import compare_mixed_workloads
from test_mixed_series import records, encode
from test_activity import COUNTS, witness


def compare(control, enabled, activity=None, minimum=2):
    return compare_mixed_workloads(encode(control), encode(enabled), count=6, period_ms=1000,
                                  witness=activity or witness(13), seconds=13, expected_objects=COUNTS,
                                  minimum_active_operations_per_mode=minimum)


class MixedWorkloadTests(unittest.TestCase):
    def test_exact_per_mode_pairs_and_byte_totals(self):
        rows = records(6, 1000)
        result = compare(rows, rows)
        self.assertTrue(result['mixedSelectedLatencyBudgetPassed'])
        self.assertEqual(result['scheduledReadBytes'], 4 * 8388608)
        self.assertEqual(result['scheduledWriteBytes'], 2 * 8388608)
        for mode in ('cached', 'uncached', 'write'):
            self.assertEqual(result['byMode'][mode]['fullyBracketedOperations']['control']['operations'], 2)
            self.assertIsNone(result['byMode'][mode]['unprovenOrTransitionOperations'])

    def test_aggregate_pass_cannot_hide_cached_read_regression(self):
        control = records(6, 1000)
        for row in control[1:]:
            obs = row['observation']
            elapsed = 1000000 if obs['mode'] == 'cached' else 20000000
            obs['operationNanos'] = elapsed
            obs['operationEndedMonotonicNanos'] = obs['operationStartedMonotonicNanos'] + elapsed
        for elapsed, passed in ((1019999, True), (1020000, False), (2000000, False)):
            enabled = copy.deepcopy(control)
            for row in enabled[1::3]:
                obs = row['observation']
                obs['operationNanos'] = elapsed
                obs['operationEndedMonotonicNanos'] = obs['operationStartedMonotonicNanos'] + elapsed
            with self.subTest(elapsed=elapsed):
                result = compare(control, enabled)
                self.assertTrue(result['combined']['selectedLatencyBudgetPassed'])
                self.assertEqual(result['mixedSelectedLatencyBudgetPassed'], passed)

    def test_inactive_operations_are_retained_but_each_class_needs_its_own_minimum(self):
        rows = records(6, 1000)
        activity = witness(13)
        activity[7]['snapshot']['activeControls'] = 0
        result = compare(rows, rows, activity, minimum=1)
        self.assertEqual(result['combined']['allOperations']['control']['operations'], 6)
        self.assertIsNotNone(result['combined']['unprovenOrTransitionOperations'])
        with self.assertRaisesRegex(ValueError, 'insufficient fully bracketed operations'):
            compare(rows, rows, activity)

    def test_clock_shift_and_missing_or_substituted_mode_cannot_compare(self):
        original = records(6, 1000)
        shifted = copy.deepcopy(original)
        shifted[0]['wallNanos'] += 1000000000
        substituted = copy.deepcopy(original)
        substituted[2]['observation']['mode'] = 'cached'
        for enabled in (shifted, original[:-1], substituted):
            with self.subTest(enabled=enabled), self.assertRaises(ValueError):
                compare(original, enabled)


if __name__ == '__main__':
    unittest.main()
