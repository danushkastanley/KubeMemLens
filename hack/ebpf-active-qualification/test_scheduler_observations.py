import unittest

from scheduler_observations import scheduler_observations


def rows():
    return [{'elapsedNanos': index * 1000000000,
             'node': {'scheduling': {'state': 'observed', 'cpus': 2, 'version': 17,
                                     'runtimeNanos': 100 * index, 'runqueueWaitNanos': 60 * index,
                                     'timeslices': 3 * index}},
             'groups': {'selected': {'process': {'scheduling': {
                 'state': 'observed', 'cohort': 'bound-cohort', 'runtimeNanos': 100 * index,
                 'runqueueWaitNanos': 60 * index, 'timeslices': 3 * index}}}}} for index in range(4)]


class SchedulerObservationTests(unittest.TestCase):
    def test_complete_counter_coverage_reports_ratio_without_quantiles(self):
        result = scheduler_observations(rows())
        self.assertTrue(result['completeCounterCoverage'])
        self.assertEqual(result['observedNanos'], 3000000000)
        self.assertEqual(result['observedTotals'], {'runtimeNanos': 300, 'runqueueWaitNanos': 180, 'timeslices': 9})
        self.assertEqual(result['meanRunqueueWaitPerTimeslice'], {'numeratorNanos': 180, 'denominatorTimeslices': 9})
        self.assertEqual(result['latencyPercentiles']['state'], 'unavailable')

    def test_disabled_and_unsupported_are_disclosed_without_zero_totals(self):
        samples = rows()
        for sample, state in zip(samples, ('unsupported', 'disabled', 'unavailable', 'unsupported')):
            sample['node']['scheduling'] = {'state': state}
        result = scheduler_observations(samples)
        self.assertEqual(result['sampleStates'], {'disabled': 1, 'unavailable': 1, 'unsupported': 2})
        self.assertEqual(result['intervalStates'], {'unavailable': 3})
        self.assertEqual(result['observedNanos'], 0)
        self.assertIsNone(result['observedTotals'])
        self.assertIsNone(result['meanRunqueueWaitPerTimeslice'])
        self.assertFalse(result['completeCounterCoverage'])

    def test_resets_and_cpu_changes_preserve_gap_counts(self):
        samples = rows()
        samples[1]['node']['scheduling']['runtimeNanos'] = 999
        samples[3]['node']['scheduling']['cpus'] = 3
        result = scheduler_observations(samples)
        self.assertEqual(result['intervalStates'], {'cohort-changed': 1, 'counter-reset': 1, 'observed': 1})
        self.assertEqual(result['observedNanos'], 1000000000)
        self.assertFalse(result['completeCounterCoverage'])

    def test_task_replacement_cannot_count_as_zero_delay_or_complete_coverage(self):
        samples = rows()
        samples[2]['groups']['selected']['process']['scheduling']['cohort'] = 'replacement'
        result = scheduler_observations(samples, 'selected')
        self.assertEqual(result['scope'], 'stable-observed-task-cohorts')
        self.assertEqual(result['intervalStates'], {'cohort-changed': 2, 'observed': 1})
        self.assertEqual(result['observedTotals']['timeslices'], 3)

    def test_no_timeslice_increment_has_no_mean_and_observed_zero_wait_is_retained(self):
        samples = rows()
        for row in samples:
            row['node']['scheduling']['runqueueWaitNanos'] = 0
        self.assertEqual(scheduler_observations(samples)['meanRunqueueWaitPerTimeslice']['numeratorNanos'], 0)
        for row in samples:
            row['node']['scheduling']['timeslices'] = 0
        result = scheduler_observations(samples)
        self.assertTrue(result['completeCounterCoverage'])
        self.assertIsNone(result['meanRunqueueWaitPerTimeslice'])
        self.assertEqual(result['observedTotals']['runqueueWaitNanos'], 0)


if __name__ == '__main__':
    unittest.main()
