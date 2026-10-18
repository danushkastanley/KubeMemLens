import copy
import unittest

from standard_metrics import AGENT_FIELDS, RESULTS
from standard_window import standard_window
from scans import compare_scans
from test_activity import COUNTS, witness


def rows(seconds=8, duration=100):
    result = []
    for index in range(seconds + 1):
        completed = index // 2
        agent = {key: 0 for key in AGENT_FIELDS.values()}
        agent.update(scanSuccess=1 + completed, postSuccess=1 + completed,
                     scanCompletedUnixSeconds=1700000000 + completed * 2 if completed else 1699999999,
                     scanDurationNanos=duration, found=32, mapped=32, metadataCachePods=32)
        counts = {key: 0 for key in RESULTS}
        counts['accepted'] = 1 + completed
        result.append({'schemaVersion': 1, 'index': index, 'elapsedNanos': index * 1000000000,
                       'readNanos': 20000000,
                       'clock': {'wallNanos': 1700000000000000000 + index * 1000000000 + 20000000,
                                 'monotonicNanos': 1000000000 + index * 1000000000 + 20000000,
                                 'uncertaintyNanos': 1000},
                       'agent': agent, 'collector': {'durationNanos': duration, 'results': counts},
                       'observerCPUUsec': 1000 + index * 100, 'observerPeakRSSBytes': 1000000})
    return result


class StandardWindowTests(unittest.TestCase):
    def test_duplicates_are_not_scans_and_coarse_completion_is_preserved(self):
        result = standard_window(rows(), 8)
        self.assertEqual(len(result['scans']), 4)
        self.assertEqual([r['pollIndex'] for r in result['scans']], [2, 4, 6, 8])
        first = result['scans'][0]
        self.assertEqual(first['earliestStartWallNanos'], 1700000002000000000 - 100)
        self.assertEqual(first['latestEndWallNanos'], 1700000003000000000)
        self.assertEqual(result['collectorMissingDurations'], 0)

    def test_collector_missing_durations_remain_explicit(self):
        data = rows()
        for row in data[2:]:
            row['collector']['results']['accepted'] += 2
        result = standard_window(data, 8)
        self.assertEqual(result['collectorMissingDurations'], 2)
        self.assertEqual(result['ingestions'][0]['newRequests'], 3)
        self.assertEqual(result['ingestions'][0]['knownLatestDurationNanos'], 100)
        self.assertNotIn('collectorP95Nanos', result)

    def test_missing_reset_failed_changed_or_misaligned_observations_fail(self):
        for failure in ('missing', 'jump', 'gauge', 'scan-failure', 'post-failure', 'collector-failure',
                        'collector-gauge', 'collector-reset', 'time', 'monotonic', 'read', 'identity', 'future'):
            data = rows()
            if failure == 'missing':
                data.pop()
            elif failure == 'jump':
                data[2]['agent']['scanSuccess'] += 1
            elif failure == 'gauge':
                data[1]['agent']['scanDurationNanos'] += 1
            elif failure == 'scan-failure':
                data[1]['agent']['scanFailure'] += 1
            elif failure == 'post-failure':
                data[1]['agent']['postFailure'] += 1
            elif failure == 'collector-failure':
                data[1]['collector']['results']['identity_rejected'] += 1
            elif failure == 'collector-gauge':
                data[1]['collector']['durationNanos'] += 1
            elif failure == 'collector-reset':
                data[1]['collector']['results']['accepted'] = 0
            elif failure == 'time':
                data[1]['elapsedNanos'] += 200000000
            elif failure == 'monotonic':
                data[1]['clock']['monotonicNanos'] += 200000000
            elif failure == 'read':
                data[1]['readNanos'] = 100000001
            elif failure == 'identity':
                data[1]['pid'] = 123
            else:
                data[2]['agent']['scanCompletedUnixSeconds'] += 10
            with self.subTest(failure=failure), self.assertRaises(ValueError):
                standard_window(data, 8)

    def test_scan_gate_is_strict_and_excludes_unproven_attachments(self):
        args = dict(seconds=8, witness=witness(8), expected_objects=COUNTS,
                    minimum_scans=4, minimum_active_scans=3)
        result = compare_scans(rows(), rows(duration=104), **args)
        self.assertTrue(result['normalScanBudgetPassed'])
        self.assertEqual(result['fullyBracketedScans']['scans'], 3)
        self.assertEqual(result['unprovenOrTransitionScans'], 1)
        self.assertFalse(compare_scans(rows(), rows(duration=105), **args)['normalScanBudgetPassed'])
        changed = copy.deepcopy(args)
        changed['witness'][4]['snapshot']['objects']['link'] = [100, 101, 102, 103, 104]
        with self.assertRaises(ValueError):
            compare_scans(rows(), rows(), **changed)
        changed = copy.deepcopy(args)
        changed['minimum_scans'] = 5
        with self.assertRaises(ValueError):
            compare_scans(rows(), rows(), **changed)


if __name__ == '__main__':
    unittest.main()
