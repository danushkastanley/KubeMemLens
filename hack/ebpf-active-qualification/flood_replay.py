"""Recompute single-session flood results from frozen numeric streams."""
from activity import snapshot
from ceiling_result import evaluate_ceiling_session
from flood_result import kernel_budget
from flood_session import CASES, OBJECTS, pause_covers_burst, trace_bounds, workload_spec
from local_case import digest
from paused_reader_result import evaluate_paused_reader_session
from samples import integer, require
from session_cleanup_replay import verify_cleanup
from verify_flood import validate_flood, validate_paced_flood


def replay_session(record, raw, workload):
    kind = record['case']
    require(kind in CASES and record['completed'] is True and record['readyBeforeBurst'] is True,
            'complete bounded flood record required')
    integer(record['index'], 0, 999)
    require(digest(raw.encode()) == record['deliverySHA256'], 'flood stream changed')
    bounds = trace_bounds(kind)
    spec = workload_spec(kind)
    validate = validate_paced_flood if kind == 'event-limit' else validate_flood
    receipt = validate(workload, spec['count'])
    require(receipt == record['workload'], 'independent workload receipt changed')
    witness = record['attachmentWitness']
    snapshot(witness)
    require(witness['workers'] == witness['activeControls'] == 1 and witness['excludedWorkers'] == 0
            and all(len(witness['objects'][key]) == count for key, count in OBJECTS.items()),
            'flood attachment witness incomplete')
    require(receipt['operationStartedMonotonicNanos'] >= witness['clock']['monotonicNanos'],
            'flood workload predates complete attachment observation')
    evaluate = evaluate_paused_reader_session if kind == 'paused-reader' else evaluate_ceiling_session
    observed = evaluate(raw, expected_reason=CASES[kind][1], max_events=bounds['maxEvents'],
                        max_output_bytes=bounds['maxOutputBytes'], exit_code=record['deliveryExitCode'])
    require(observed == record['observation'], 'flood observation differs from replay')
    require(record['requestedCeilingObserved'] is observed['requestedCeilingObserved'],
            'writer ceiling verdict changed')
    passed = observed['requestedCeilingObserved']
    if kind in ('ring-flood', 'paused-reader'):
        budget = kernel_budget(observed, receipt, bounds['maxEvents'])
        require(budget == record['kernelBudget'], 'kernel budget differs from replay')
        passed = budget['kernelBudgetAndLossPassed']
    if kind == 'paused-reader':
        covered = pause_covers_burst(observed, receipt, witness['clock'])
        require(record['burstFullyWithinPause'] is covered, 'pause overlap verdict changed')
        passed = passed and covered
    cleanup = verify_cleanup(record, witness)
    passed = passed and cleanup
    require(record['sessionChecksPassed'] is passed, 'flood session verdict changed')
    return {'case': kind, 'sessionChecksPassed': passed, 'observation': observed,
            'workload': receipt, 'cancelCleanupBudgetPassed': cleanup,
            'scope': 'single-session numeric replay; source and stream provenance required'}
