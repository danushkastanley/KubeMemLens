import unittest

from deadline_diagnostic import validate_deadline_diagnostic


class DeadlineDiagnosticTests(unittest.TestCase):
    def fixture(self):
        value = {'type': 'series-deadline-failure', 'schemaVersion': 1, 'sequence': 4,
                 'waitStartedMonotonicNanos': 1000, 'wokeMonotonicNanos': 2200,
                 'cpuBeforeWaitNanos': 100, 'cpuAfterWakeNanos': 110, 'cpuAfterOperationNanos': 180}
        last = {'sequence': 4, 'dueMonotonicNanos': 2000,
                'observation': {'operationStartedMonotonicNanos': 2220, 'operationEndedMonotonicNanos': 2260}}
        return value, last

    def test_wakeup_and_setup_are_reported_separately_without_causal_claim(self):
        value, last = self.fixture()
        timing = validate_deadline_diagnostic(value, last, 100)
        self.assertEqual(timing['wakeLatenessNanos'], 200)
        self.assertEqual(timing['setupAfterWakeNanos'], 20)
        self.assertEqual(timing['operationNanos'], 40)
        self.assertEqual(timing['processCPUAcrossWaitNanos'], 10)
        self.assertNotIn('qualified', timing)
        value['wokeMonotonicNanos'] = 2000
        timing = validate_deadline_diagnostic(value, last, 100)
        self.assertEqual(timing['wakeLatenessNanos'], 0)
        self.assertEqual(timing['setupAfterWakeNanos'], 220)

    def test_missing_added_mismatched_or_reset_counters_fail(self):
        for change in ({'sequence': 5}, {'schemaVersion': True}, {'cpuAfterWakeNanos': 99},
                       {'cpuAfterOperationNanos': 100}, {'wokeMonotonicNanos': 2300},
                       {'waitStartedMonotonicNanos': 0}, {'privatePath': 'hidden'}, {'type': 'success'}):
            value, last = self.fixture()
            value.update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_deadline_diagnostic(value, last, 100)
        value, last = self.fixture()
        value.pop('cpuBeforeWaitNanos')
        with self.assertRaises(ValueError):
            validate_deadline_diagnostic(value, last, 100)

    def test_on_time_operation_cannot_be_labelled_as_a_missed_deadline(self):
        value, last = self.fixture()
        value['wokeMonotonicNanos'] = 2000
        last['observation'].update(operationStartedMonotonicNanos=2020, operationEndedMonotonicNanos=2060)
        with self.assertRaises(ValueError):
            validate_deadline_diagnostic(value, last, 100)


if __name__ == '__main__':
    unittest.main()
