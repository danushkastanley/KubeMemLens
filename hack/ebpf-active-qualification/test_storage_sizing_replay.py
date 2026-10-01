from copy import deepcopy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from noisy_profile import load_noisy_profile
from storage_sizing_replay import application_observations, replay_sizing
from test_workload import series


def full_series(mode):
    source = [json.loads(row) for row in series().splitlines()]
    start, template = source[0], source[1]
    start['count'] = 1320
    rows = [start]
    for i in range(1320):
        row = deepcopy(template)
        due = start['firstDueNanos'] + i * start['periodNanos']
        row.update(sequence=i, dueMonotonicNanos=due)
        row['observation'].update(mode=mode, operationStartedMonotonicNanos=due + 1000000,
                                  operationEndedMonotonicNanos=due + 2000000)
        if mode == 'noise':
            row['observation'].update(readBytes=33554432, writeBytes=33554432)
        rows.append(row)
    return ''.join(json.dumps(row) + '\n' for row in rows)


class StorageSizingReplayTests(unittest.TestCase):
    def test_storage_fixture_substitution_is_rejected_before_reading_metrics(self):
        roles = ['selected', *[f'nonselected-{i}' for i in range(10)]]
        bindings = {f'owned/pod-{i}': {'identity': f'identity-{i}', 'group': {'inode': i + 1, 'role': role}}
                    for i, role in enumerate(roles)}
        before = {'bootID': 'boot', 'groups': {key: {'identity': row['identity'], 'role': row['group']['role'],
                                                   'cgroupInode': row['group']['inode']} for key, row in bindings.items()}}
        receipt = {'completed': True, 'cleanupFailures': [], 'phase': 'control', 'pair': 1, 'sessions': [],
                   'fixtureMapping': [{'mappedContainers': 32}],
                   'fixtureIdentities': {key: row['identity'] for key, row in bindings.items()}}
        mutations = [lambda row: row.update(bootID='reboot'),
                     lambda row: row['groups']['owned/pod-0'].update(role='nonselected-9'),
                     lambda row: row['groups']['owned/pod-0'].update(cgroupInode=99),
                     lambda row: row['groups']['owned/pod-0'].update(identity='other'),
                     lambda row: row['groups'].pop('owned/pod-10')]
        for change in mutations:
            after = deepcopy(before)
            change(after)
            with patch('storage_sizing_replay.verify_envelope', return_value={'bootID': 'boot'}), \
                 patch('storage_sizing_replay.read_document', return_value=receipt), \
                 patch('storage_sizing_replay.read_samples') as samples:
                with self.subTest(change=mutations.index(change)), self.assertRaises(ValueError):
                    replay_sizing(Path('/unused'), load_noisy_profile(), before, after, {'bootID': 'boot'}, bindings)
                samples.assert_not_called()

    def test_all_eleven_complete_streams_keep_application_bytes_separate(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'workload.jsonl').write_text(full_series('cached'))
            noise = full_series('noise')
            for i in range(10):
                (root / f'nonselected-{i}.jsonl').write_text(noise)
            result = application_observations(root, load_noisy_profile())
            self.assertEqual(len(result), 11)
            self.assertTrue(all(row['latency']['operations'] == 1320 for row in result.values()))
            self.assertEqual(result['selected']['writeBytes'], 0)
            self.assertEqual(result['nonselected-9']['writeBytes'], 1320 * 33554432)
            (root / 'nonselected-9.jsonl').write_text('\n'.join(noise.splitlines()[:-1]) + '\n')
            with self.assertRaisesRegex(ValueError, 'incomplete series'):
                application_observations(root, load_noisy_profile())

    def test_changed_envelope_incomplete_receipt_or_wrong_density_fail_before_metrics(self):
        receipt = {'completed': True, 'cleanupFailures': [], 'phase': 'control', 'pair': 1,
                   'sessions': [], 'fixtureMapping': [{'mappedContainers': 32}]}
        for mutate in (lambda row: row.update(completed=False), lambda row: row.update(sessions=[{}]),
                       lambda row: row.update(fixtureMapping=[{'mappedContainers': 31}])):
            changed = deepcopy(receipt)
            mutate(changed)
            with patch('storage_sizing_replay.verify_envelope', return_value={}), \
                 patch('storage_sizing_replay.read_document', return_value=changed), \
                 patch('storage_sizing_replay.read_samples') as samples:
                with self.assertRaises(ValueError):
                    replay_sizing(Path('/unused'), load_noisy_profile(), {}, {}, {}, {})
                samples.assert_not_called()
        with patch('storage_sizing_replay.verify_envelope', return_value={'boot': 'other'}):
            with self.assertRaisesRegex(ValueError, 'source/configuration/boot'):
                replay_sizing(Path('/unused'), load_noisy_profile(), {}, {}, {}, {})


if __name__ == '__main__':
    unittest.main()
