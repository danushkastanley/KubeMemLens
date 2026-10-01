from copy import deepcopy
from pathlib import Path
import tempfile
import unittest

from pressure_profile import load_pressure_profile
from pressure_replay import producer_bounds, replay_pressure_session
from test_activity import witness
from test_pressure_observation import records, stream
from test_pressure_session import execute, prepared
from verify_pressure import validate_pressure


def producer_and_witness():
    rows = records(40)
    for row in rows:
        row['wallNanos'] += 95000000000 - 1700000000000000000
        for key in list(row):
            if key != 'wallNanos' and ('Monotonic' in key or key.startswith('monotonic')):
                row[key] += 4000000000
        if 'readCalls' in row:
            row.update(readCalls=4096, readBytes=4096 * 64)
    producer = validate_pressure(stream(rows), 40)
    activity = witness(40)
    for row in activity:
        for point in (row['clock'], row['snapshot']['clock']):
            point['wallNanos'] += 95000000000 - 1700000000000000000
            point['monotonicNanos'] += 4000000000
        snapshot = row['snapshot']
        snapshot['objects']['map'].pop()
        if not 5 <= row['index'] < 35:
            snapshot.update(workers=0, activeControls=0, kernelMapBytes=0, userMapBytes=0,
                            objects={'map': [], 'prog': [], 'link': []})
    return producer, activity


class PressureReplayTests(unittest.TestCase):
    def test_counts_are_bound_to_independent_reads_and_exact_attachments(self):
        producer, activity = producer_and_witness()
        profile = load_pressure_profile()
        profile['windowSeconds'] = 40  # Synthetic replay fixture; runtime profile remains 900 seconds.
        for kind in ('ring-pressure', 'paused-pressure'):
            with tempfile.TemporaryDirectory() as path:
                fixture = prepared(Path(path), kind)
                record = execute(fixture, kind)
                result = replay_pressure_session(record, fixture.path.read_text(), producer, activity, profile)
                self.assertTrue(result['sessionChecksPassed'])
                counts = result['independentProducer']
                self.assertGreaterEqual(counts['fullyBracketedProducerIntervals'], 20)
                self.assertLessEqual(counts['minimumReadCalls'], 131072)
                self.assertGreaterEqual(counts['maximumReadCalls'], 131072)
                if kind == 'paused-pressure':
                    self.assertGreater(counts['pauseMinimumReadCalls'], 10000)

    def test_other_session_attachments_cannot_support_this_session(self):
        producer, activity = producer_and_witness()
        with tempfile.TemporaryDirectory() as path:
            record = execute(prepared(Path(path)))
            for row in activity:
                row['snapshot']['objects']['link'] = [value + 100 for value in row['snapshot']['objects']['link']]
            with self.assertRaisesRegex(ValueError, 'this exact session'):
                producer_bounds(producer, activity, record, 40, 20)

    def test_impossible_produced_counts_and_foreign_pause_are_rejected(self):
        producer, activity = producer_and_witness()
        with tempfile.TemporaryDirectory() as path:
            record = execute(prepared(Path(path), 'paused-pressure'), 'paused-pressure')
            for count in (0, 1000000000):
                wrong = deepcopy(record)
                wrong['observation']['engineCounts']['produced'] = count
                with self.assertRaisesRegex(ValueError, 'independent read bounds'):
                    producer_bounds(producer, activity, wrong, 40, 20)
            wrong = deepcopy(record)
            wrong['observation']['pause']['startedUnixNano'] -= 100000000000
            with self.assertRaisesRegex(ValueError, 'admitted lifetime'):
                producer_bounds(producer, activity, wrong, 40, 20)

    def test_native_cleanup_counts_require_explicit_integer_zero(self):
        producer, activity = producer_and_witness()
        profile = load_pressure_profile()
        profile['windowSeconds'] = 40
        with tempfile.TemporaryDirectory() as path:
            fixture = prepared(Path(path))
            record = execute(fixture)
            for invalid in ([], None, False, -1, 0.0, '0'):
                value = deepcopy(record)
                value['cleanupWitness']['remaining']['map'] = invalid
                with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                    replay_pressure_session(value, fixture.path.read_text(), producer, activity, profile)
            value = deepcopy(record)
            del value['cleanupWitness']['remaining']['map']
            with self.assertRaises(ValueError):
                replay_pressure_session(value, fixture.path.read_text(), producer, activity, profile)

    def test_changed_cleanup_or_reported_verdict_cannot_pass_replay(self):
        producer, activity = producer_and_witness()
        profile = load_pressure_profile()
        profile['windowSeconds'] = 40
        with tempfile.TemporaryDirectory() as path:
            fixture = prepared(Path(path))
            record = execute(fixture)
            mutations = []
            value = deepcopy(record)
            value['cleanupWitness']['remaining']['map'] = 1
            mutations.append(value)
            value = deepcopy(record)
            value['deadlineCleanupUpperNanos'] = 1
            mutations.append(value)
            value = deepcopy(record)
            value['sessionChecksPassed'] = False
            mutations.append(value)
            for value in mutations:
                with self.assertRaises(ValueError):
                    replay_pressure_session(value, fixture.path.read_text(), producer, activity, profile)


if __name__ == '__main__':
    unittest.main()
