"""One bounded pair of simultaneous selected traces; no profile activation."""
from datetime import datetime
import time

from local_case import digest
from processes import read_document
from profile import trace_slots
from trace_observation import clock_ns, empty


def expiry_nanos(admission):
    value = datetime.fromisoformat(admission['expiresAt'].replace('Z', '+00:00'))
    if value.tzinfo is None:
        raise ValueError('admission expiry lacks timezone')
    return int(value.timestamp()) * 1000000000 + value.microsecond * 1000


def complete_delivery(window, process, name):
    code = process.poll()
    if code is None:
        raise ValueError('delivery still running')
    path = window.directory / (name + '.jsonl')
    delivered = read_document(path)
    if (delivered.get('sameKernelClock') is not True
            or delivered['observation']['transportComplete'] is not True
            or delivered['observation']['hookCoverageIncomplete'] is not True
            or delivered['latency']['receivedEvents'] <= 0):
        raise ValueError('selected concurrent event transport incomplete')
    latency = delivered['latency']
    passed = latency['eventDeliveryBudgetPassed'] and latency['normalLossBudgetPassed']
    if code not in (0, 1) or (code == 0) != passed:
        raise ValueError('delivery process and complete result disagree')
    if code == 1:
        window.processes.accept_delivery_budget_failure(process)
    return {'deliverySHA256': digest(path.read_bytes()), 'latency': latency}


class ConcurrentTracePair:
    def __init__(self, window):
        self.window = window

    def run(self, index, due):
        w, t = self.window, self.window.profile['trace']
        while time.monotonic() < due:
            w.processes.healthy()
            time.sleep(max(0, min(.25, due - time.monotonic())))
        w.processes.healthy()
        lateness = time.monotonic() - due
        if lateness > .25 or not empty(w.snapshot()):
            raise ValueError('concurrent slot missed or previous owned state remains')
        intent = {'schemaVersion': 1, 'container': 'worker',
                  **{key: t[key] for key in ('kind', 'rawPaths', 'durationSeconds', 'maxEvents',
                                            'maxOutputBytes', 'maxMapBytes', 'maxPathBytes')}}
        finish_by = time.monotonic() + t['durationSeconds'] + 10
        receivers, requested = {}, {}
        # Start both streams before waiting for either. Any exception escapes to
        # Window's cancellation/source teardown, retaining every known admission.
        for target in (0, 1):
            requested[target] = clock_ns(w.case)
            admission = w.admissions.create(target, {**intent, 'pod': w.targets[target]['podName']})
            name = f'delivery-{index:02}-target-{target}'
            path = w.processes.configuration(name, w.receiver_config(admission, target))
            receivers[target] = (name, w.processes.native(name, 'delivery', '--config', path))
        attach_by = min(finish_by, time.monotonic() + 5)
        while True:
            w.processes.healthy()
            snapshot = w.snapshot()
            if (snapshot['workers'] == snapshot['activeControls'] == 2
                    and snapshot['excludedWorkers'] == 0
                    and all(len(snapshot['objects'][key]) == count for key, count in t['objects'].items())):
                break
            if any(process.poll() is not None for _, process in receivers.values()) or time.monotonic() >= attach_by:
                raise ValueError('complete simultaneous attachments not observed')
            time.sleep(.1)
        attached = snapshot['clock']
        probe = w.admissions.probe_capacity({**intent, 'pod': w.targets[0]['podName']})
        expiries = {}
        for target in (0, 1):
            status, admission = w.admissions.get(target)
            if (status != 200 or admission.get('state') != 'active'
                    or '/' + admission['metadata']['name'] != w.admissions.pending[target]):
                raise ValueError('concurrent admission expired or changed before delivery')
            expiries[target] = expiry_nanos(admission)
        deliveries = {}
        while len(deliveries) < 2:
            for target, (name, process) in receivers.items():
                if target not in deliveries and process.poll() is not None:
                    deliveries[target] = complete_delivery(w, process, name)
            w.processes.healthy()
            if time.monotonic() >= finish_by:
                raise ValueError('concurrent delivery exceeded its shared deadline')
            if len(deliveries) == 2:
                break
            time.sleep(.1)
        w.admissions.cancel_all()
        cleared_by = time.monotonic() + 5
        while True:
            current = w.snapshot()
            left = w.case.remaining({key: sorted(ids) for key, ids in w.owned.items()})
            if empty(current) and not any(left['remaining'].values()):
                break
            if time.monotonic() >= cleared_by:
                raise ValueError('concurrent owned state did not clear')
            time.sleep(.1)
        ended = current['clock']['wallNanos'] + current['clock']['uncertaintyNanos']
        for target in (0, 1):
            deliveries[target].update(
                targetIndex=target,
                attachUpperNanos=attached['wallNanos'] + attached['uncertaintyNanos']
                    - requested[target]['wallNanos'] + requested[target]['uncertaintyNanos'],
                deadlineTeardownUpperNanos=max(0, ended - expiries[target]))
        result = {'index': index, 'scheduledOffsetSeconds': trace_slots(w.profile)[index],
                  'admissionLatenessNanos': int(lateness * 1000000000), 'zeroOwnedState': True,
                  'capacityProbe': probe, 'targets': [deliveries[target] for target in (0, 1)]}
        w.sessions.append(result)
        return result
