"""Observe one saturated trace while the independent producer keeps running."""
from datetime import datetime
import time

from ceiling_result import evaluate_ceiling_session
from flood_session import OBJECTS, trace_bounds, wait_ready
from local_case import digest
from paused_reader_result import evaluate_paused_reader_session
from samples import integer, require
from session_cleanup import clear_owned_session
from trace_observation import clock_ns, empty


def saturation(observation, maximum):
    integer(maximum, 1, 10000)
    require(observation['transportComplete'] is True, 'pressure transport incomplete')
    counts = observation['engineCounts']
    for key in ('produced', 'sampled', 'lost', 'rejected'):
        integer(counts[key])
    require(counts['produced'] >= counts['sampled'], 'pressure kernel counter ordering invalid')
    admitted = counts['produced'] - counts['sampled']
    accounted = observation['events'] + counts['lost'] + counts['rejected'] == admitted
    return {'kernelCandidates': admitted, 'kernelBudgetExhausted': admitted == maximum,
            'ringLossObserved': counts['lost'] > 0,
            'knownCandidatesAccounted': accounted,
            'saturationObserved': observation['termination'] == 'expired' and counts['produced'] > maximum
                and admitted == maximum and counts['lost'] > 0 and accounted,
            # Reported rejection is an outcome, not evidence of an unfilled ring.
            # Floods may drop; every admitted candidate must still be accounted.
            'scope': 'kernel counters only; independent continuous-producer interval bounds still required'}


def run_pressure_session(window, kind, index):
    require(kind in ('ring-pressure', 'paused-pressure'), 'unknown sustained pressure observation')
    integer(index, 0, 999)
    trace = trace_bounds('ring-flood')
    mode = 'paused-reader' if kind == 'paused-pressure' else 'ceiling'
    name = f'delivery-pressure-{index:03}'
    path = window.directory / (name + '.jsonl')
    result = {'index': index, 'case': kind, 'completed': False, 'zeroOwnedState': False,
              'scope': 'saturated session mechanism only; producer and paired resources required'}
    window.sessions.append(result)
    try:
        require(not window.admissions.pending and empty(window.snapshot()), 'previous trace state remains')
        require(window.pressure.process.poll() is None, 'continuous producer stopped before admission')
        requested = clock_ns(window.case)
        admission = window.admissions.create(0, {'schemaVersion': 1, 'pod': window.targets[0]['podName'], 'container': 'worker', **trace})
        config = window.processes.configuration(name, window.receiver_config(admission))
        started = time.monotonic()
        receiver = window.processes.native(name, 'delivery', '--config', config, '--observation', mode)
        wait_ready(receiver, path, mode, started + 5)
        witness = window.snapshot()
        require(witness['workers'] == witness['activeControls'] == 1 and witness['excludedWorkers'] == 0
                and all(len(witness['objects'][key]) == count for key, count in OBJECTS.items()),
                'complete pressure attachment witness unavailable')
        status, active = window.admissions.get(0)
        require(status == 200 and active['state'] == 'active'
                and active['metadata']['name'] == admission['metadata']['name'], 'active pressure admission unavailable')
        expiry = datetime.fromisoformat(active['expiresAt'].replace('Z', '+00:00'))
        expiry_ns = int(expiry.timestamp()) * 1000000000 + expiry.microsecond * 1000
        require(witness['clock']['wallNanos'] < expiry_ns
                <= requested['wallNanos'] + (trace['durationSeconds'] + 5) * 1000000000,
                'pressure admission duration differs from bounds')
        code = receiver.wait(timeout=max(.01, started + 40 - time.monotonic()))
        require(time.monotonic() < started + 40 and window.pressure.process.poll() is None,
                'pressure receiver deadline or producer lifetime failed')
        require(path.stat().st_size <= 65536, 'pressure result exceeds bound')
        raw = path.read_text()
        evaluate = evaluate_paused_reader_session if mode == 'paused-reader' else evaluate_ceiling_session
        observed = evaluate(raw, expected_reason='event_limit', max_events=trace['maxEvents'],
                            max_output_bytes=trace['maxOutputBytes'], exit_code=code)
        if code == 1:
            window.processes.accept_delivery_budget_failure(receiver)
        budget = saturation(observed, trace['maxEvents'])
        result.update(completed=True, requestedClock=requested, attachmentWitness=witness,
                      activeExpiresAtNanos=expiry_ns, deliverySHA256=digest(raw.encode()),
                      deliveryExitCode=code, observation=observed, saturation=budget,
                      sessionChecksPassed=budget['saturationObserved'])
        return result
    except BaseException as error:
        result['failureType'] = type(error).__name__
        raise
    finally:
        try:
            clear_owned_session(window, result)
            if result['completed']:
                clock = result['cleanupWitness']['snapshot']['clock']
                upper = max(0, clock['wallNanos'] + clock['uncertaintyNanos'] - result['activeExpiresAtNanos'])
                result['deadlineCleanupUpperNanos'] = upper
                result['deadlineCleanupBudgetPassed'] = upper < 2000000000
                result['sessionChecksPassed'] = result['sessionChecksPassed'] and result['deadlineCleanupBudgetPassed']
        except Exception:
            result.update(completed=False, cleanupFailures=['admission or owned kernel state'])
            raise ValueError('pressure cleanup unconfirmed; outer source teardown required')
