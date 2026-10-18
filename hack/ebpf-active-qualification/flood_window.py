"""Complete fixed-duration flood/control windows with common owned cleanup."""
import json
import time

from flood_profile import slots, stem
from flood_session import run_session, workload_spec
from gated import GatedWorkload
from samples import require
from trace_observation import clock_ns
from verify_flood import validate_flood, validate_paced_flood
from window import Window


class FloodWindow(Window):
    def measure_configuration(self, groups):
        return {**super().measure_configuration(groups), 'observation': 'containment'}

    def snapshot(self):
        # The secondary fixture must not satisfy this selected-only witness.
        value = self.case.snapshot(self.owner, self.targets[:1])
        for key, ids in value['objects'].items():
            self.owned[key].update(ids)
        return value

    def run_workloads(self, origin, neighbours, selected_count):
        require(not neighbours and selected_count == 1, 'flood fixture profile mismatch')
        for index, kind, offset in slots(self.profile):
            # Starting the gated process is inside the declared slot preparation;
            # slow startup fails the slot instead of silently shifting the series.
            due = origin + offset
            while time.monotonic() < due - 2:
                self.processes.healthy()
                time.sleep(max(0, min(.25, due - 2 - time.monotonic())))
            work = workload_spec(kind)
            command = self.case.runtime.kube + ['-n', self.case.namespaces[0], 'exec', '-i',
                self.targets[0]['podName'], '-c', 'worker', '--', '/usr/local/bin/kml-io-workload',
                work['mode'], str(work['count'])]
            workload = GatedWorkload(command)
            try:
                while time.monotonic() < due:
                    self.processes.healthy()
                    time.sleep(max(0, min(.05, due - time.monotonic())))
                lateness = int((time.monotonic() - due) * 1000000000)
                require(lateness <= self.profile['maximumSlotLatenessNanos'], 'flood slot missed; no retimed retry')
                self.processes.healthy()
                if self.phase == 'enabled':
                    result = run_session(self, workload, kind, index)
                else:
                    validate = validate_paced_flood if kind == 'event-limit' else validate_flood
                    anchor = clock_ns(self.case)
                    receipt = validate(workload.run(), work['count'])
                    workload.finish()
                    with (self.directory / (stem(index, kind) + '-workload.json')).open('x') as stream:
                        json.dump(receipt, stream)
                    result = {'index': index, 'case': kind, 'completed': True, 'workload': receipt,
                              'operationAnchor': anchor}
                    self.sessions.append(result)
                result.update(scheduledOffsetSeconds=offset, slotLatenessNanos=lateness)
            finally:
                workload.close()
            # Preserve a complete failed mechanism in the final paired verdict;
            # transport, timing or cleanup exceptions still abort this window.
            self.processes.healthy()
