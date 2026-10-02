import copy
import unittest

from standard_window import standard_window
from scans import compare_scans
from test_activity import COUNTS, witness
from test_standard_window import rows


def history_rows(seconds=8, duration=100):
    result = rows(seconds, duration)
    history = []
    for row in result:
        total = row['agent']['scanSuccess']
        if len(history) != total:
            history.append({'sequence': total, 'completedUnixNanos': row['agent']['scanCompletedUnixSeconds'] * 1000000000,
                            'durationNanos': duration, 'result': 'success'})
        row['schemaVersion'] = 2
        row['agentScans'] = copy.deepcopy(history[-32:])
    return result


def burst_rows():
    data = history_rows()
    hidden = {'sequence': 2, 'completedUnixNanos': 1700000001800000000,
              'durationNanos': 500000000, 'result': 'success'}
    for row in data[2:]:
        row['agent']['scanSuccess'] += 1
        row['agent']['postSuccess'] += 1
        row['collector']['results']['accepted'] += 1
        for scan in row['agentScans'][1:]:
            scan['sequence'] += 1
        row['agentScans'].insert(1, copy.deepcopy(hidden))
    return data


class ScanHistoryTest(unittest.TestCase):
    def test_every_back_to_back_scan_is_retained_with_its_actual_duration(self):
        value = standard_window(burst_rows(), 8)
        self.assertEqual(len(value['scans']), 5)
        self.assertEqual([s['pollIndex'] for s in value['scans']], [2, 2, 4, 6, 8])
        self.assertEqual([s['durationNanos'] for s in value['scans']], [500000000, 100, 100, 100, 100])
        first = value['scans'][0]
        self.assertEqual(first['earliestStartWallNanos'], 1700000001300000000)
        self.assertEqual(first['latestEndWallNanos'], 1700000001800000000)
        self.assertEqual(value['collectorMissingDurations'], 1)

    def test_the_same_burst_without_history_still_fails(self):
        data = burst_rows()
        for row in data:
            row['schemaVersion'] = 1
            del row['agentScans']
        with self.assertRaisesRegex(ValueError, 'missed between polls'):
            standard_window(data, 8)

    def test_history_eviction_between_polls_is_not_silently_accepted(self):
        data = history_rows()
        row = data[2]
        row['agent']['scanSuccess'] = 34
        row['agentScans'] = [{'sequence': seq, 'durationNanos': 100, 'result': 'success',
                             'completedUnixNanos': 1700000002000000000 - (34 - seq)} for seq in range(3, 35)]
        with self.assertRaisesRegex(ValueError, 'overwritten'):
            standard_window(data, 8)

    def test_retained_old_timings_cannot_change_even_when_latest_gauge_agrees(self):
        data = history_rows()
        data[4]['agentScans'][0]['durationNanos'] += 1
        with self.assertRaisesRegex(ValueError, 'previously observed'):
            standard_window(data, 8)

    def test_schema_sequence_clock_outcome_and_counter_disagreement_fail(self):
        mutations = {
            'missing': lambda r: r['agentScans'].pop(0),
            'sequence': lambda r: r['agentScans'][0].update(sequence=2),
            'boolean': lambda r: r['agentScans'][0].update(sequence=True),
            'future': lambda r: r['agentScans'][-1].update(completedUnixNanos=1800000000000000000),
            'reverse': lambda r: r['agentScans'][-1].update(completedUnixNanos=1600000000000000000),
            'duration': lambda r: r['agentScans'][-1].update(durationNanos=101),
            'negative': lambda r: r['agentScans'][-1].update(durationNanos=-1),
            'unknown': lambda r: r['agentScans'][-1].update(result='unknown'),
            'failure': lambda r: r['agentScans'][-1].update(result='failure'),
            'extra': lambda r: r['agentScans'][-1].update(node='private-node'),
            'mixed': lambda r: r.update(schemaVersion=1),
        }
        for name, change in mutations.items():
            data = history_rows()
            change(data[2])
            with self.subTest(name=name), self.assertRaises(ValueError):
                standard_window(data, 8)

    def test_failure_before_the_window_is_retained_but_not_counted_as_a_new_scan(self):
        data = history_rows()
        for row in data:
            row['agent']['scanSuccess'] -= 1
            row['agent']['scanFailure'] = 1
            row['agentScans'][0]['result'] = 'failure'
        self.assertEqual(len(standard_window(data, 8)['scans']), 4)

    def test_history_keeps_the_same_strict_scan_budget(self):
        args = dict(seconds=8, witness=witness(8), expected_objects=COUNTS,
                    minimum_scans=4, minimum_active_scans=3)
        self.assertTrue(compare_scans(history_rows(), history_rows(duration=104), **args)['normalScanBudgetPassed'])
        self.assertFalse(compare_scans(history_rows(), history_rows(duration=105), **args)['normalScanBudgetPassed'])

    def test_a_pair_cannot_mix_legacy_and_history_measurement_methods(self):
        with self.assertRaisesRegex(ValueError, 'paired scan observation schemas differ'):
            compare_scans(rows(), history_rows(), seconds=8, witness=witness(8), expected_objects=COUNTS,
                          minimum_scans=4, minimum_active_scans=3)


if __name__ == '__main__':
    unittest.main()
