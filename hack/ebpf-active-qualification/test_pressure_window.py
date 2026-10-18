from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from pressure_profile import load_pressure_profile, pressure_slots
from pressure_window import PressureWindow
from test_flood_session import Clock


class Producer:
    def __init__(self, clock):
        self.clock = clock
        self.events = []
        self.process = SimpleNamespace(poll=lambda: None)
        self.raw = 'validated producer receipt\n'

    def start(self):
        self.events.append(('start', self.clock.value))

    def collect(self):
        self.events.append(('collect', self.clock.value))
        self.clock.value = max(self.clock.value, 910)

    def finish(self):
        self.events.append(('finish', self.clock.value))

    def close(self):
        self.events.append(('close', self.clock.value))


class PressureWindowTests(unittest.TestCase):
    def window(self, path, phase):
        window = PressureWindow.__new__(PressureWindow)
        window.profile, window.phase, window.directory = load_pressure_profile(), phase, path
        window.pressure = None
        window.sessions = []
        window.targets = [{'podName': 'target-owned-child'}]
        window.case = SimpleNamespace(runtime=SimpleNamespace(kube=['kubectl']), namespaces=['owned'])
        window.processes = SimpleNamespace(healthy=Mock())
        return window

    def test_same_continuous_producer_surrounds_control_and_enabled_sampling(self):
        for phase in ('control', 'enabled'):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as path:
                window, clock = self.window(Path(path), phase), Clock()
                producer = Producer(clock)
                def session(_window, kind, index):
                    clock.value += 30
                    row = {'case': kind, 'index': index, 'sessionChecksPassed': False}
                    window.sessions.append(row)
                    return row
                with patch('pressure_window.time.monotonic', clock.monotonic), patch('pressure_window.time.sleep', clock.sleep), \
                     patch('pressure_window.PressureWorkload', return_value=producer) as make, \
                     patch('pressure_window.run_pressure_session', side_effect=session) as trace:
                    window.prepare_workloads()
                    origin = clock.value
                    window.run_workloads(origin, [], 1)
                    window.close_workloads()
                self.assertEqual(make.call_args.args[0][-2:], ['pressure', '910'])
                self.assertEqual(make.call_args.args[1], 910)
                self.assertEqual(origin, 1)
                self.assertEqual(producer.events, [('start', 0), ('collect', 901), ('finish', 910), ('close', 910)])
                self.assertEqual(trace.call_count, 20 if phase == 'enabled' else 0)
                self.assertEqual((Path(path) / 'pressure.jsonl').read_text(), producer.raw)
                self.assertEqual(window.measure_configuration([])['observation'], 'containment')
                if phase == 'enabled':
                    self.assertEqual([row['scheduledOffsetSeconds'] for row in window.sessions],
                                     [offset for _, _, offset in pressure_slots(window.profile)])

    def test_failed_producer_prevents_observation_and_can_be_closed(self):
        with tempfile.TemporaryDirectory() as path:
            window, clock = self.window(Path(path), 'control'), Clock()
            producer = Producer(clock)
            producer.process.poll = lambda: 2
            with patch('pressure_window.time.monotonic', clock.monotonic), \
                 patch('pressure_window.PressureWorkload', return_value=producer):
                with self.assertRaisesRegex(ValueError, 'before sampling'):
                    window.prepare_workloads()
                window.close_workloads()
            self.assertEqual([event[0] for event in producer.events], ['start', 'close'])

    def test_observer_failure_during_control_is_not_hidden_until_producer_exit(self):
        with tempfile.TemporaryDirectory() as path:
            window, clock = self.window(Path(path), 'control'), Clock()
            producer = Producer(clock)
            window.pressure = producer
            window.processes.healthy.side_effect = ValueError('observer stopped')
            with patch('pressure_window.time.monotonic', clock.monotonic):
                with self.assertRaisesRegex(ValueError, 'observer stopped'):
                    window.run_workloads(0, [], 1)
            self.assertFalse(producer.events)


if __name__ == '__main__':
    unittest.main()
