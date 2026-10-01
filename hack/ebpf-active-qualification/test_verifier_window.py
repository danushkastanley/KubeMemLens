import copy
import json
from pathlib import Path
import tempfile
import unittest

from verifier_window import read_verifier_samples, verifier_window


def samples(seconds=3):
    rows = []
    binding = {'owner': 'a' * 32, 'btfSHA256': 'b' * 64, 'anchorSHA256': 'c' * 64,
               'cgroupInode': 42, 'formatSHA256': {role: 'd' * 64 for role in ('enter', 'return', 'log')}}
    for index in range(seconds + 1):
        cutoff = 1_000_000_000 + index * 1_000_000_000
        rows.append({'schemaVersion': 1, 'index': index, 'startedNanos': 1_000_000_000,
                     'cutoffNanos': cutoff, 'readStartedNanos': cutoff + 110_000_000,
                     'readEndedNanos': cutoff + 111_000_000, 'observerCPUUsec': index * 100,
                     'observerPeakRSSBytes': 123456, 'startupDiscarded': 0,
                     'perf': {'events': index * 3, 'enabledNanos': index * 600,
                              'runningNanos': index * 600, 'minimumEnabledNanos': index * 100,
                              'lostSamples': 0, 'descriptors': 6},
                     'observation': {'completedCalls': index, 'rejectedCalls': 0, 'logFailures': 0,
                                     'durationNanos': index * 100, 'maximumNanos': 100 if index else 0,
                                     'finalizedLogBytes': index * 98, 'maximumLogBytes': 98 if index else 0,
                                     'pendingCalls': 0, 'unassociatedLogs': 0},
                     'probeCounts': {role: {'hits': index, 'missed': 0} for role in ('enter', 'return', 'log')},
                     'binding': copy.deepcopy(binding), 'captureClosed': index == seconds,
                     'closeStartedNanos': cutoff + 112_000_000 if index == seconds else 0,
                     'closeEndedNanos': cutoff + 115_000_000 if index == seconds else 0})
    return rows


class VerifierWindowTests(unittest.TestCase):
    def replay(self, rows):
        return verifier_window(rows, 3, 2, samples()[0]['binding'])

    def test_positive_numeric_calls_and_resource_cost(self):
        result = self.replay(samples())
        self.assertEqual(result['totals']['completedCalls'], 3)
        self.assertEqual(result['totals']['finalizedLogBytes'], 294)
        self.assertEqual(result['observer']['cpuUsecDelta'], 300)
        self.assertEqual([row['durationNanos'] for row in result['intervals']], [100] * 3)

    def test_zero_idle_context_time_is_valid_but_not_positive_calibration(self):
        rows = samples()
        for row in rows:
            for key in row['observation']:
                row['observation'][key] = 0
            for key in row['perf']:
                if key != 'descriptors':
                    row['perf'][key] = 0
            for value in row['probeCounts'].values():
                value['hits'] = 0
        self.assertEqual(self.replay(rows)['totals']['completedCalls'], 0)

    def test_final_unassociated_and_startup_records_remain_explicit(self):
        rows = samples()
        for row in rows:
            row['startupDiscarded'] = 2
            row['perf']['events'] += 2
            row['perf']['enabledNanos'] += 1
            row['perf']['runningNanos'] += 1
        rows[-1]['observation']['unassociatedLogs'] = 1
        rows[-1]['perf']['events'] += 1
        self.assertEqual(self.replay(rows)['totals']['unassociatedLogs'], 1)

    def test_missing_cleanup_loss_missed_or_unaccounted_samples_fail(self):
        changes = [('captureClosed', False), ('closeEndedNanos', 1)]
        for key, value in changes:
            with self.subTest(key=key):
                rows = samples(); rows[-1][key] = value
                with self.assertRaises(ValueError): self.replay(rows)
        for branch, key, value in [('perf', 'lostSamples', 1), ('perf', 'events', 10),
                                    ('perf', 'runningNanos', 0), ('perf', 'descriptors', 3),
                                    ('observation', 'pendingCalls', 1)]:
            with self.subTest(branch=branch, key=key):
                rows = samples(); rows[-1][branch][key] = value
                with self.assertRaises(ValueError): self.replay(rows)
        rows = samples(); rows[-1]['probeCounts']['log']['missed'] = 1
        with self.assertRaises(ValueError): self.replay(rows)

    def test_bindings_schema_and_clock_cannot_drift(self):
        for branch, key, value in [(None, 'schemaVersion', True), (None, 'index', True),
                                   (None, 'cutoffNanos', 10), (None, 'readEndedNanos', 10),
                                   ('binding', 'owner', 'e' * 32), ('binding', 'cgroupInode', 43),
                                   ('binding', 'anchorSHA256', 'e' * 64)]:
            with self.subTest(branch=branch, key=key):
                rows = samples(); (rows[1][branch] if branch else rows[1])[key] = value
                with self.assertRaises(ValueError): self.replay(rows)

    def test_counter_resets_and_impossible_maximum_fail(self):
        for branch, key, value in [('observation', 'durationNanos', 99),
                                   ('observation', 'maximumNanos', 99),
                                   ('observation', 'finalizedLogBytes', 97),
                                   ('observation', 'rejectedCalls', 4),
                                   ('observation', 'logFailures', True),
                                   ('perf', 'minimumEnabledNanos', 301)]:
            with self.subTest(key=key):
                rows = samples(); rows[-1][branch][key] = value
                with self.assertRaises(ValueError): self.replay(rows)
        rows = samples(); rows[1]['observation']['pendingCalls'] = 2; rows[1]['perf']['events'] += 2
        with self.assertRaisesRegex(ValueError, 'disappeared'): self.replay(rows)

    def test_completed_calls_can_span_samples_without_disappearing(self):
        rows = samples()
        row = rows[1]
        row['observation'] = {key: 0 for key in row['observation']}
        row['observation']['pendingCalls'] = 1
        row['perf']['events'] = 1
        result = self.replay(rows)
        self.assertEqual(result['intervals'][0]['pendingAtEnd'], 1)
        self.assertEqual(result['intervals'][1]['completedCalls'], 2)

    def test_incomplete_duplicate_or_oversized_stream_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'rows.jsonl'
            path.write_text(''.join(json.dumps(row) + '\n' for row in samples()))
            self.assertEqual(len(read_verifier_samples(path)), 4)
            for raw in [b'{"index":0,"index":1}\n', b'{}', b' ' * 8192 + b'\n']:
                path.write_bytes(raw)
                with self.assertRaises(ValueError): read_verifier_samples(path)
        with self.assertRaises(ValueError): self.replay(samples()[:-1])


if __name__ == '__main__':
    unittest.main()
