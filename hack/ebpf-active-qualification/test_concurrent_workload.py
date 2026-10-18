import json
import unittest

from concurrent_workload import TARGETS, compare_concurrent_workloads
from test_concurrent_activity import COMBINED, simultaneous
from test_workload import series


def streams(first=1000000, second=1000000):
    return dict(zip(TARGETS, (series(first), series(second))))


def compare(control, enabled, witness=None, minimum=5):
    return compare_concurrent_workloads(
        control, enabled, mode='cached', count=5, period_ms=1000,
        witness=simultaneous(12) if witness is None else witness,
        seconds=12, expected_objects=COMBINED, minimum_active_operations=minimum)


class ConcurrentWorkloadTests(unittest.TestCase):
    def test_two_complete_streams_retain_separate_verdicts(self):
        result = compare(streams(), streams(1019999, 1000000))
        self.assertTrue(result['concurrentSelectedLatencyBudgetPassed'])
        self.assertEqual(set(result['workloads']), set(TARGETS))
        for workload in result['workloads'].values():
            self.assertEqual(workload['fullyBracketedOperations']['enabled']['operations'], 5)
        self.assertIn('identity bindings', result['qualification'])

    def test_a_faster_peer_cannot_mask_either_targets_two_percent_regression(self):
        for enabled in (streams(1020000, 100), streams(100, 1020000)):
            with self.subTest(enabled=enabled):
                result = compare(streams(), enabled)
                self.assertFalse(result['concurrentSelectedLatencyBudgetPassed'])
                self.assertEqual(sum(r['normalSelectedLatencyBudgetPassed']
                                     for r in result['workloads'].values()), 1)

    def test_missing_extra_or_truncated_stream_fails(self):
        for enabled in ({}, {'target-0': series()}, {**streams(), 'target-2': series()},
                        {**streams(), 'target-1': '\n'.join(series().splitlines()[:-1]) + '\n'}):
            with self.subTest(enabled=enabled), self.assertRaises(ValueError):
                compare(streams(), enabled)

    def test_each_target_needs_its_own_minimum_simultaneously_bracketed_operations(self):
        # Both streams share the observer clock, but the second starts later.
        # Its last operations extend past the complete two-target observations.
        rows = [json.loads(line) for line in series().splitlines()]
        for field in ('monotonicBeforeNanos', 'monotonicAfterNanos', 'wallNanos', 'firstDueNanos'):
            rows[0][field] += 3000000000
        for row in rows[1:]:
            row['dueMonotonicNanos'] += 3000000000
            for field in ('operationStartedMonotonicNanos', 'operationEndedMonotonicNanos'):
                row['observation'][field] += 3000000000
        enabled = {**streams(), 'target-1': ''.join(json.dumps(row) + '\n' for row in rows)}
        with self.assertRaisesRegex(ValueError, 'insufficient fully bracketed'):
            compare(streams(), enabled)
        result = compare(streams(), enabled, minimum=1)
        self.assertEqual(result['workloads']['target-0']['fullyBracketedOperations']['enabled']['operations'], 5)
        self.assertLess(result['workloads']['target-1']['fullyBracketedOperations']['enabled']['operations'], 5)
        self.assertEqual(result['workloads']['target-1']['allOperations']['enabled']['operations'], 5)

    def test_duplicate_target_workers_cannot_qualify_either_stream(self):
        rows = simultaneous(12)
        for row in rows:
            row['targetWorkers'] = [2, 0]
        with self.assertRaisesRegex(ValueError, 'insufficient fully bracketed'):
            compare(streams(), streams(), rows)


if __name__ == '__main__':
    unittest.main()
