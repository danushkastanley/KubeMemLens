from pathlib import Path
import json
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from pressure_session import run_pressure_session, saturation
from test_flood_session import Fixture


def prepared(path, kind='ring-pressure', fault=None):
    fixture = Fixture(path, 'paused-reader' if kind == 'paused-pressure' else 'ring-flood', fault)
    fixture.pressure = SimpleNamespace(process=SimpleNamespace(poll=lambda: None))
    fixture.receiver_config = lambda admission: {}
    fixture.admissions.get = lambda index: (200, {'state': 'active', 'metadata': {'name': 'fixture-session'},
                                                'expiresAt': '1970-01-01T00:02:10Z'})
    original_snapshot = fixture.snapshot
    def snapshot():
        value = original_snapshot()
        value['clock'] = {'monotonicNanos': int((10 + fixture.clock.value) * 1000000000),
                          'wallNanos': int((100 + fixture.clock.value) * 1000000000), 'uncertaintyNanos': 0}
        return value
    fixture.snapshot = snapshot
    def wait(timeout):
        fixture.burst()  # Test-only producer of the native client's numeric output.
        if fault == 'unknown-counts':
            lines = fixture.path.read_text().splitlines()
            value = json.loads(lines[1])
            value['observation']['counts']['lost'] = None
            fixture.path.write_text(lines[0] + '\n' + json.dumps(value) + '\n')
        fixture.clock.value += 30
        return fixture.receiver.returncode
    fixture.receiver.wait = wait
    return fixture


def execute(fixture, kind='ring-pressure'):
    requested = {'monotonicNanos': 7000000000, 'wallNanos': 97000000000, 'uncertaintyNanos': 0}
    with patch('pressure_session.time.monotonic', fixture.clock.monotonic), \
         patch('pressure_session.time.sleep', fixture.clock.sleep), \
         patch('pressure_session.clock_ns', return_value=requested):
        return run_pressure_session(fixture, kind, 0)


class PressureSessionTests(unittest.TestCase):
    def test_live_admission_and_observed_ring_loss_are_required(self):
        for kind in ('ring-pressure', 'paused-pressure'):
            with tempfile.TemporaryDirectory() as path:
                fixture = prepared(Path(path), kind)
                result = execute(fixture, kind)
                self.assertTrue(result['completed'] and result['sessionChecksPassed'])
                self.assertTrue(result['saturation']['ringLossObserved'])
                self.assertFalse(result['observation']['requestedCeilingObserved'])
                self.assertEqual(result['activeExpiresAtNanos'], 130000000000)
                self.assertEqual(result['deadlineCleanupUpperNanos'], 0)
                self.assertEqual(result['cleanupWitness']['remaining'], {'map': 0, 'prog': 0, 'link': 0})

    def test_missing_active_admission_is_not_waived_for_a_fast_stream(self):
        with tempfile.TemporaryDirectory() as path:
            fixture = prepared(Path(path))
            fixture.admissions.get = lambda index: (404, {})
            with self.assertRaisesRegex(ValueError, 'active pressure admission unavailable'):
                execute(fixture)
            self.assertIn('cancel', fixture.events)
            self.assertNotIn('burst', fixture.events)
            self.assertFalse(fixture.sessions[0]['completed'])

    def test_unknown_counter_or_residual_cleanup_does_not_pass(self):
        for fault in ('unknown-counts', 'cleanup'):
            with tempfile.TemporaryDirectory() as path:
                fixture = prepared(Path(path), fault=fault)
                with self.assertRaises(ValueError):
                    execute(fixture)
                self.assertFalse(fixture.sessions[0]['completed'])

    def test_two_second_cleanup_equality_is_a_retained_failure(self):
        with tempfile.TemporaryDirectory() as path:
            result = execute(prepared(Path(path), fault='slow-cleanup'))
            self.assertTrue(result['completed'])
            self.assertFalse(result['sessionChecksPassed'])
            self.assertFalse(result['cancelCleanupBudgetPassed'])
            self.assertFalse(result['deadlineCleanupBudgetPassed'])

    def test_reported_rejections_remain_in_exact_flood_accounting(self):
        observed = {'transportComplete': True, 'termination': 'expired', 'events': 2953,
                    'engineCounts': {'produced': 45792138, 'sampled': 45782138,
                                     'lost': 7046, 'rejected': 1}}
        result = saturation(observed, 10000)
        self.assertTrue(result['saturationObserved'])
        self.assertTrue(result['knownCandidatesAccounted'])
        self.assertEqual(observed['engineCounts']['rejected'], 1)
        for rejected in (0, 2):
            observed['engineCounts']['rejected'] = rejected
            self.assertFalse(saturation(observed, 10000)['saturationObserved'])
        for rejected in (None, -1, True):
            observed['engineCounts']['rejected'] = rejected
            with self.assertRaises(ValueError):
                saturation(observed, 10000)

    def test_rejections_alone_do_not_establish_ring_saturation(self):
        observed = {'transportComplete': True, 'termination': 'expired', 'events': 0,
                    'engineCounts': {'produced': 20000, 'sampled': 10000,
                                     'lost': 0, 'rejected': 10000}}
        result = saturation(observed, 10000)
        self.assertTrue(result['knownCandidatesAccounted'])
        self.assertFalse(result['saturationObserved'])

    def test_candidate_cap_alone_does_not_establish_saturation(self):
        observed = {'transportComplete': True, 'termination': 'expired', 'events': 10000,
                    'engineCounts': {'produced': 10000, 'sampled': 0, 'lost': 0, 'rejected': 0}}
        self.assertFalse(saturation(observed, 10000)['saturationObserved'])


if __name__ == '__main__':
    unittest.main()
