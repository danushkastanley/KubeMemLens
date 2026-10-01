from copy import deepcopy
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from storage_probe import StorageProbe, read_optional, READ_OPTIONAL
from test_storage_counters import IO, MOUNT

BOOT = '10000000-0000-0000-0000-000000000001'


def fixture():
    bound = {'identity': 'frozen', 'pid': 42, 'podName': 'target-child',
             'group': {'path': '/sys/fs/cgroup/owned', 'inode': 7, 'role': 'selected'}}
    runtime = Mock()
    case = SimpleNamespace(runtime=runtime, namespaces=('kml-active-a',), fixture=Mock(return_value=deepcopy(bound)))
    def execute(args):
        if args == ['cat', '/proc/sys/kernel/random/boot_id']:
            return BOOT.encode()
        if args == ['cat', '/proc/42/mountinfo']:
            return MOUNT.encode()
        if args[:2] == ['readlink', '-f']:
            return ('/sys/devices/block/sda' + ('/sda1' if args[-1].endswith('8:1') else '')).encode()
        if args[0] == 'stat':
            return b'90'
        if args[:3] == ['sh', '-ec', READ_OPTIONAL]:
            path = args[-1]
            sources = {'/sys/fs/cgroup/owned/io.stat': IO, '/sys/fs/cgroup/owned/cpu.stat': 'nr_throttled 0\n',
                       '/sys/dev/block/8:1/stat': '0 ' * 17, '/sys/dev/block/8:0/stat': '0 ' * 17,
                       '/sys/dev/block/8:1/partition': '1', '/sys/devices/block/sda/dev': '8:0'}
            return b'present\n' + sources[path].encode() if path in sources else b'absent\n'
        raise AssertionError(args)
    runtime.exec.side_effect = execute
    return case, {'kml-active-a/target-child': bound}


class StorageProbeTests(unittest.TestCase):
    def test_private_snapshot_binds_fixture_and_partition_parent_without_mutation(self):
        case, bindings = fixture()
        probe = StorageProbe(case, bindings)
        result = probe.snapshot()
        self.assertTrue(result['private'])
        self.assertEqual(result['devices']['8:1']['parentDevice'], '8:0')
        self.assertEqual(set(result['devices']), {'8:0', '8:1'})
        group = result['groups']['kml-active-a/target-child']
        self.assertEqual(group['identity'], 'frozen')
        self.assertEqual(group['cgroupInode'], 7)
        self.assertIsNone(group['pressureRaw'])
        self.assertEqual(group['io']['state'], 'observed')
        self.assertGreaterEqual(group['readEndedNanos'], group['readStartedNanos'])
        self.assertEqual(case.fixture.call_count, 3)
        self.assertTrue(all(call.args[0][0] in {'cat', 'sh', 'readlink', 'stat'} for call in case.runtime.exec.call_args_list))

    def test_changed_fixture_is_rejected_before_or_after_read(self):
        for boundary in ('before', 'after'):
            case, bindings = fixture()
            probe = StorageProbe(case, bindings)
            case.fixture.side_effect = [next(iter(bindings.values())), {}] if boundary == 'after' else [{}]
            with self.subTest(boundary=boundary), self.assertRaisesRegex(ValueError, 'fixture lifetime'):
                probe.snapshot()

    def test_foreign_and_duplicate_fixture_inventories_never_execute(self):
        case, bindings = fixture()
        for changed in ({}, {'foreign/target-child': next(iter(bindings.values()))},
                        {**bindings, 'kml-active-a/duplicate': next(iter(bindings.values()))}):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                StorageProbe(case, changed)
        case.fixture.assert_not_called()
        case.runtime.exec.assert_not_called()

    def test_reboot_or_block_device_replacement_during_read_fails(self):
        for change in ('boot', 'device'):
            case, bindings = fixture()
            probe = StorageProbe(case, bindings)
            execute = case.runtime.exec.side_effect
            reads = 0
            def changed(args):
                nonlocal reads
                if (change == 'boot' and args == ['cat', '/proc/sys/kernel/random/boot_id']) or (change == 'device' and args[0] == 'stat'):
                    reads += 1
                    if reads == 2:
                        return b'10000000-0000-0000-0000-000000000002' if change == 'boot' else b'91'
                return execute(args)
            case.runtime.exec.side_effect = changed
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'rebooted|replaced'):
                probe.snapshot()

    def test_optional_read_distinguishes_missing_empty_and_transport_error(self):
        runtime = Mock()
        for raw, expected in ((b'absent\n', None), (b'present\n', ''), (b'present\n0\n', '0\n')):
            runtime.exec.return_value = raw
            self.assertEqual(read_optional(runtime, '/fixed'), expected)
        for raw in (b'absent\n0', b'unknown\n', b'present\n' + b'x' * 65537):
            runtime.exec.return_value = raw
            with self.assertRaises(ValueError):
                read_optional(runtime, '/fixed')
        runtime.exec.side_effect = RuntimeError('permission denied')
        with self.assertRaises(RuntimeError):
            read_optional(runtime, '/fixed')


if __name__ == '__main__':
    unittest.main()
