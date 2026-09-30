"""Replay the measured normal-case budgets; keep remaining qualification explicit."""
import json
from pathlib import Path

from activity import active_intervals
from memory import paired_memory
from processes import read_document
from provenance import verify_envelope
from resources import resource_summary
from samples import read_samples, require, validate_window
from scans import compare_scans
from standard_window import standard_window
from workload import compare_workloads


def evaluate_pair(control_directory, enabled_directory, profile):
    control_scope = verify_envelope(control_directory, 'control', profile)
    enabled_scope = verify_envelope(enabled_directory, 'enabled', profile)
    require(control_scope == enabled_scope, 'pair source, configuration or kernel scope changed')
    seconds = profile['windowSeconds']
    roles = {'agent', 'collector', 'selected'}
    before = read_samples(control_directory / 'resources.jsonl')
    after = read_samples(enabled_directory / 'resources.jsonl')
    validate_window(before, seconds, roles)
    validate_window(after, seconds, roles | {'node', 'api'})
    witness = read_samples(enabled_directory / 'witness.jsonl')
    active = active_intervals(after, witness, seconds, profile['trace']['objects'])
    cpu = resource_summary(after, active, profile['minimumActiveSeconds'])
    memory = paired_memory(before, after, seconds, roles)
    work = profile['workload']
    workload = compare_workloads((control_directory / 'workload.jsonl').read_text(),
                                 (enabled_directory / 'workload.jsonl').read_text(),
                                 mode=work['mode'], count=work['count'], period_ms=work['periodMilliseconds'],
                                 witness=witness, seconds=seconds, expected_objects=profile['trace']['objects'],
                                 minimum_active_operations=profile['minimumActiveOperations'])
    standard_before = read_samples(control_directory / 'standard.jsonl')
    standard_after = read_samples(enabled_directory / 'standard.jsonl')
    scans = compare_scans(standard_before, standard_after, seconds=seconds, witness=witness,
                         expected_objects=profile['trace']['objects'], minimum_scans=profile['minimumScans'],
                         minimum_active_scans=profile['minimumActiveScans'])
    control = read_document(control_directory / 'window.private.json')
    enabled = read_document(enabled_directory / 'window.private.json')
    require(control['completed'] and enabled['completed'] and not control['cleanupFailures']
            and not enabled['cleanupFailures'], 'window or cleanup incomplete')
    require(control['fixtureIdentities'] == enabled['fixtureIdentities']
            and control['standardIdentities'] == enabled['standardIdentities'], 'pair uses different service/workload lifetimes')
    require(control['fixtureMapping'] == enabled['fixtureMapping'] and
            sum(r['mappedContainers'] for r in enabled['fixtureMapping']) == profile['workloadContainers'],
            'paired production workload mapping incomplete')
    sessions = enabled['sessions']
    require(len(sessions) == profile['trace']['count'] and [s['index'] for s in sessions] == list(range(len(sessions))),
            'normal session inventory incomplete')
    # Every emitted event was checked by the bound native reader; aggregate only
    # numeric counts/latencies here. Do not flatten per-trace quantiles into one.
    delivery_pass = all(s['latency']['normalLossBudgetPassed'] and s['latency']['eventDeliveryBudgetPassed'] for s in sessions)
    deadline_pass = all(s['zeroOwnedState'] and s['deadlineTeardownUpperNanos'] < 2000000000 for s in sessions)
    measured = (cpu['normalCPUBudgetPassed'] and memory['normalObservedWorkingSetBudgetPassed']
                and workload['normalSelectedLatencyBudgetPassed'] and scans['normalScanBudgetPassed']
                and delivery_pass and deadline_pass)
    observed = [r['snapshot'] for r in witness if r['state'] == 'observed']
    require(bool(observed), 'no observed allocation census')
    return {'schemaVersion': 1, 'case': profile['case'], 'measuredNormalBudgetsPassed': measured,
            'resource': cpu, 'memory': memory, 'workload': workload, 'standardScans': scans,
            'sessions': sessions, 'normalEventBudgetsPassed': delivery_pass,
            'deadlineCleanupBudgetPassed': deadline_pass,
            'kernelMapPeakBytes': max(r['kernelMapBytes'] for r in observed),
            'userRingReservePeakBytes': max(r['userMapBytes'] for r in observed),
            'allocationUnavailableSamples': len(witness) - len(observed),
            'qualification': 'normal measured budgets only; idle, other workload/lifecycle cases, remaining observations and provider gates remain required',
            'unavailableMeasurements': ['node-wide scheduler latency percentiles', 'isolated verifier duration/log size'],
            'admissionTimingMethod': 'end-to-end conservative admission plus attachment upper bound'}
