from copy import deepcopy
import unittest

from pressure_metrics import compare_pressure
from test_flood_resources import contained
from test_samples import fixture
from test_pressure_observation import records, stream


def resource_rows(enabled):
    rows = fixture(4)
    limits = contained()[0]['groups']['node']['containment']
    for row in rows:
        row['schemaVersion'] = 3
        for group in row['groups'].values():
            group['containment'] = deepcopy(limits)
        base = deepcopy(row['groups']['node'])
        if not enabled:
            row['groups'] = {}
        row['groups'].update({role: deepcopy(base) for role in ('agent', 'collector', 'selected')})
    return rows


def producer():
    rows = records(6)
    # Producer starts one second before the resource window and ends after it.
    for row in rows:
        row['wallNanos'] -= 1000000000
    return rows


class PressureComparisonTests(unittest.TestCase):
    def compare(self, before=None, after=None):
        return compare_pressure(stream(before or producer()), stream(after or producer()),
            resource_rows(False), resource_rows(True), pressure_seconds=6, window_seconds=4,
            minimum_common_intervals=2)

    def test_complete_windows_use_only_matching_whole_intervals(self):
        result = self.compare()
        self.assertTrue(result['continuousProducerCoversBothWindows'])
        self.assertTrue(result['selectedMatchedThroughputBudgetPassed'])
        self.assertEqual(result['commonIntervals'], 2)
        self.assertEqual([row['producerSequence'] for row in result['pairedIntervals']], [2, 3])
        self.assertEqual(result['controlBoundaryIntervalsExcluded'], 4)
        self.assertNotIn('activeTraceThroughputBudgetPassed', result)

    def test_two_percent_throughput_loss_is_a_failure(self):
        after = producer()
        for row in after[1:]:
            row['readCalls'] = 401408
            row['readBytes'] = 401408 * 64
        result = self.compare(after=after)
        self.assertFalse(result['selectedMatchedThroughputBudgetPassed'])
        self.assertAlmostEqual(result['throughputRegressionPercent'], 2)

    def test_late_start_or_early_end_cannot_claim_full_coverage(self):
        for shift in (1500000000, -3000000000):
            rows = producer()
            for row in rows:
                row['wallNanos'] += shift
            with self.subTest(shift=shift), self.assertRaisesRegex(ValueError, 'complete resource window'):
                self.compare(after=rows)

    def test_missing_producer_or_invalid_resource_observation_fails(self):
        rows = producer()
        rows.pop()
        with self.assertRaises(ValueError):
            self.compare(after=rows)
        resources = resource_rows(True)
        resources[1]['groups']['node']['memoryEvents']['oom'] = 1
        with self.assertRaises(ValueError):
            compare_pressure(stream(producer()), stream(producer()), resource_rows(False), resources,
                             pressure_seconds=6, window_seconds=4, minimum_common_intervals=2)


if __name__ == '__main__':
    unittest.main()
