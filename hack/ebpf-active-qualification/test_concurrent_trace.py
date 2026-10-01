import copy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from concurrent_trace import ConcurrentTracePair
from profile import load_profile


CLOCK = {'wallNanos': 1790812800000000000, 'uncertaintyNanos': 1000}
EMPTY = {'workers': 0, 'activeControls': 0, 'excludedWorkers': 0,
         'objects': {'map': [], 'prog': [], 'link': []}, 'clock': CLOCK}
ATTACHED = {**EMPTY, 'workers': 2, 'activeControls': 2,
            'objects': {'map': [1, 2], 'prog': [3, 4], 'link': [5, 6]}}


class Clock:
    def __init__(self):
        self.now = 100.

    def monotonic(self):
        self.now += .001
        return self.now

    def sleep(self, seconds):
        self.now += seconds


class ConcurrentTraceTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.events = []
        self.clock = Clock()
        patcher = patch('concurrent_trace.time', self.clock)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.receivers = [Mock(), Mock()]
        for process in self.receivers:
            process.poll.return_value = 0
        admissions = Mock()
        admissions.pending = {0: '/first', 1: '/second'}
        admissions.create.side_effect = lambda i, intent: {'metadata': {'name': ('first', 'second')[i]}}
        admissions.get.side_effect = lambda i: (200, {
            'metadata': {'name': ('first', 'second')[i]}, 'state': 'active',
            'expiresAt': '2026-10-01T00:00:00Z'})
        admissions.probe_capacity.side_effect = lambda intent: self.events.append('probe') or {'thirdRequestCapacityDenied': True}
        profile = load_profile()
        profile['trace']['objects'] = {key: 2 for key in ('map', 'prog', 'link')}
        self.window = SimpleNamespace(case=Mock(), admissions=admissions, profile=profile,
            directory=Path(directory.name), processes=Mock(), sessions=[],
            targets=[{'podName': 'target-owned-a'}, {'podName': 'target-owned-b'}],
            owned={'map': {1, 2}, 'prog': {3, 4}, 'link': {5, 6}},
            snapshot=Mock(side_effect=[EMPTY, ATTACHED, EMPTY]), receiver_config=Mock(return_value={}))
        self.window.case.runtime.exec.return_value = json.dumps(CLOCK).encode()
        self.window.case.remaining.return_value = {'remaining': {'map': 0, 'prog': 0, 'link': 0}}
        self.window.processes.configuration.side_effect = lambda name, value: '/private/' + name
        self.window.processes.native.side_effect = self.start
        self.window.processes.accept_delivery_budget_failure.side_effect = lambda process: self.events.append('retained-budget-failure')
        self.write_delivery(0)
        self.write_delivery(1)

    def start(self, name, *args):
        self.events.append(name)
        return self.receivers[int(name[-1])]

    def write_delivery(self, target, *, passed=True, complete=True, received=None):
        value = {'sameKernelClock': True, 'observation': {'transportComplete': complete, 'hookCoverageIncomplete': True},
                 'latency': {'receivedEvents': target + 1 if received is None else received,
                             'eventDeliveryBudgetPassed': passed, 'normalLossBudgetPassed': True}}
        (self.window.directory / f'delivery-00-target-{target}.jsonl').write_text(json.dumps(value))

    def run_pair(self, due=100):
        return ConcurrentTracePair(self.window).run(0, due)

    def test_both_streams_start_before_probe_and_both_results_are_retained(self):
        result = self.run_pair()
        self.assertEqual(self.events, ['delivery-00-target-0', 'delivery-00-target-1', 'probe'])
        self.assertEqual([x['targetIndex'] for x in result['targets']], [0, 1])
        self.assertEqual([x['latency']['receivedEvents'] for x in result['targets']], [1, 2])
        self.assertNotEqual(result['targets'][0]['deliverySHA256'], result['targets'][1]['deliverySHA256'])
        self.assertTrue(result['zeroOwnedState'])
        self.window.admissions.cancel_all.assert_called_once()
        self.assertEqual(self.window.sessions, [result])
        self.assertEqual([call.args[1]['pod'] for call in self.window.admissions.create.call_args_list],
                         ['target-owned-a', 'target-owned-b'])
        self.assertEqual(self.window.admissions.probe_capacity.call_args.args[0]['pod'], 'target-owned-a')
        for process in self.receivers:
            process.wait.assert_not_called()

    def test_partial_second_creation_escapes_for_window_owned_cleanup(self):
        self.window.admissions.create.side_effect = [{'metadata': {'name': 'first'}}, TimeoutError('uncertain response')]
        with self.assertRaises(TimeoutError):
            self.run_pair()
        self.assertEqual(self.events, ['delivery-00-target-0'])
        self.window.admissions.probe_capacity.assert_not_called()
        self.assertEqual(self.window.sessions, [])

    def test_budget_failure_is_retained_without_becoming_a_success(self):
        self.receivers[0].poll.return_value = 1
        self.write_delivery(0, passed=False)
        result = self.run_pair()
        self.assertFalse(result['targets'][0]['latency']['eventDeliveryBudgetPassed'])
        self.assertTrue(result['targets'][1]['latency']['eventDeliveryBudgetPassed'])
        self.assertIn('retained-budget-failure', self.events)

    def test_incomplete_empty_or_inconsistent_delivery_invalidates_pair(self):
        for condition in ('incomplete', 'empty', 'exit-mismatch'):
            self.window.snapshot.side_effect = [EMPTY, ATTACHED, EMPTY]
            self.write_delivery(1, complete=condition != 'incomplete', received=0 if condition == 'empty' else 2)
            self.receivers[1].poll.return_value = 1 if condition == 'exit-mismatch' else 0
            with self.subTest(condition=condition), self.assertRaises(ValueError):
                self.run_pair(due=self.clock.now)
            self.assertEqual(self.window.sessions, [])

    def test_second_stream_cannot_extend_shared_deadline(self):
        self.receivers[1].poll.return_value = None
        with self.assertRaisesRegex(ValueError, 'shared deadline'):
            self.run_pair()
        self.assertLess(self.clock.now, 141)
        self.assertEqual(self.window.sessions, [])

    def test_complete_results_observed_after_shared_deadline_are_still_rejected(self):
        calls = 0

        def health_check():
            nonlocal calls
            calls += 1
            if calls == 3:
                self.clock.now = 141

        self.window.processes.healthy.side_effect = health_check
        with self.assertRaisesRegex(ValueError, 'shared deadline'):
            self.run_pair()
        self.assertEqual(self.window.sessions, [])

    def test_missed_slot_is_not_retimed_or_retried(self):
        with self.assertRaisesRegex(ValueError, 'slot missed'):
            self.run_pair(due=99)
        self.window.admissions.create.assert_not_called()

    def test_partial_or_excluded_attachment_cannot_start_capacity_probe(self):
        for kind in ('partial', 'excluded'):
            attached = copy.deepcopy(ATTACHED)
            if kind == 'partial':
                attached['objects']['link'].pop()
            else:
                attached['excludedWorkers'] = 1
            self.window.snapshot.side_effect = [EMPTY, attached]
            with self.subTest(kind=kind), self.assertRaisesRegex(ValueError, 'attachments not observed'):
                self.run_pair(due=self.clock.now)
            self.window.admissions.probe_capacity.assert_not_called()

    def test_residual_objects_cannot_produce_successful_session(self):
        self.window.snapshot.side_effect = [EMPTY, ATTACHED] + [EMPTY] * 100
        self.window.case.remaining.return_value = {'remaining': {'map': 1, 'prog': 0, 'link': 0}}
        with self.assertRaisesRegex(ValueError, 'did not clear'):
            self.run_pair()
        self.assertEqual(self.window.sessions, [])


if __name__ == '__main__':
    unittest.main()
