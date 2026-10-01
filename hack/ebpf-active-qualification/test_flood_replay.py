from copy import deepcopy
from pathlib import Path
import tempfile
import unittest

from flood_replay import replay_session
from test_flood_session import Fixture


class FloodReplayTests(unittest.TestCase):
    def test_each_observed_mechanism_replays_without_relabelling_writer_limits(self):
        for kind in ('event-limit', 'output-limit', 'ring-flood', 'paused-reader'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as path:
                fixture = Fixture(Path(path), kind)
                record = fixture.run()
                value = replay_session(record, fixture.path.read_text(), record['workload'])
                self.assertTrue(value['sessionChecksPassed'])
                if kind in ('ring-flood', 'paused-reader'):
                    self.assertFalse(value['observation']['requestedCeilingObserved'])

    def test_complete_failures_stay_failures_after_replay(self):
        for kind, fault in (('event-limit', 'expiry'), ('paused-reader', 'outside-pause'),
                            ('event-limit', 'slow-cleanup')):
            with tempfile.TemporaryDirectory() as path:
                fixture = Fixture(Path(path), kind, fault)
                record = fixture.run()
                self.assertFalse(replay_session(record, fixture.path.read_text(), record['workload'])['sessionChecksPassed'])

    def test_changed_stream_workload_attachment_and_verdict_are_rejected(self):
        with tempfile.TemporaryDirectory() as path:
            fixture = Fixture(Path(path), 'paused-reader')
            original = fixture.run()
            raw = fixture.path.read_text()
            mutations = []
            value = deepcopy(original)
            value['deliverySHA256'] = '0' * 64
            mutations.append(value)
            value = deepcopy(original)
            value['attachmentWitness']['objects']['link'].pop()
            mutations.append(value)
            value = deepcopy(original)
            value['burstFullyWithinPause'] = False
            mutations.append(value)
            value = deepcopy(original)
            value['cancelCleanupBudgetPassed'] = False
            mutations.append(value)
            value = deepcopy(original)
            value['kernelBudget']['admittedCandidates'] = 1
            mutations.append(value)
            value = deepcopy(original)
            value['workload']['readCalls'] = 1
            mutations.append(value)
            value = deepcopy(original)
            value['cleanupWitness']['remaining']['link'] = 1
            mutations.append(value)
            value = deepcopy(original)
            value['cleanupWitness']['capturedObjects']['map'].pop()
            mutations.append(value)
            for record in mutations:
                with self.assertRaises(ValueError):
                    replay_session(record, raw, original['workload'])


if __name__ == '__main__':
    unittest.main()
