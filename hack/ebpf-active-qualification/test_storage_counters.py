from copy import deepcopy
import unittest

from storage_counters import io_statistics, block_statistics, work_mount, io_delta, device_number

IO = '8:1 rbytes=512 wbytes=4096 rios=1 wios=8 dbytes=0 dios=0\n'
MOUNT = '31 22 8:1 /owned/pod/volume /work rw,relatime - ext4 /dev/sda1 rw\n'


class StorageCounterTests(unittest.TestCase):
    def test_missing_empty_and_observed_zero_remain_distinct(self):
        self.assertEqual(io_statistics(None), {'state': 'unavailable'})
        self.assertEqual(io_statistics(' \n'), {'state': 'empty'})
        zero = io_statistics('8:1 rbytes=0 wbytes=0 rios=0 wios=0 dbytes=0 dios=0')
        self.assertEqual(zero['state'], 'observed')
        self.assertEqual(zero['devices']['8:1']['wbytes'], 0)
        for unknown in (io_statistics(None), io_statistics('')):
            self.assertEqual(io_delta(unknown, zero)['state'], 'unproven')

    def test_duplicate_missing_invalid_and_reset_counters_fail(self):
        cases = [IO + IO, IO.replace('rios=1', 'rbytes=1'), IO.replace(' rios=1', ''),
                 IO.replace('rbytes=512', 'rbytes=-1'), IO.replace('wios=8', 'wios=18446744073709551616')]
        for raw in cases:
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                io_statistics(raw)
        before = io_statistics(IO)
        after = deepcopy(before)
        after['devices']['8:1']['rbytes'] -= 1
        with self.assertRaisesRegex(ValueError, 'reset'):
            io_delta(before, after)

    def test_per_device_delta_does_not_treat_extra_gauges_as_counters(self):
        before = io_statistics(IO.strip() + ' depth=5\n')
        after = io_statistics(IO.replace('wbytes=4096', 'wbytes=8192').strip() + ' depth=2\n')
        result = io_delta(before, after)
        self.assertEqual(result['devices']['8:1']['wbytes'], 4096)
        self.assertNotIn('depth', result['devices']['8:1'])
        self.assertEqual(io_delta(before, io_statistics(IO.replace('8:1', '8:2')))['state'], 'unproven')

    def test_device_numbers_cannot_inject_paths_or_aliases(self):
        self.assertEqual(device_number('259:12'), '259:12')
        for value in ('../stat', '08:1', '8:-1', '8:1/other', '4294967296:1'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                device_number(value)

    def test_block_sector_units_and_optional_fields_are_explicit(self):
        for count in (11, 15, 17):
            result = block_statistics(' '.join(str(i) for i in range(count)))
            self.assertEqual(result['sectorBytes'], 512)
            self.assertEqual(result['counters']['readSectors'], 2)
            self.assertEqual('flushIOs' in result['counters'], count == 17)
        self.assertEqual(block_statistics(None), {'state': 'unavailable'})
        for raw in ('', '0 ' * 12, 'x ' * 17):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                block_statistics(raw)

    def test_mount_requires_the_actual_work_volume_without_root_fallback(self):
        mount = work_mount(MOUNT)
        self.assertEqual(mount['device'], '8:1')
        self.assertEqual(mount['filesystem'], 'ext4')
        self.assertEqual(work_mount(MOUNT.replace(' rw,relatime - ', ' rw,relatime shared:9 - ')), mount)
        for raw in ('', MOUNT.replace('/work ', '/ '), MOUNT + MOUNT, MOUNT.replace(' - ', ' ')):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                work_mount(raw)


if __name__ == '__main__':
    unittest.main()
