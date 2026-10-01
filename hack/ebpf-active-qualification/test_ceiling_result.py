import json
import unittest

from ceiling_result import evaluate_ceiling, evaluate_ceiling_session


def document(reason='event_limit', reported=True):
    return {'schemaVersion': 1, 'case': 'ceiling-observation', 'sameKernelClock': True,
            'observation': {'schemaVersion': 1, 'metadataMatched': True, 'transportComplete': True,
                'ceilingReported': reported, 'frames': 10, 'events': 8, 'encodedBytes': 5000,
                'bytesBeforeSummary': 4000, 'termination': reason, 'hookCoverageIncomplete': True,
                'counts': {key: None for key in ('produced', 'sampled', 'lost', 'rejected')},
                'clientClockDriftNanos': 1000}}


def evaluate(value, reason='event_limit', output=8 << 20, code=0):
    return evaluate_ceiling(json.dumps(value), expected_reason=reason, max_events=8,
                            max_output_bytes=output, exit_code=code)


class CeilingResultTests(unittest.TestCase):
    def test_session_requires_exact_readiness_then_final_observation(self):
        ready = json.dumps({'schemaVersion': 1, 'case': 'ceiling-ready', 'ready': True})
        terminal = json.dumps(document())
        kwargs = {'expected_reason': 'event_limit', 'max_events': 8, 'max_output_bytes': 8 << 20, 'exit_code': 0}
        result = evaluate_ceiling_session(ready + '\n' + terminal + '\n', **kwargs)
        self.assertTrue(result['requestedCeilingObserved'])
        self.assertTrue(result['readyBeforeResult'])
        for raw in (terminal + '\n', terminal + '\n' + ready + '\n', ready + '\n' + ready + '\n' + terminal + '\n',
                    ready.replace('true', 'false') + '\n' + terminal + '\n', ready + '\n' + terminal):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                evaluate_ceiling_session(raw, **kwargs)

    def test_unknown_counts_stay_unknown_and_do_not_produce_latency_or_loss(self):
        result = evaluate(document())
        self.assertTrue(result['requestedCeilingObserved'])
        self.assertEqual(result['engineCounts'], document()['observation']['counts'])
        self.assertNotIn('eventLossPercent', result)
        self.assertNotIn('p99', json.dumps(result))
        self.assertIn('resources', result['qualification'])

    def test_output_ceiling_preserves_terminal_reserve_and_unused_bytes(self):
        value = document('output_limit')
        result = evaluate(value, 'output_limit', output=9000)
        self.assertTrue(result['requestedCeilingObserved'])
        self.assertLess(result['encodedBytes'], 9000)
        wrong = evaluate(value, 'event_limit', output=9000)
        self.assertFalse(wrong['requestedCeilingObserved'])

    def test_expiry_is_complete_transport_but_not_a_ceiling_pass(self):
        result = evaluate(document('expired', False), code=1)
        self.assertTrue(result['transportComplete'])
        self.assertFalse(result['requestedCeilingObserved'])
        with self.assertRaisesRegex(ValueError, 'process exit disagree'):
            evaluate(document('expired', False), code=0)

    def test_partial_event_and_early_output_labels_remain_inconclusive(self):
        value = document('event_limit', False)
        value['observation'].update(events=7, frames=9)
        self.assertFalse(evaluate(value, code=1)['requestedCeilingObserved'])
        value = document('output_limit', False)
        self.assertFalse(evaluate(value, 'output_limit', code=1)['requestedCeilingObserved'])
        value['observation']['ceilingReported'] = True
        with self.assertRaises(ValueError):
            evaluate(value, 'output_limit')

    def test_frame_output_clock_and_bound_violations_cannot_pass(self):
        for change in ({'events': 9}, {'frames': 11}, {'encodedBytes': (8 << 20) + 1},
                       {'bytesBeforeSummary': 5000}, {'encodedBytes': 8097},
                       {'clientClockDriftNanos': 5000001}, {'metadataMatched': False},
                       {'transportComplete': False}, {'events': True}, {'ceilingReported': 1}, {'termination': []}):
            value = document()
            value['observation'].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                evaluate(value)
        with self.assertRaises(ValueError):
            evaluate(document(), output=6000)

    def test_partial_engine_counts_are_preserved_but_contradictions_fail(self):
        value = document()
        value['observation']['counts'].update(produced=10, lost=1)
        self.assertEqual(evaluate(value)['engineCounts'], {'produced': 10, 'sampled': None, 'lost': 1, 'rejected': None})
        value['observation']['counts']['produced'] = 8
        with self.assertRaisesRegex(ValueError, 'contradict'):
            evaluate(value)
        value['observation']['counts'] = {'produced': None, 'sampled': None, 'lost': True, 'rejected': None}
        with self.assertRaises(ValueError):
            evaluate(value)
        value['observation']['counts']['lost'] = 2**64 - 1
        with self.assertRaisesRegex(ValueError, 'overflows'):
            evaluate(value)

    def test_private_extra_or_missing_fields_are_rejected(self):
        for change in ('private', 'missing', 'normal-case', 'kernel', 'duplicate', 'oversize'):
            value = document()
            if change == 'private':
                value['observation']['path'] = '/private/path'
            elif change == 'missing':
                del value['observation']['counts']['lost']
            elif change == 'normal-case':
                value['case'] = 'normal'
            elif change == 'kernel':
                value['sameKernelClock'] = False
            raw = json.dumps(value)
            if change == 'duplicate':
                raw = raw.replace('"events": 8', '"events": 8, "events": 8')
            elif change == 'oversize':
                raw += ' ' * 65536
            with self.subTest(change=change), self.assertRaises(ValueError):
                evaluate_ceiling(raw, expected_reason='event_limit', max_events=8, max_output_bytes=8 << 20, exit_code=0)


if __name__ == '__main__':
    unittest.main()
