from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from local_case import canonical, digest
from scheduler_test_fixture import BOOT, owner_binding
from test_verifier_window import samples
from verifier_capture import INPUT, STREAM, prepare_verifier, paired_verifier, replay_verifier
from verifier_probe_lease import JOURNAL, definitions


def owner_for(phase):
    owner = owner_binding()
    if phase == 'enabled':
        for key in ('controllerUID', 'controllerSpecSHA256'):
            del owner[key]
        owner['group']['role'] = 'node'
        owner['group']['path'] = '/sys/fs/cgroup/owned-node'
    owner['identity'] = digest(canonical({key: value for key, value in owner.items() if key != 'identity'}))
    return owner


class VerifierCaptureTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def window(self, name, phase):
        directory = self.root / name; directory.mkdir()
        owner = owner_for(phase)
        config = {'owner': 'a' * 32, 'seconds': 3, 'bootID': BOOT, 'onlineCPUs': '0-1',
                  'btfSHA256': 'd' * 64, 'anchor': {key: owner[key] for key in ('pid', 'start', 'sha256')},
                  'group': {key: owner['group'][key] for key in ('path', 'inode')}}
        rows = samples()
        for row in rows:
            row['binding'].update(owner=config['owner'], btfSHA256=config['btfSHA256'],
                                  anchorSHA256=owner['sha256'], cgroupInode=owner['group']['inode'])
            if phase == 'control':
                row['observation'] = {key: 0 for key in row['observation']}
                row['perf']['events'] = 0
        receipt = {'completed': True, 'cleanupFailures': [], 'phase': phase, 'verifierCleanup': True,
                   'verifierOwner': owner, 'standardIdentities': {'collector': owner['identity']},
                   'traceNodeOwnerIdentity': owner['identity']}
        journal = {'schemaVersion': 1, 'owner': config['owner'], 'helper': '/usr/local/bin/kml-verifier-test',
                   'expectedDefinitions': definitions(config['owner']), 'initialOwnedRegistryEmpty': True}
        for path, value in ((INPUT, config), (JOURNAL, journal), ('window.private.json', receipt)):
            (directory / path).write_bytes(canonical(value))
        (directory / STREAM).write_bytes(b''.join(canonical(row) + b'\n' for row in rows))
        (directory / 'standard.jsonl').write_bytes(b''.join(canonical({'clock': {
            'monotonicNanos': row['cutoffNanos'] + 1000, 'wallNanos': row['cutoffNanos'],
            'uncertaintyNanos': 0}, 'readNanos': 1000}) + b'\n' for row in rows))
        return directory, config, receipt

    def test_pair_reports_distinct_negative_control_and_observed_enabled_scope(self):
        control, _, _ = self.window('control', 'control')
        enabled, _, _ = self.window('enabled', 'enabled')
        result = paired_verifier(control, enabled, 3, {'bootID': BOOT})
        self.assertEqual(result['control']['totals']['completedCalls'], 0)
        self.assertEqual(result['enabled']['totals']['completedCalls'], 3)
        self.assertIn('no cross-scope subtraction', result['scope'])

    def test_missing_or_changed_binding_and_cleanup_cannot_qualify(self):
        for index, change in enumerate(('owner', 'phase', 'boot', 'duration', 'group', 'cleanup', 'source')):
            with self.subTest(change=change):
                directory, config, receipt = self.window(str(index), 'enabled')
                if change == 'owner': receipt['traceNodeOwnerIdentity'] = 'other'
                elif change == 'phase': receipt['phase'] = 'control'
                elif change == 'boot': config['bootID'] = '2' * 36
                elif change == 'duration': config['seconds'] = 2
                elif change == 'group': config['group']['inode'] += 1
                elif change == 'cleanup': receipt['verifierCleanup'] = False
                else: config['btfSHA256'] = 'e' * 64
                (directory / INPUT).write_bytes(canonical(config))
                (directory / 'window.private.json').write_bytes(canonical(receipt))
                with self.assertRaises(ValueError): replay_verifier(directory, 3, {'bootID': BOOT}, 'enabled')

    def test_preparation_writes_journal_before_native_configuration_and_strips_flags(self):
        runtime = Mock()
        runtime.exec.side_effect = lambda args: (('d' * 64 + '  /sys/kernel/btf/vmlinux\n').encode()
            if args[0] == 'sha256sum' else b'0-1\n' if args[-1].endswith('/online') else b'')
        processes = Mock()
        def configured(name, value):
            self.assertTrue((self.root / JOURNAL).exists())
            self.assertEqual(name, 'verifier')
            self.assertEqual((self.root / INPUT).read_bytes(), canonical(value))
            return '/tmp/input.json'
        processes.configuration.side_effect = configured
        case = SimpleNamespace(runtime=runtime, cfg={'helpers': {'verifier': {'path': '/usr/local/bin/kml-verifier-test'}}})
        owner = owner_for('enabled'); owner['flags'] = ['private', 'runtime', 'binding']
        path, lease, bound = prepare_verifier(case, processes, self.root, 3, BOOT, owner, 'enabled')
        self.assertEqual(path, '/tmp/input.json')
        self.assertNotIn('flags', bound)
        self.assertFalse(lease.closed)
        self.assertFalse(any(args.args[0][0] == 'sh' for args in runtime.exec.call_args_list))


if __name__ == '__main__':
    unittest.main()
