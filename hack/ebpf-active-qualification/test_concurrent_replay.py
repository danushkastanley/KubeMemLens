import copy
import json
from pathlib import Path
import tempfile
import unittest

from concurrent_profile import load_concurrent_profile
from evaluate import evaluate_pair
from local_case import digest
from provenance import write_envelope
from test_concurrent_activity import COMBINED, simultaneous
from test_samples import fixture
from test_standard_window import rows as standard_rows
from test_workload import series
from scheduler_test_fixture import BOOT, write_scheduler
from verifier_test_fixture import write_verifier


class ConcurrentReplayTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        # Short synthetic streams exercise replay failures, not the frozen live
        # campaign. The public loader separately requires the full schedule.
        self.profile = load_concurrent_profile()
        self.profile.update(windowSeconds=12, minimumActiveSeconds=1, minimumActiveOperations=5,
                            minimumScans=1, minimumActiveScans=1)
        self.profile['trace'].update(count=1, objects=COMBINED)
        self.profile['workload']['count'] = 5
        self.cfg = {'sourceSHA256': 'a' * 64}
        self.receipts = {}
        for phase in ('control', 'enabled'):
            directory = self.root / phase
            directory.mkdir()
            samples = fixture(12)
            for row in samples:
                for group in row['groups'].values():
                    group['cpu']['usage_usec'] = 1000 + row['index'] * 1000
                for role in ('agent', 'collector', 'selected', 'selected-peer'):
                    row['groups'][role] = copy.deepcopy(row['groups']['node'])
                if phase == 'control':
                    del row['groups']['node'], row['groups']['api']
            self.write_rows(directory / 'resources.jsonl', samples)
            self.write_rows(directory / 'standard.jsonl', standard_rows(12))
            for target in (0, 1):
                (directory / f'workload-target-{target}.jsonl').write_text(series())
            self.receipts[phase] = {
                'completed': True, 'cleanupFailures': [], 'sessions': [],
                'fixtureIdentities': {'a/target': 'first', 'b/target': 'second',
                                      **{f'a/passive-{i}': f'passive-{i}' for i in range(30)}},
                'standardIdentities': {'agent': 'agent', 'collector': 'collector'},
                'fixtureMapping': [{'mappedContainers': 32}],
                'targetBindings': [{'targetIndex': 0, 'witnessOrdinal': 1, 'identity': 'first'},
                                   {'targetIndex': 1, 'witnessOrdinal': 0, 'identity': 'second'}]}
        enabled = self.root / 'enabled'
        self.write_rows(enabled / 'witness.jsonl', simultaneous(12))
        targets = []
        for index in (0, 1):
            raw = {'sameKernelClock': True, 'observation': {'transportComplete': True, 'hookCoverageIncomplete': True},
                   'latency': {'receivedEvents': 10 + index, 'normalLossBudgetPassed': True, 'eventDeliveryBudgetPassed': True}}
            path = enabled / f'delivery-00-target-{index}.jsonl'
            path.write_text(json.dumps(raw))
            targets.append({'targetIndex': index, 'latency': raw['latency'], 'deliverySHA256': digest(path.read_bytes()),
                            'attachUpperNanos': 100, 'deadlineTeardownUpperNanos': 100})
        self.receipts['enabled']['sessions'] = [{'index': 0, 'zeroOwnedState': True,
            'capacityProbe': {'thirdRequestCapacityDenied': True, 'bothAdmissionsActiveBeforeAndAfter': True},
            'targets': targets}]

    def write_rows(self, path, rows):
        path.write_text(''.join(json.dumps(row) + '\n' for row in rows))

    def replay(self):
        for phase in ('control', 'enabled'):
            directory = self.root / phase
            write_scheduler(directory, self.profile['windowSeconds'], self.receipts[phase])
            write_verifier(directory, self.profile['windowSeconds'], self.receipts[phase], phase)
            (directory / 'window.private.json').write_text(json.dumps(self.receipts[phase]))
            write_envelope(directory, phase, 1, self.profile, self.cfg, BOOT)
        return evaluate_pair(self.root / 'control', self.root / 'enabled', self.profile)

    def test_two_streams_and_both_cgroups_reach_the_complete_replay(self):
        result = self.replay()
        self.assertTrue(result['measuredNormalBudgetsPassed'])
        self.assertEqual(set(result['workload']['workloads']), {'target-0', 'target-1'})
        self.assertIn('selected-peer', result['memory']['controlWorkingSetFloorBytes'])
        self.assertIn('remaining observations', result['qualification'])

    def test_second_workload_regression_fails_overall_replay(self):
        (self.root / 'enabled/workload-target-1.jsonl').write_text(series(1020000))
        result = self.replay()
        self.assertFalse(result['measuredNormalBudgetsPassed'])
        self.assertTrue(result['workload']['workloads']['target-0']['normalSelectedLatencyBudgetPassed'])
        self.assertFalse(result['workload']['workloads']['target-1']['normalSelectedLatencyBudgetPassed'])

    def test_second_receiver_budget_failure_is_preserved(self):
        path = self.root / 'enabled/delivery-00-target-1.jsonl'
        raw = json.loads(path.read_text())
        raw['latency']['normalLossBudgetPassed'] = False
        path.write_text(json.dumps(raw))
        target = self.receipts['enabled']['sessions'][0]['targets'][1]
        target.update(latency=raw['latency'], deliverySHA256=digest(path.read_bytes()))
        result = self.replay()
        self.assertFalse(result['normalEventBudgetsPassed'])
        self.assertFalse(result['measuredNormalBudgetsPassed'])

    def test_second_cgroup_cannot_be_omitted(self):
        path = self.root / 'enabled/resources.jsonl'
        rows = [json.loads(line) for line in path.read_text().splitlines()]
        for row in rows:
            del row['groups']['selected-peer']
        self.write_rows(path, rows)
        with self.assertRaises(ValueError):
            self.replay()

    def test_forged_session_summary_cannot_override_raw_delivery(self):
        self.receipts['enabled']['sessions'][0]['targets'][1]['latency']['receivedEvents'] = 100
        with self.assertRaisesRegex(ValueError, 'retained stream'):
            self.replay()

    def test_changed_ordinal_or_target_identity_invalidates_replay(self):
        self.receipts['enabled']['targetBindings'][1]['witnessOrdinal'] = 1
        with self.assertRaisesRegex(ValueError, 'target identities'):
            self.replay()

    def test_unproven_capacity_denial_cannot_pass_replay(self):
        self.receipts['enabled']['sessions'][0]['capacityProbe']['bothAdmissionsActiveBeforeAndAfter'] = False
        with self.assertRaisesRegex(ValueError, 'capacity probe'):
            self.replay()

    def test_earlier_expiry_teardown_is_not_hidden_by_other_target(self):
        self.receipts['enabled']['sessions'][0]['targets'][0]['deadlineTeardownUpperNanos'] = 2000000000
        result = self.replay()
        self.assertFalse(result['deadlineCleanupBudgetPassed'])
        self.assertFalse(result['measuredNormalBudgetsPassed'])


if __name__ == '__main__':
    unittest.main()
