import copy
import json
from pathlib import Path
import tempfile
import unittest

from scheduler_window import percentile_bounds, scheduler_window, read_scheduler_samples


def samples():
    result = []
    for index in range(3):
        origin = 1_000_000_000
        cutoff = origin + index * 1_000_000_000
        buckets = [0] * 65
        buckets[4] = index  # Each completed wait is exactly 10ns, in [8,15].
        enabled = (index + 1) * 1_000_000_000
        result.append({'schemaVersion': 2, 'index': index, 'startedNanos': origin, 'cutoffNanos': cutoff,
                       'readStartedNanos': cutoff + 100_000_000, 'readEndedNanos': cutoff + 101_000_000,
                       'observerCPUUsec': index * 5000, 'observerPeakRSSBytes': 1_000_000,
                       'startupDiscarded': 2, 'perf': {'enabledNanos': enabled * 8, 'runningNanos': enabled * 8,
                                                      'lostSamples': 0, 'minimumEnabledNanos': enabled},
                       'observation': {'completedWaits': {'buckets': buckets, 'count': index,
                                                         'sumNanos': index * 10, 'maximumNanos': 10 if index else 0},
                                       'events': index * 3, 'unmatchedSwitchIns': 0, 'replacedEnqueues': 0,
                                       'exitedPending': 0, 'pending': 0}})
    return result


class SchedulerWindowTests(unittest.TestCase):
    def test_full_window_reports_ranges_and_exact_mean_without_raw_identities(self):
        result = scheduler_window(samples(), 2, 2)
        self.assertEqual(result['completedWaits']['count'], 2)
        self.assertEqual(result['completedWaits']['percentiles']['p99'], {'lowerNanos': 8, 'upperNanos': 15})
        self.assertEqual(result['completedWaits']['meanNanos'], {'numerator': 20, 'denominator': 2})
        self.assertEqual(result['observer']['cpuUsecDelta'], 10000)
        self.assertEqual(len(result['intervals']), 2)

    def test_reader_rejects_partial_duplicate_and_oversized_records(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'samples.jsonl'
            path.write_text(''.join(json.dumps(row) + '\n' for row in samples()))
            self.assertEqual(read_scheduler_samples(path), samples())
            for raw in (b'{}', b' ' * 8192 + b'\n', b'{"index":0,"index":1}\n', b'{}\n' * 1802):
                path.write_bytes(raw)
                with self.subTest(raw=raw[:24]), self.assertRaises(ValueError):
                    read_scheduler_samples(path)

    def test_missing_old_or_invalid_evidence_cannot_qualify(self):
        changes = [lambda r: r.pop(), lambda r: r[1].update(schemaVersion=1),
                   lambda r: r[1].update(index=True), lambda r: r[1].update(startedNanos=2),
                   lambda r: r[1].update(readStartedNanos=r[1]['cutoffNanos'] + 99_000_000),
                   lambda r: r[1].update(readEndedNanos=r[1]['readStartedNanos'] + 50_000_001),
                   lambda r: r[1]['perf'].update(lostSamples=1), lambda r: r[1]['perf'].update(runningNanos=1),
                   lambda r: r[1]['perf'].update(minimumEnabledNanos=9_000_000_000),
                   lambda r: r[1]['observation']['completedWaits'].update(count=2),
                   lambda r: r[1]['observation']['completedWaits'].update(sumNanos=9),
                   lambda r: r[1]['observation']['completedWaits'].update(maximumNanos=16),
                   lambda r: r[2]['observation']['completedWaits'].update(sumNanos=15),
                   lambda r: r[2].update(observerCPUUsec=0),
                   lambda r: r[1]['observation'].update(pending=32769),
                   lambda r: r[1].update(taskName='private')]
        for change in changes:
            rows = samples()
            change(rows)
            with self.subTest(change=changes.index(change)), self.assertRaises(ValueError):
                scheduler_window(rows, 2, 2)

    def test_unmatched_replaced_and_pending_are_explicit_not_synthesised_waits(self):
        rows = samples()
        for index in (1, 2):
            rows[index]['observation'].update(events=index * 6, unmatchedSwitchIns=index,
                                              replacedEnqueues=index, pending=1)
        result = scheduler_window(rows, 2, 2)
        self.assertEqual(result['coverage']['unmatchedSwitchIns'], 2)
        self.assertEqual(result['coverage']['replacedEnqueues'], 2)
        self.assertEqual(result['pendingAtEnd'], 1)
        self.assertEqual(result['completedWaits']['count'], 2)

    def test_empty_intervals_have_no_invented_percentile(self):
        rows = samples()
        for row in rows:
            row['observation']['completedWaits'] = copy.deepcopy(rows[0]['observation']['completedWaits'])
        result = scheduler_window(rows, 2, 2)
        self.assertEqual(result['completedWaits']['state'], 'empty')
        self.assertIsNone(result['completedWaits']['meanNanos'])
        self.assertIsNone(result['completedWaits']['percentiles']['p95'])
        with self.assertRaises(ValueError):
            percentile_bounds([0] * 65, 0)


if __name__ == '__main__':
    unittest.main()
