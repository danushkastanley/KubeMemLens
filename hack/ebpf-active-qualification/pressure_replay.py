"""Bind sustained trace observations to independent producer and link records."""
from activity import active_signatures, bracket_windows, clock, snapshot, validate_witness
from ceiling_result import evaluate_ceiling_session
from flood_session import OBJECTS, trace_bounds
from local_case import digest
from paused_reader_result import evaluate_paused_reader_session
from pressure_metrics import boundaries
from pressure_session import saturation
from samples import integer, require
from session_cleanup_replay import verify_cleanup


def producer_bounds(producer, witness, record, seconds, minimum_intervals):
    integer(minimum_intervals, 1, seconds)
    validate_witness(witness, seconds)
    expected = record['attachmentWitness']
    signature = tuple(tuple(expected['objects'][key]) for key in sorted(OBJECTS))
    signatures = [value if value == signature else None for value in active_signatures(witness, OBJECTS)]
    starts = [row['clock']['monotonicNanos'] - row['readNanos'] - row['clock']['uncertaintyNanos'] for row in witness]
    ends = [row['clock']['monotonicNanos'] + row['clock']['uncertaintyNanos'] for row in witness]
    intervals = producer['intervals']
    linked = bracket_windows(starts, ends, signatures,
        [(row['startedMonotonicNanos'], row['endedMonotonicNanos']) for row in intervals])
    spans = boundaries(producer)
    requested = record['requestedClock']
    cleanup = record['cleanupWitness']['snapshot']['clock']
    lower_start = expected['clock']['wallNanos'] + expected['clock']['uncertaintyNanos']
    upper_start = requested['wallNanos'] - requested['uncertaintyNanos']
    upper_end = cleanup['wallNanos'] + cleanup['uncertaintyNanos']
    lower = [i for i, span in enumerate(spans) if linked[i]
             and span[0] >= lower_start and span[3] <= record['activeExpiresAtNanos']]
    upper = [i for i, span in enumerate(spans) if span[0] < upper_end and span[3] > upper_start]
    require(len(lower) >= minimum_intervals and set(lower) <= set(upper),
            'insufficient producer intervals bracketed by this exact session')
    low = sum(intervals[i]['readCalls'] for i in lower)
    high = sum(intervals[i]['readCalls'] for i in upper)
    produced = record['observation']['engineCounts']['produced']
    require(low <= produced <= high, 'kernel produced count contradicts independent read bounds')
    result = {'fullyBracketedProducerIntervals': len(lower), 'minimumReadCalls': low,
              'maximumReadCalls': high, 'reportedProducedCandidates': produced}
    if record['case'] == 'paused-pressure':
        pause = record['observation']['pause']
        require(upper_start <= pause['startedUnixNano'] < pause['endedUnixNano'] <= record['activeExpiresAtNanos'],
                'reader pause lies outside its admitted lifetime')
        inside = [i for i, span in enumerate(spans)
                  if span[0] >= pause['startedUnixNano'] and span[3] <= pause['endedUnixNano']]
        calls = sum(intervals[i]['readCalls'] for i in inside)
        require(calls > 10000, 'independent pressure during the reader pause was not established')
        result.update(pauseFullyCoveredProducerIntervals=len(inside), pauseMinimumReadCalls=calls)
    return result


def replay_pressure_session(record, raw, producer, witness, profile):
    kind = record['case']
    require(kind in ('ring-pressure', 'paused-pressure') and record['completed'] is True,
            'complete sustained session required')
    integer(record['index'], 0, profile['trace']['count'] - 1)
    require(digest(raw.encode()) == record['deliverySHA256'], 'pressure stream changed')
    requested, attached = record['requestedClock'], record['attachmentWitness']
    clock(requested)
    snapshot(attached)
    require(attached['workers'] == attached['activeControls'] == 1 and attached['excludedWorkers'] == 0
            and all(len(attached['objects'][key]) == count for key, count in OBJECTS.items()),
            'pressure attachment witness incomplete')
    require(attached['clock']['monotonicNanos'] >= requested['monotonicNanos'],
            'pressure attachment predates request')
    bounds = trace_bounds('ring-flood')
    integer(record['activeExpiresAtNanos'], attached['clock']['wallNanos'] + 1,
            requested['wallNanos'] + (bounds['durationSeconds'] + 5) * 1000000000)
    evaluate = evaluate_paused_reader_session if kind == 'paused-pressure' else evaluate_ceiling_session
    observed = evaluate(raw, expected_reason='event_limit', max_events=bounds['maxEvents'],
                        max_output_bytes=bounds['maxOutputBytes'], exit_code=record['deliveryExitCode'])
    require(observed == record['observation'], 'pressure observation differs from raw replay')
    budget = saturation(observed, bounds['maxEvents'])
    require(budget == record['saturation'], 'pressure saturation verdict changed')
    cleanup_pass = verify_cleanup(record, attached)
    cleaned = record['cleanupWitness']['snapshot']['clock']
    deadline_upper = max(0, cleaned['wallNanos'] + cleaned['uncertaintyNanos'] - record['activeExpiresAtNanos'])
    require(record['deadlineCleanupUpperNanos'] == deadline_upper
            and record['deadlineCleanupBudgetPassed'] is (deadline_upper < 2000000000),
            'pressure deadline cleanup verdict changed')
    passed = budget['saturationObserved'] and cleanup_pass and deadline_upper < 2000000000
    require(record['sessionChecksPassed'] is passed, 'pressure session verdict changed')
    counts = producer_bounds(producer, witness, record, profile['windowSeconds'],
                             profile['minimumObservedSessionIntervals'])
    return {'case': kind, 'sessionChecksPassed': passed, 'saturation': budget,
            'independentProducer': counts, 'deadlineCleanupUpperNanos': deadline_upper,
            'cancelCleanupElapsedNanos': record['cancelCleanupElapsedNanos'],
            'scope': 'one session bound to producer and kernel witnesses; paired qualification still required'}
