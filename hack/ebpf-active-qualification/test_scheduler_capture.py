import copy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from bound_case import BoundCase
from local_case import canonical
from scheduler_capture import cpu_count, prepare_scheduler, replay_scheduler, paired_scheduler, INPUT, STREAM
from scheduler_test_fixture import BOOT, owner_binding, write_scheduler
from test_standard_window import rows as standard_rows


class SchedulerCaptureTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def window(self, phase):
        directory = self.root / phase
        directory.mkdir()
        receipt = {'completed': True, 'cleanupFailures': [], 'standardIdentities': {'agent': 'owned-agent'}}
        write_scheduler(directory, 2, receipt)
        (directory / 'window.private.json').write_bytes(canonical(receipt))
        (directory / 'standard.jsonl').write_text(''.join(json.dumps(row) + '\n' for row in standard_rows(2)))
        return directory, receipt

    def test_preparation_retains_exact_native_input_and_binds_owned_collector(self):
        runtime = Mock()
        runtime.exec.side_effect = lambda args: b'0-1\n' if args[-1].endswith('/online') else b'kernel tracepoint format\n'
        processes = Mock()
        processes.configuration.return_value = '/tmp/owned.json'
        result = prepare_scheduler(SimpleNamespace(runtime=runtime), processes, self.root, 2, BOOT, owner_binding())
        self.assertEqual(result, '/tmp/owned.json')
        value = processes.configuration.call_args.args[1]
        self.assertEqual((self.root / INPUT).read_bytes(), canonical(value))
        self.assertEqual(value['anchor'], {'pid': 42, 'start': 100})
        self.assertEqual(runtime.exec.call_count, 5)
        owner = owner_binding()
        owner['pid'] = 43
        with self.assertRaisesRegex(ValueError, 'identity hash'):
            prepare_scheduler(SimpleNamespace(runtime=runtime), processes, self.root, 2, BOOT, owner)
        self.assertEqual(processes.configuration.call_count, 1)

    def test_real_standard_binding_contract_is_accepted(self):
        case = object.__new__(BoundCase)
        case.node, case.owner = 'owned-node', 'owned'
        case.cfg = {'standardNamespace': 'owned-standard', 'standardImage': 'standard-image',
                    'release': 'owned-release', 'standardSHA256': {'collector': 'b' * 64}}
        case.namespace = Mock()
        pod = {'metadata': {'ownerReferences': [{'controller': True, 'kind': 'ReplicaSet',
                'name': 'owned-replica', 'uid': 'replica-uid'}]},
               'spec': {'nodeName': case.node, 'containers': [{'image': 'standard-image'}]}}
        deployment = {'metadata': {'uid': 'deployment-uid', 'annotations': {
            'meta.helm.sh/release-name': 'owned-release', 'meta.helm.sh/release-namespace': 'owned-standard'}},
            'spec': {'replicas': 1}}
        replica = {'metadata': {'uid': 'replica-uid', 'ownerReferences': [
            {'controller': True, 'kind': 'Deployment', 'uid': 'deployment-uid'}]}}
        case.json = Mock(side_effect=[{'items': [pod]}, deployment, replica])
        binding = owner_binding()
        for key in ('controllerUID', 'controllerSpecSHA256'):
            del binding[key]
        case.bind = Mock(return_value=binding)
        owner = case.standard('collector')
        case.runtime = Mock()
        case.runtime.exec.side_effect = lambda args: b'0-1\n' if args[-1].endswith('/online') else b'format\n'
        processes = Mock()
        prepare_scheduler(case, processes, self.root, 2, BOOT, owner)
        case.bind.assert_called_once_with(pod, 'collector', 'b' * 64)
        self.assertEqual(processes.configuration.call_args.args[1]['anchor'], {'pid': 42, 'start': 100})

    def test_matching_pairs_replay_and_changed_platform_cannot_pass(self):
        control, _ = self.window('control')
        enabled, _ = self.window('enabled')
        result = paired_scheduler(control, enabled, 2, {'bootID': BOOT})
        self.assertEqual(result['control']['completedWaits']['count'], 2)
        config = json.loads((enabled / INPUT).read_text())
        config['onlineCPUs'] = '2-3'
        (enabled / INPUT).write_bytes(canonical(config))
        with self.assertRaisesRegex(ValueError, 'bindings changed'):
            paired_scheduler(control, enabled, 2, {'bootID': BOOT})

    def test_wrong_owner_boot_duration_cleanup_or_clock_is_rejected(self):
        for index, change in enumerate(('anchor', 'identity', 'boot', 'duration', 'cleanup', 'clock', 'hash', 'missing-owner')):
            with self.subTest(change=change):
                directory, receipt = self.window(str(index))
                config = json.loads((directory / INPUT).read_text())
                if change == 'anchor': config['anchor']['pid'] += 1
                elif change == 'identity': receipt['standardIdentities']['collector'] = 'other'
                elif change == 'boot': config['bootID'] = '22222222-2222-2222-2222-222222222222'
                elif change == 'duration': config['seconds'] = 1
                elif change == 'cleanup': receipt['cleanupFailures'] = ['owned observer']
                elif change == 'hash': config['tracepointSHA256']['sched_switch'] = 'unbound'
                elif change == 'missing-owner': del receipt['schedulerOwner']
                else:
                    rows = [json.loads(line) for line in (directory / STREAM).read_text().splitlines()]
                    for row in rows:
                        for key in ('startedNanos', 'cutoffNanos', 'readStartedNanos', 'readEndedNanos'):
                            row[key] += 20_000_000_000
                    (directory / STREAM).write_text(''.join(json.dumps(row) + '\n' for row in rows))
                (directory / INPUT).write_bytes(canonical(config))
                (directory / 'window.private.json').write_bytes(canonical(receipt))
                with self.assertRaises(ValueError):
                    replay_scheduler(directory, 2, {'bootID': BOOT})

    def test_topology_bounds_are_not_silently_normalised(self):
        self.assertEqual(cpu_count('0-2,4,6-7'), 6)
        for value in ('', '0-64', '0,0', '2,1', '0-0', '-1', '4096', '0,,1', True):
            with self.subTest(value=value), self.assertRaises(ValueError):
                cpu_count(value)


if __name__ == '__main__':
    unittest.main()
