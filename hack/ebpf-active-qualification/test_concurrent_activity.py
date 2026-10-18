import json
import unittest

from activity import active_intervals, active_signatures, operation_activity
from workload import compare_workloads
from test_activity import COUNTS, witness
from test_samples import fixture
from test_workload import series
from test_standard_window import rows as standard_rows
from scans import compare_scans

COMBINED = {kind: count * 2 for kind, count in COUNTS.items()}


def simultaneous(seconds=5):
    rows = witness(seconds)
    for row in rows:
        row.update(schemaVersion=2, targetWorkers=[1, 1])
        row['snapshot'].update(workers=2, activeControls=2,
                               objects={kind: list(range(1, count + 1)) for kind, count in COMBINED.items()})
    return rows


class ConcurrentActivityTests(unittest.TestCase):
    def test_two_workers_require_an_explicit_expectation(self):
        rows = simultaneous()
        with self.assertRaises(ValueError):
            active_intervals(fixture(5), rows, 5, COMBINED)
        self.assertEqual(active_intervals(fixture(5), rows, 5, COMBINED, expected_workers=2),
                         [False, True, True, True, False])
        with self.assertRaises(ValueError):
            active_intervals(fixture(5), witness(), 5, COUNTS, expected_workers=2)

    def test_partial_excluded_replaced_or_single_worker_intervals_cannot_count_as_simultaneous(self):
        for condition in ('single-worker', 'single-control', 'excluded', 'partial-map', 'replaced-link'):
            rows = simultaneous()
            current = rows[2]['snapshot']
            if condition == 'single-worker':
                current.update(workers=1, activeControls=1)
                rows[2]['targetWorkers'] = [1, 0]
            elif condition == 'single-control':
                current['activeControls'] = 1
            elif condition == 'excluded':
                current['excludedWorkers'] = 1
            elif condition == 'partial-map':
                current['objects']['map'].pop()
            else:
                current['objects']['link'] = list(range(100, 100 + COMBINED['link']))
            with self.subTest(condition=condition):
                self.assertFalse(any(active_intervals(fixture(5), rows, 5, COMBINED, expected_workers=2)[:4]))

    def test_two_workers_on_one_target_do_not_prove_simultaneous_two_target_coverage(self):
        for counts in ([2, 0], [0, 2]):
            rows = simultaneous()
            rows[2]['targetWorkers'] = counts
            self.assertFalse(any(active_intervals(fixture(5), rows, 5, COMBINED, expected_workers=2)[:4]))
        for counts in (None, [1], [1, 0], [True, 1], [1, 1, 0]):
            rows = simultaneous()
            rows[2]['targetWorkers'] = counts
            with self.subTest(counts=counts), self.assertRaises(ValueError):
                active_intervals(fixture(5), rows, 5, COMBINED, expected_workers=2)

    def test_unavailable_target_coverage_remains_unproven_and_cannot_contain_zero_substitutes(self):
        rows = simultaneous()
        rows[2]['state'] = 'unavailable'
        del rows[2]['snapshot']
        del rows[2]['targetWorkers']
        self.assertFalse(any(active_intervals(fixture(5), rows, 5, COMBINED, expected_workers=2)[:4]))
        rows[2]['targetWorkers'] = [0, 0]
        with self.assertRaises(ValueError):
            active_intervals(fixture(5), rows, 5, COMBINED, expected_workers=2)

    def test_unsupported_or_boolean_worker_counts_are_rejected(self):
        for count in (0, 3, True, 1.0, '2'):
            with self.subTest(count=count), self.assertRaises(ValueError):
                active_signatures(simultaneous(), COMBINED, expected_workers=count)

    def test_workload_operations_need_the_full_two_worker_bracket(self):
        records = [json.loads(line) for line in series().splitlines()]
        rows = simultaneous(12)
        self.assertEqual(operation_activity(records[1:], rows, 12, COMBINED, expected_workers=2), [True] * 5)
        result = compare_workloads(series(), series(), mode='cached', count=5, period_ms=1000,
                                   witness=rows, seconds=12, expected_objects=COMBINED,
                                   minimum_active_operations=5, expected_workers=2)
        self.assertTrue(result['normalSelectedLatencyBudgetPassed'])
        rows[7]['snapshot']['activeControls'] = 1
        with self.assertRaises(ValueError):
            compare_workloads(series(), series(), mode='cached', count=5, period_ms=1000,
                              witness=rows, seconds=12, expected_objects=COMBINED,
                              minimum_active_operations=5, expected_workers=2)

    def test_scans_require_simultaneous_coverage_across_the_whole_scan(self):
        activity = simultaneous(8)
        kwargs = {'seconds': 8, 'witness': activity, 'expected_objects': COMBINED,
                  'minimum_scans': 4, 'minimum_active_scans': 3, 'expected_workers': 2}
        result = compare_scans(standard_rows(), standard_rows(), **kwargs)
        self.assertEqual(result['fullyBracketedScans']['scans'], 3)
        self.assertTrue(result['normalScanBudgetPassed'])
        activity[4]['snapshot']['activeControls'] = 1
        with self.assertRaises(ValueError):
            compare_scans(standard_rows(), standard_rows(), **kwargs)


if __name__ == '__main__':
    unittest.main()
