import json
import unittest

from paused_reader_result import evaluate_paused_reader_session
from test_ceiling_result import document


def paused_document():
    value = document()
    value['case'] = 'paused-reader-observation'
    value['pause'] = {'requestedNanos': 5000000000, 'startedUnixNano': 100000000000,
                      'endedUnixNano': 105000000000, 'elapsedNanos': 5000000000, 'completed': True}
    return value


def evaluate(value):
    ready = {'schemaVersion': 1, 'case': 'paused-reader-ready', 'ready': True}
    raw = json.dumps(ready) + '\n' + json.dumps(value) + '\n'
    return evaluate_paused_reader_session(raw, expected_reason='event_limit', max_events=8,
                                         max_output_bytes=8 << 20, exit_code=0)


class PausedReaderTests(unittest.TestCase):
    def test_complete_pause_retains_unknown_counts_without_saturation_claim(self):
        result = evaluate(paused_document())
        self.assertTrue(result['requestedCeilingObserved'])
        self.assertEqual(result['pause']['elapsedNanos'], 5000000000)
        self.assertIsNone(result['engineCounts']['lost'])
        self.assertIn('not established', result['backpressureEvidence'])

    def test_incomplete_short_or_clock_inconsistent_pause_cannot_pass(self):
        for change in ({'completed': False}, {'completed': 1}, {'elapsedNanos': 4999999999},
                       {'requestedNanos': 1}, {'startedUnixNano': True},
                       {'endedUnixNano': 104000000000}, {'elapsedNanos': 40000000001},
                       {'unexpected': 'private-value'}):
            value = paused_document()
            value['pause'].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                evaluate(value)

    def test_pause_cannot_replace_ceiling_or_identity_validation(self):
        for change in ({'transportComplete': False}, {'events': 9}, {'ceilingReported': False}):
            value = paused_document()
            value['observation'].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                evaluate(value)
        value = paused_document()
        value['sameKernelClock'] = False
        with self.assertRaises(ValueError):
            evaluate(value)


if __name__ == '__main__':
    unittest.main()
