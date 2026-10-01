import unittest

from verify_flood import validate_flood


def observation():
    return {'schemaVersion': 1, 'mode': 'flood', 'fileBytes': 8 << 20, 'readCalls': 131072,
            'bytesPerRead': 64, 'readBytes': 8 << 20, 'operationStartedMonotonicNanos': 100,
            'operationEndedMonotonicNanos': 1000000100, 'operationNanos': 1000000000}


class FloodObservationTests(unittest.TestCase):
    def test_fixed_burst_retains_exact_independent_calls_and_bytes(self):
        value = observation()
        self.assertEqual(validate_flood(value, 131072), value)

    def test_changed_bounds_missing_counters_or_deadlines_fail(self):
        for change in ({'readCalls': 131071}, {'readBytes': 1}, {'bytesPerRead': 128},
                       {'readCalls': True}, {'fileBytes': 1}, {'mode': 'cached'},
                       {'operationNanos': 10}, {'operationStartedMonotonicNanos': 0},
                       {'operationEndedMonotonicNanos': 10000000101, 'operationNanos': 10000000001}):
            value = observation()
            value.update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_flood(value, 131072)
        for count in (0, True, 262145):
            with self.subTest(count=count), self.assertRaises(ValueError):
                validate_flood(observation(), count)


if __name__ == '__main__':
    unittest.main()
