"""Run continuous selected I/O around a complete paired resource window."""
import time

from flood_window import FloodWindow
from pressure_profile import pressure_slots
from pressure_session import run_pressure_session
from pressure_workload import PressureWorkload
from samples import require


class PressureWindow(FloodWindow):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.pressure = None

    def prepare_workloads(self):
        require(self.pressure is None, 'pressure producer already created')
        command = self.case.runtime.kube + ['-n', self.case.namespaces[0], 'exec', '-i',
            self.targets[0]['podName'], '-c', 'worker', '--', '/usr/local/bin/kml-io-workload',
            'pressure', str(self.profile['producerSeconds'])]
        self.pressure = PressureWorkload(command, self.profile['producerSeconds'])
        self.pressure.start()
        deadline = time.monotonic() + self.profile['producerLeadSeconds']
        while time.monotonic() < deadline:
            require(self.pressure.process.poll() is None, 'pressure producer stopped before sampling')
            time.sleep(max(0, min(.1, deadline - time.monotonic())))

    def run_workloads(self, origin, neighbours, selected_count):
        require(not neighbours and selected_count == 1 and self.pressure is not None,
                'sustained workload profile mismatch')
        if self.phase == 'enabled':
            for index, kind, offset in pressure_slots(self.profile):
                due = origin + offset
                while time.monotonic() < due:
                    self.processes.healthy()
                    require(self.pressure.process.poll() is None, 'pressure producer stopped during sampling')
                    time.sleep(max(0, min(.25, due - time.monotonic())))
                lateness = int((time.monotonic() - due) * 1000000000)
                require(lateness <= self.profile['maximumSlotLatenessNanos'], 'pressure slot missed; no retry')
                self.processes.healthy()
                result = run_pressure_session(self, kind, index)
                result.update(scheduledOffsetSeconds=offset, slotLatenessNanos=lateness)
        while time.monotonic() < origin + self.profile['windowSeconds']:
            self.processes.healthy()
            require(self.pressure.process.poll() is None, 'pressure producer stopped during sampling')
            time.sleep(max(0, min(.25, origin + self.profile['windowSeconds'] - time.monotonic())))
        self.pressure.collect()
        with (self.directory / 'pressure.jsonl').open('x') as stream:
            stream.write(self.pressure.raw)
        self.pressure.finish()

    def close_workloads(self):
        if self.pressure is not None:
            self.pressure.close()
