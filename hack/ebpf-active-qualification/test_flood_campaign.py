from copy import deepcopy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from flood_evaluate import compare_workloads, operation_in_window
from flood_profile import KINDS, load_flood_profile, slots, stem
from flood_window import FloodWindow
from provenance import profile_stream_names, write_envelope, verify_envelope
from test_flood_session import Clock


class Workload:
    def __init__(self, command, clock):
        self.mode, self.count = command[-2], int(command[-1])
        self.clock = clock
        self.released = False
        self.closed = False

    def run(self):
        self.released = True
        start = int(self.clock.value * 1000000000)
        duration = 6000000000 if self.mode == 'paced-flood' else 100000000
        receipt = {'schemaVersion': 1, 'mode': self.mode, 'fileBytes': 8 << 20,
                   'readCalls': self.count, 'bytesPerRead': 64, 'readBytes': self.count * 64,
                   'operationStartedMonotonicNanos': start,
                   'operationEndedMonotonicNanos': start + duration, 'operationNanos': duration}
        if self.mode == 'paced-flood':
            receipt.update(schemaVersion=2, readPeriodNanos=500000)
        self.clock.value += duration / 1000000000
        return receipt

    def finish(self):
        pass

    def close(self):
        self.closed = True


class FloodCampaignTests(unittest.TestCase):
    def test_profile_fixes_all_five_full_pairs_and_twenty_slots(self):
        profile = load_flood_profile()
        self.assertEqual((profile['pairs'], profile['windowSeconds']), (5, 900))
        schedule = slots(profile)
        self.assertEqual(len(schedule), 20)
        self.assertEqual([kind for _, kind, _ in schedule], list(KINDS) * 5)
        self.assertEqual(schedule[-1][2], 790)
        with tempfile.TemporaryDirectory() as path:
            altered = deepcopy(profile)
            altered['windowSeconds'] = 30
            file = Path(path) / 'profile.json'
            file.write_text(json.dumps(altered))
            with self.assertRaises(ValueError):
                load_flood_profile(file)

    def test_control_and_enabled_execute_same_workload_slots(self):
        for phase in ('control', 'enabled'):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as path:
                window, clock, created = self.window(Path(path), phase)
                def start(command):
                    item = Workload(command, clock)
                    created.append(item)
                    return item
                def admitted(_window, workload, kind, index):
                    result = {'case': kind, 'index': index, 'completed': True,
                              'sessionChecksPassed': False, 'workload': workload.run()}
                    window.sessions.append(result)
                    return result
                with patch('flood_window.time.monotonic', clock.monotonic), patch('flood_window.time.sleep', clock.sleep), \
                     patch('flood_window.GatedWorkload', side_effect=start), \
                     patch('flood_window.clock_ns', return_value={'monotonicNanos': 1, 'wallNanos': 1, 'uncertaintyNanos': 0}), \
                     patch('flood_window.run_session', side_effect=admitted) as run:
                    window.run_workloads(0, [], 1)
                self.assertEqual(len(created), 20)
                self.assertTrue(all(item.released and item.closed for item in created))
                self.assertEqual(run.call_count, 20 if phase == 'enabled' else 0)
                self.assertEqual([r['scheduledOffsetSeconds'] for r in window.sessions], list(range(30, 791, 40)))
                self.assertEqual([r['index'] for r in window.sessions], list(range(20)))
                self.assertEqual(window.measure_configuration([])['observation'], 'containment')
                if phase == 'control':
                    self.assertEqual(len(list(Path(path).glob('*-workload.json'))), 20)

    def window(self, path, phase):
        window = FloodWindow.__new__(FloodWindow)
        window.profile, window.phase, window.directory = load_flood_profile(), phase, path
        window.sessions = []
        window.targets = [{'podName': 'target-owned-child'}]
        window.case = SimpleNamespace(runtime=SimpleNamespace(kube=['kubectl']), namespaces=['owned'])
        window.processes = SimpleNamespace(healthy=Mock())
        return window, Clock(), []

    def test_late_preparation_never_releases_or_retimes_workload(self):
        with tempfile.TemporaryDirectory() as path:
            window, clock, created = self.window(Path(path), 'enabled')
            def slow(command):
                clock.value += 3
                item = Workload(command, clock)
                created.append(item)
                return item
            with patch('flood_window.time.monotonic', clock.monotonic), patch('flood_window.time.sleep', clock.sleep), \
                 patch('flood_window.GatedWorkload', side_effect=slow), patch('flood_window.run_session') as run:
                with self.assertRaisesRegex(ValueError, 'slot missed'):
                    window.run_workloads(0, [], 1)
            run.assert_not_called()
            self.assertEqual(len(created), 1)
            self.assertTrue(created[0].closed)
            self.assertFalse(created[0].released)

    def test_throughput_uses_io_duration_and_keeps_each_failed_burst(self):
        rows = [{'case': kind, 'workload': {'readBytes': 100, 'readCalls': 1, 'operationNanos': 980}}
                for _, kind, _ in slots(load_flood_profile())]
        self.assertTrue(compare_workloads(rows, deepcopy(rows))['selectedThroughputBudgetPassed'])
        after = deepcopy(rows)
        after[3]['workload']['operationNanos'] = 1000  # Exactly 2% throughput loss fails.
        result = compare_workloads(rows, after)
        self.assertFalse(result['selectedThroughputBudgetPassed'])
        self.assertFalse(result['pairs'][3]['throughputRegressionBelowTwoPercent'])
        self.assertTrue(result['pairs'][2]['throughputRegressionBelowTwoPercent'])
        with self.assertRaises(ValueError):
            compare_workloads(rows, after[:-1])

    def test_workload_clock_must_fit_inside_resource_window(self):
        receipt = {'operationStartedMonotonicNanos': 2000000000,
                   'operationEndedMonotonicNanos': 2100000000}
        anchor = {'monotonicNanos': 1000000000, 'wallNanos': 100000000000, 'uncertaintyNanos': 1000}
        bounds = [{'wallNanos': 100000000000}, {'wallNanos': 102000000000}]
        operation_in_window(receipt, anchor, bounds)
        with self.assertRaises(ValueError):
            operation_in_window(receipt, anchor, bounds[:1] * 2)

    def test_every_workload_delivery_and_resource_stream_is_bound(self):
        profile = load_flood_profile()
        self.assertEqual(len(profile_stream_names('control', profile)), 27)
        self.assertEqual(len(profile_stream_names('enabled', profile)), 48)
        with tempfile.TemporaryDirectory() as path:
            root = Path(path)
            for name in profile_stream_names('enabled', profile) + ['window.private.json']:
                (root / name).write_text('{}\n')
            write_envelope(root, 'enabled', 1, profile, {'sourceSHA256': 'a' * 64}, 'boot')
            verify_envelope(root, 'enabled', profile)
            (root / (stem(0, KINDS[0]) + '-workload.json')).write_text('{"changed":true}\n')
            with self.assertRaises(ValueError):
                verify_envelope(root, 'enabled', profile)


if __name__ == '__main__':
    unittest.main()
