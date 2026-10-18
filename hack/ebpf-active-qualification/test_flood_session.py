import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import flood_session as session


class Clock:
    def __init__(self):
        self.value = 0.0

    def monotonic(self):
        return self.value

    def sleep(self, seconds):
        self.value += seconds


class Fixture:
    def __init__(self, directory, kind='event-limit', fault=None):
        self.directory, self.kind, self.fault = directory, kind, fault
        self.clock = Clock()
        self.active = False
        self.owned = {key: set() for key in session.OBJECTS}
        self.sessions, self.events, self.boot = [], [], 'fixture-boot'
        self.receiver = SimpleNamespace(returncode=None)
        self.receiver.poll = lambda: self.receiver.returncode
        self.receiver.wait = self.wait
        client = SimpleNamespace(namespace='kml-active-tenant', token='private-token', ca='private-ca')
        self.admissions = SimpleNamespace(pending={}, create=self.create, cancel_all=self.cancel,
                                          client=lambda index: client)
        self.processes = SimpleNamespace(configuration=self.configure, native=self.native,
                                         healthy=lambda: None, accept_delivery_budget_failure=self.accept)
        self.case = SimpleNamespace(node='lab-worker', namespaces=['kml-active-tenant'],
            runtime=SimpleNamespace(observer_network_scope=lambda: 'local', observer_server=lambda: 'https://lab-control-plane:6443',
                                    cfg={'engineSHA256': 'a' * 64, 'programmeIndexSHA256': 'b' * 64,
                                         'nodeUID': 'fixture-node'}),
            remaining=lambda objects: {'remaining': {key: 0 for key in session.OBJECTS}})
        self.targets = [{'podName': 'target-owned-child', 'podUID': 'fixture-pod', 'container': 'c' * 64, 'startedAt': '2026-10-01T00:00:00Z',
                         'group': {'inode': 10}}]
        self.workload = SimpleNamespace(process=SimpleNamespace(poll=lambda: None), run=self.burst,
                                        finish=lambda: self.events.append('finish'),
                                        close=lambda: self.events.append('close'))

    def create(self, index, intent):
        self.events.append('create')
        self.intent = intent
        self.admissions.pending[0] = '/fixture-session'
        return {'metadata': {'name': 'fixture-session'}, 'engineDigest': 'sha256:' + 'a' * 64}

    def cancel(self):
        self.events.append('cancel')
        if self.fault == 'slow-cleanup':
            self.clock.value += 2
        if self.fault == 'cleanup':
            raise ValueError('simulated cancellation failure')
        self.active = False
        self.admissions.pending.clear()

    def configure(self, name, value):
        self.events.append('configure')
        self.configuration = value
        return '/private-fixture-config'

    def native(self, name, helper, *args):
        self.events.append('receiver')
        self.path = self.directory / (name + '.jsonl')
        self.active = True
        mode = args[-1]
        ready = {'schemaVersion': 1, 'case': mode + '-ready', 'ready': True}
        if self.fault == 'readiness':
            ready['ready'] = False
        self.path.write_text(json.dumps(ready) + '\n')
        return self.receiver

    def snapshot(self):
        self.events.append('snapshot')
        objects = {key: list(range(1, count + 1)) if self.active else [] for key, count in session.OBJECTS.items()}
        if self.active and self.fault == 'attachments':
            objects['map'].pop()
        for key, ids in objects.items():
            self.owned[key].update(ids)
        return {'workers': int(self.active), 'activeControls': int(self.active), 'excludedWorkers': 0,
                'kernelMapBytes': 262144 if self.active else 0, 'userMapBytes': 540672 if self.active else 0,
                'objects': objects, 'clock': {'monotonicNanos': 1000000000,
                                             'wallNanos': 100000000000, 'uncertaintyNanos': 0}}

    def burst(self):
        self.events.append('burst')
        reason = 'output_limit' if self.kind == 'output-limit' else 'event_limit'
        output = self.kind == 'output-limit'
        value = {'schemaVersion': 1, 'metadataMatched': True, 'transportComplete': True,
                 'ceilingReported': True, 'frames': 102 if output else 10002,
                 'events': 100 if output else 10000, 'encodedBytes': 64000 if output else 6000000,
                 'bytesBeforeSummary': 61400 if output else 5998000, 'termination': reason,
                 'hookCoverageIncomplete': True, 'clientClockDriftNanos': 0,
                 'counts': {key: None for key in ('produced', 'sampled', 'lost', 'rejected')}}
        self.receiver.returncode = 0
        if self.kind in ('ring-flood', 'paused-reader'):
            value.update(termination='expired', ceilingReported=False, events=2929, frames=2931,
                         encodedBytes=538145, bytesBeforeSummary=537000,
                         counts={'produced': 131072, 'sampled': 121072, 'lost': 7071, 'rejected': 0})
            self.receiver.returncode = 1
        if self.fault == 'expiry':
            value.update(termination='expired', ceilingReported=False, events=10, frames=12,
                         encodedBytes=8000, bytesBeforeSummary=7000)
            self.receiver.returncode = 1
        document = {'schemaVersion': 1, 'case': 'ceiling-observation', 'sameKernelClock': True, 'observation': value}
        if self.kind == 'paused-reader':
            document.update(case='paused-reader-observation', pause={'requestedNanos': 5000000000,
                'startedUnixNano': 99000000000, 'endedUnixNano': 104000000000,
                'elapsedNanos': 5000000000, 'completed': True})
        with self.path.open('a') as stream:
            stream.write(json.dumps(document) + '\n')
        start = 6000000000 if self.fault == 'outside-pause' else 1100000000
        receipt = {'schemaVersion': 1, 'mode': 'flood', 'fileBytes': 8 << 20, 'readCalls': session.READ_CALLS,
                'bytesPerRead': 64, 'readBytes': 8 << 20, 'operationStartedMonotonicNanos': start,
                'operationEndedMonotonicNanos': start + 100000000, 'operationNanos': 100000000}
        if self.kind == 'event-limit':
            receipt.update(schemaVersion=2, mode='paced-flood', readCalls=12000, readBytes=768000,
                           readPeriodNanos=500000, operationNanos=6000000000,
                           operationEndedMonotonicNanos=start + 6000000000)
        return receipt

    def wait(self, timeout):
        if self.fault == 'late':
            self.clock.value += 41
        return self.receiver.returncode

    def accept(self, process):
        if process.returncode != 1:
            raise AssertionError('non-failing delivery accepted')
        self.events.append('budget-failure')

    def run(self):
        with patch.object(session.time, 'monotonic', self.clock.monotonic), patch.object(session.time, 'sleep', self.clock.sleep):
            return session.run_session(self, self.workload, self.kind, 0)


