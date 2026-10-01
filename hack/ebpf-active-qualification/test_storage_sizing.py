import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from fixture_roster import noise_targets
from noisy_profile import load_noisy_profile
from storage_sizing import StorageSizing, EKSStorageSizing, main


def runner(directory):
    run = StorageSizing.__new__(StorageSizing)
    run.cfg = {'sourceSHA256': 'a' * 64}
    run.profile = load_noisy_profile()
    run.directory = Path(directory)
    run.case = SimpleNamespace(runtime=Mock(), namespaces=('kml-active-a', 'kml-active-b'))
    run.fixtures = None
    run.record = Mock()
    run.progress = Mock()
    run.invariant = Mock()
    events = []
    run.restore = Mock(side_effect=lambda: events.append('restore'))
    fixtures = Mock()
    fixtures.prepare.side_effect = lambda profile: events.append('prepare')
    fixtures.cleanup.side_effect = lambda: events.append('cleanup')
    fixtures.pod_name.side_effect = lambda namespace, name: name + '-child'
    roster = [{'namespace': run.case.namespaces[0], 'name': 'target', 'role': 'selected'}]
    roster += noise_targets(run.profile, run.case.namespaces)
    fixtures.bindings = {row['namespace'] + '/' + row['name'] + '-child': {'group': {'role': row['role']}}
                         for row in roster}
    probe = Mock(boot='boot', bindings=fixtures.bindings)
    def snapshot():
        events.append('snapshot')
        return {'bootID': 'boot'}
    probe.snapshot.side_effect = snapshot
    window = Mock()
    window.run.side_effect = lambda: events.append('window')
    return run, fixtures, probe, window, events


class StorageSizingTests(unittest.TestCase):
    def execute(self, run, fixtures, probe, window):
        with patch('storage_sizing.Fixtures', return_value=fixtures), \
             patch('storage_sizing.StorageProbe', return_value=probe) as constructor, \
             patch('storage_sizing.Window', return_value=window) as control, \
             patch('storage_sizing.wait_until'), \
             patch('storage_sizing.replay_sizing', return_value={'private': True, 'qualification': 'none'}):
            run.run()
            return constructor, control

    def test_complete_fixed_control_is_bracketed_and_result_follows_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            run, fixtures, probe, window, events = runner(directory)
            constructor, control = self.execute(run, fixtures, probe, window)
            self.assertEqual(events, ['prepare', 'snapshot', 'window', 'snapshot', 'restore', 'cleanup'])
            self.assertEqual(set(row['group']['role'] for row in constructor.call_args.args[1].values()),
                             {'selected', *[f'nonselected-{i}' for i in range(10)]})
            control.assert_called_once_with(run.case, fixtures, load_noisy_profile(), Path(directory) / 'control', 'control', 1)
            runtime = run.case.runtime
            self.assertEqual([call.args for call in runtime.scale.call_args_list], [('api', 0), ('node', 0)])
            runtime.stopped.assert_called_once()
            runtime.absent.assert_called_once()
            result = json.loads((Path(directory) / 'storage-sizing-result.private.json').read_text())
            self.assertTrue(result['ownedServicesRestored'])
            self.assertTrue(result['ownedFixtureCleanupComplete'])
            self.assertEqual(result['qualification'], 'none')
            self.assertEqual(set(result['storageSnapshotSHA256']), {'before', 'after', 'bindings'})

    def test_workload_failure_retains_before_snapshot_and_restores_before_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            run, fixtures, probe, window, events = runner(directory)
            window.run.side_effect = RuntimeError('workload failed')
            with self.assertRaisesRegex(RuntimeError, 'workload failed'):
                self.execute(run, fixtures, probe, window)
            self.assertTrue((Path(directory) / 'storage-before.private.json').exists())
            self.assertFalse((Path(directory) / 'storage-sizing-result.private.json').exists())
            self.assertEqual(events[-2:], ['restore', 'cleanup'])
            self.assertEqual(probe.snapshot.call_count, 1)

    def test_cleanup_failure_never_emits_completed_result(self):
        for target in ('restore', 'cleanup'):
            with self.subTest(target=target), tempfile.TemporaryDirectory() as directory:
                run, fixtures, probe, window, events = runner(directory)
                action = run.restore if target == 'restore' else fixtures.cleanup
                action.side_effect = RuntimeError('cleanup failed')
                with self.assertRaisesRegex(RuntimeError, 'cleanup failed'):
                    self.execute(run, fixtures, probe, window)
                fixtures.cleanup.assert_called_once()
                self.assertFalse((Path(directory) / 'storage-sizing-result.private.json').exists())
                self.assertTrue((Path(directory) / 'storage-comparison.private.json').exists())

    def test_provider_sizing_keeps_explicit_provider_case(self):
        run = EKSStorageSizing.__new__(EKSStorageSizing)
        with patch('storage_sizing.EKSCase') as provider, patch('campaign.LocalCase') as local:
            run.make_case({'explicit': 'config'})
            provider.assert_called_once_with({'explicit': 'config'})
            local.assert_not_called()

    def test_cli_requires_explicit_mode_and_retains_whole_run_alarm(self):
        for flag, cls in (('--acknowledge-local-storage-sizing', 'StorageSizing'),
                          ('--acknowledge-owned-eks-storage-sizing', 'EKSStorageSizing')):
            args = ['storage_sizing.py', '--config', '/private/input', '--output', '/private/new', flag]
            with patch('sys.argv', args), patch('storage_sizing.os.umask'), patch('storage_sizing.signal.signal'), \
                 patch('storage_sizing.signal.alarm') as alarm, patch('storage_sizing.read_configuration', return_value={}), \
                 patch('storage_sizing.' + cls) as run:
                run.return_value.run.side_effect = RuntimeError('failed')
                with self.assertRaises(RuntimeError):
                    main()
                run.assert_called_once_with({}, Path('/private/new'))
                self.assertEqual([call.args[0] for call in alarm.call_args_list], [3000, 0])
        with patch('sys.argv', ['storage_sizing.py', '--config', '/private/input', '--output', '/private/new']), \
             patch('sys.stderr'), patch('storage_sizing.read_configuration') as read:
            with self.assertRaises(SystemExit):
                main()
            read.assert_not_called()


if __name__ == '__main__':
    unittest.main()
