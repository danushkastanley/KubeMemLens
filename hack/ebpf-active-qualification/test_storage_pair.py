from copy import deepcopy
import unittest

from storage_pair import compare_storage, rate_bounds
from storage_counters import block_statistics, io_statistics
from storage_probe import StorageProbe
from test_storage_probe import fixture


def pair():
    case, bindings = fixture()
    before = StorageProbe(case, bindings).snapshot()
    after = deepcopy(before)
    for row, start in ((before, 0), (after, 10000000000)):
        row.update(readStartedNanos=start, readEndedNanos=start + 1000000000)
        for group in row['groups'].values():
            group.update(readStartedNanos=start, readEndedNanos=start + 1000000000)
    for group in after['groups'].values():
        group['ioRaw'] = group['ioRaw'].replace('wbytes=4096', 'wbytes=8192')
        group['io'] = io_statistics(group['ioRaw'])
    for device in after['devices'].values():
        fields = ['0'] * 17
        fields[4], fields[6] = '2', '16'
        device['raw'] = ' '.join(fields)
        device['statistics'] = block_statistics(device['raw'])
    return before, after


class StoragePairTests(unittest.TestCase):
    def test_integer_rate_bounds_include_read_uncertainty_and_sector_units(self):
        before, after = pair()
        result = compare_storage(before, after)
        self.assertEqual(result['groups']['kml-active-a/target-child']['devices']['8:1']['wbytes'], 4096)
        self.assertEqual(result['devices']['8:1']['readWriteBytesPerSecond'], {'lower': 744, 'upper': 911})
        self.assertEqual(result['devices']['8:1']['parentDevice'], '8:0')
        self.assertEqual(rate_bounds(0, (1, 2)), {'lower': 0, 'upper': 0})

    def test_reboots_mount_replacement_clock_overlap_and_projection_tampering_fail(self):
        mutations = [lambda row: row.update(bootID='reboot'), lambda row: row.update(observerID='other'),
                     lambda row: row.update(readStartedNanos=0),
                     lambda row: row['groups']['kml-active-a/target-child'].update(identity='replaced'),
                     lambda row: row['groups']['kml-active-a/target-child'].update(readStartedNanos=1),
                     lambda row: row['groups']['kml-active-a/target-child']['mount'].update(device='8:9'),
                     lambda row: row['groups']['kml-active-a/target-child']['io']['devices']['8:1'].update(wbytes=9999),
                     lambda row: row['devices']['8:1'].update(sysfsInode=91)]
        for mutate in mutations:
            before, after = pair()
            mutate(after)
            with self.subTest(mutation=mutations.index(mutate)), self.assertRaises(ValueError):
                compare_storage(before, after)

    def test_unavailable_and_new_device_stay_unproven(self):
        before, after = pair()
        first = before['groups']['kml-active-a/target-child']
        first.update(ioRaw='', io=io_statistics(''))
        result = compare_storage(before, after)
        self.assertEqual(result['groups']['kml-active-a/target-child']['state'], 'unproven')
        before['devices']['8:1'] = {'raw': None, 'statistics': block_statistics(None)}
        self.assertEqual(compare_storage(before, after)['devices']['8:1']['state'], 'unproven')
        del after['devices']['8:0']
        self.assertEqual(compare_storage(before, after)['state'], 'unproven')


if __name__ == '__main__':
    unittest.main()