class FloodSessionTests(unittest.TestCase):
    def test_readiness_and_complete_attachments_precede_burst_in_each_case(self):
        for kind in session.CASES:
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as path:
                fixture = Fixture(Path(path), kind)
                result = fixture.run()
                self.assertTrue(result['completed'] and result['sessionChecksPassed'] and result['zeroOwnedState'])
                self.assertLess(fixture.events.index('receiver'), fixture.events.index('burst'))
                self.assertEqual(fixture.events[fixture.events.index('burst') - 1], 'snapshot')
                self.assertEqual(fixture.events[-1], 'close')
                self.assertFalse(fixture.admissions.pending)
                if kind == 'paused-reader':
                    self.assertTrue(result['burstFullyWithinPause'])

    def test_unmatched_readiness_or_attachments_never_release_workload(self):
        for fault in ('readiness', 'attachments'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as path:
                fixture = Fixture(Path(path), fault=fault)
                with self.assertRaises(ValueError):
                    fixture.run()
                self.assertNotIn('burst', fixture.events)
                self.assertIn('cancel', fixture.events)
                self.assertIn('close', fixture.events)

    def test_expiry_or_missing_pause_overlap_remains_a_recorded_non_pass(self):
        for kind, fault in (('event-limit', 'expiry'), ('paused-reader', 'outside-pause')):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as path:
                fixture = Fixture(Path(path), kind, fault)
                result = fixture.run()
                self.assertTrue(result['completed'] and result['zeroOwnedState'])
                self.assertFalse(result['sessionChecksPassed'])

    def test_late_receiver_and_cleanup_failure_cannot_pass(self):
        for fault in ('late', 'cleanup'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as path:
                fixture = Fixture(Path(path), fault=fault)
                with self.assertRaises(ValueError):
                    fixture.run()
                self.assertFalse(fixture.sessions[0]['completed'])
                self.assertIn('close', fixture.events)

    def test_exact_cleanup_budget_equality_remains_a_complete_failure(self):
        with tempfile.TemporaryDirectory() as path:
            result = Fixture(Path(path), fault='slow-cleanup').run()
            self.assertTrue(result['completed'] and result['zeroOwnedState'])
            self.assertEqual(result['cancelCleanupElapsedNanos'], 2000000000)
            self.assertFalse(result['cancelCleanupBudgetPassed'] or result['sessionChecksPassed'])


if __name__ == '__main__':
    unittest.main()
