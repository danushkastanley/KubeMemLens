"""One owned flood admission; the caller owns resource sampling and source teardown."""
import json
from pathlib import Path
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'prototype/trace/qualification/filecache'))

from ceiling_result import evaluate_ceiling_session
from paused_reader_result import evaluate_paused_reader_session
from flood_result import kernel_budget
from receiver_configuration import receiver_configuration
from samples import exact, integer, require, strict_json
from trace_observation import empty
from verify_flood import validate_flood, validate_paced_flood
from local_case import digest
from session_cleanup import clear_owned_session

READ_CALLS = 131072
CASES = {'event-limit': ('ceiling', 'event_limit'),
         'output-limit': ('ceiling', 'output_limit'),
         'ring-flood': ('ceiling', 'event_limit'),
         'paused-reader': ('paused-reader', 'event_limit')}
OBJECTS = {'map': 7, 'prog': 5, 'link': 5}


def trace_bounds(kind):
    require(kind in CASES, 'unknown flood session case')
    return {'kind': 'files', 'rawPaths': True, 'durationSeconds': 30, 'maxEvents': 10000,
            'maxOutputBytes': 65536 if kind == 'output-limit' else 8 << 20,
            'maxMapBytes': 8 << 20, 'maxPathBytes': 64}


def workload_spec(kind):
    require(kind in CASES, 'unknown flood session case')
    if kind == 'event-limit':
        return {'mode': 'paced-flood', 'count': 12000}
    return {'mode': 'flood', 'count': READ_CALLS}


def wait_ready(process, path, mode, deadline):
    expected = 'paused-reader-ready' if mode == 'paused-reader' else 'ceiling-ready'
    while True:
        if path.exists():
            require(path.stat().st_size <= 65536, 'flood reader output exceeds bound')
            with path.open('rb') as stream:
                line = stream.readline(4097)
            require(len(line) <= 4096, 'flood readiness exceeds bound')
            if line.endswith(b'\n'):
                ready = strict_json(line.decode())
                exact(ready, {'schemaVersion', 'case', 'ready'})
                integer(ready['schemaVersion'], 1, 1)
                require(ready['case'] == expected and ready['ready'] is True,
                        'flood receiver readiness does not match its mode')
                return
        require(process.poll() is None and time.monotonic() < deadline,
                'flood receiver stopped or readiness deadline expired')
        time.sleep(.05)


def pause_covers_burst(observation, receipt, anchor):
    for key in ('wallNanos', 'monotonicNanos', 'uncertaintyNanos'):
        integer(anchor[key])
    require(receipt['operationStartedMonotonicNanos'] >= anchor['monotonicNanos'],
            'flood receipt predates its attachment witness')
    offset = anchor['wallNanos'] - anchor['monotonicNanos']
    # The receiver already rejects wall/monotonic drift above five milliseconds.
    uncertainty = anchor['uncertaintyNanos'] + 5000000
    return (receipt['operationStartedMonotonicNanos'] + offset - uncertainty >= observation['pause']['startedUnixNano']
            and receipt['operationEndedMonotonicNanos'] + offset + uncertainty <= observation['pause']['endedUnixNano'])


def run_session(window, workload, kind, index):
    """Own the supplied ready gated workload and cancel every known admission.

    Window must have verified its helpers, fixture bindings, source identities
    and empty state. Any exception requires its outer source/fixture teardown,
    particularly when POST may have succeeded without returning a usable ID.
    This returns session correctness evidence, never resource qualification.
    """
    try:
        integer(index, 0, 999)
        trace = trace_bounds(kind)
    except Exception:
        workload.close()
        raise
    mode, reason = CASES[kind]
    name = f'delivery-flood-{index:03}-{kind}'
    path = window.directory / (name + '.jsonl')
    result = {'index': index, 'case': kind, 'completed': False, 'zeroOwnedState': False,
              'scope': 'single-session correctness only; resource and paired campaign evidence required'}
    window.sessions.append(result)
    try:
        require(not window.admissions.pending and empty(window.snapshot()), 'previous owned trace state remains')
        require(workload.process.poll() is None, 'gated flood workload exited before admission')
        admission = window.admissions.create(0, {'schemaVersion': 1, 'pod': window.targets[0]['podName'], 'container': 'worker', **trace})
        private = receiver_configuration(window.case, window.admissions, window.targets, 0,
                                         admission, trace, window.boot)
        config = window.processes.configuration(name, private)
        started = time.monotonic()
        receiver = window.processes.native(name, 'delivery', '--config', config, '--observation', mode)
        window.processes.healthy()
        wait_ready(receiver, path, mode, started + 5)
        # Enumerate complete attachments before releasing any selected-file I/O.
        # Fast bursts may finish before a periodic witness observes them.
        snapshot = window.snapshot()
        require(snapshot['workers'] == snapshot['activeControls'] == 1 and snapshot['excludedWorkers'] == 0
                and all(len(snapshot['objects'][key]) == count for key, count in OBJECTS.items()),
                'complete flood attachments not observed before burst')
        result['attachmentWitness'] = snapshot
        require(receiver.poll() is None and time.monotonic() < started + 5,
                'receiver expired before controlled burst')
        window.processes.healthy()
        work = workload_spec(kind)
        validate = validate_paced_flood if kind == 'event-limit' else validate_flood
        receipt = validate(workload.run(), work['count'])
        require(receipt['operationStartedMonotonicNanos'] >= snapshot['clock']['monotonicNanos'],
                'flood receipt predates controlled activation')
        with (window.directory / (name + '-workload.json')).open('x') as stream:
            json.dump(receipt, stream, indent=2)
            stream.write('\n')
        require(time.monotonic() < started + 40, 'flood session deadline exceeded')
        code = receiver.wait(timeout=max(.01, started + 40 - time.monotonic()))
        require(time.monotonic() < started + 40, 'flood receiver completed after its deadline')
        require(path.stat().st_size <= 65536, 'flood receiver result exceeds bound')
        raw = path.read_text()
        evaluate = evaluate_paused_reader_session if mode == 'paused-reader' else evaluate_ceiling_session
        observation = evaluate(raw, expected_reason=reason, max_events=trace['maxEvents'],
                               max_output_bytes=trace['maxOutputBytes'], exit_code=code)
        if code == 1:
            window.processes.accept_delivery_budget_failure(receiver)
        workload.finish()
        result.update(completed=True, readyBeforeBurst=True, workload=receipt, observation=observation,
                      requestedCeilingObserved=observation['requestedCeilingObserved'], deliverySHA256=digest(raw.encode()),
                      deliveryExitCode=code)
        result['sessionChecksPassed'] = observation['requestedCeilingObserved']
        if kind in ('ring-flood', 'paused-reader'):
            result['kernelBudget'] = kernel_budget(observation, receipt, trace['maxEvents'])
            result['sessionChecksPassed'] = result['kernelBudget']['kernelBudgetAndLossPassed']
        if mode == 'paused-reader':
            result['burstFullyWithinPause'] = pause_covers_burst(observation, receipt, snapshot['clock'])
            result['sessionChecksPassed'] = result['sessionChecksPassed'] and result['burstFullyWithinPause']
        return result
    except BaseException as error:
        result['failureType'] = type(error).__name__
        raise
    finally:
        errors = []
        try:
            clear_owned_session(window, result)
        except Exception:
            errors.append('admission or owned kernel state')
        try:
            workload.close()
        except Exception:
            errors.append('gated workload')
        if errors:
            result.update(completed=False, cleanupFailures=errors)
            raise ValueError('flood session cleanup unconfirmed; outer source teardown required')
