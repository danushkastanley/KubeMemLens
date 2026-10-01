"""Replay a complete paired sustained-pressure case, retaining missing gates."""
from activity import validate_witness
from flood_resources import flood_resources
from pressure_metrics import compare_pressure
from pressure_profile import pressure_slots
from pressure_replay import replay_pressure_session
from processes import read_document
from provenance import verify_envelope
from samples import integer, read_samples, require
from scans import comparison
from scheduler_observations import scheduler_observations
from scheduler_capture import paired_scheduler
from verifier_capture import paired_verifier
from standard_window import standard_window
from verify_pressure import validate_pressure


def read_pressure(path):
    require(path.stat().st_size <= 1 << 20, 'pressure producer output exceeds bound')
    return path.read_text()


def evaluate_pressure_pair(control_directory, enabled_directory, profile):
    before_scope = verify_envelope(control_directory, 'control', profile)
    after_scope = verify_envelope(enabled_directory, 'enabled', profile)
    require(before_scope == after_scope, 'pressure source, configuration or kernel changed')
    before_rows = read_samples(control_directory / 'resources.jsonl')
    after_rows = read_samples(enabled_directory / 'resources.jsonl')
    control_raw, enabled_raw = (read_pressure(path / 'pressure.jsonl')
                                for path in (control_directory, enabled_directory))
    workload = compare_pressure(control_raw, enabled_raw, before_rows, after_rows,
        pressure_seconds=profile['producerSeconds'], window_seconds=profile['windowSeconds'],
        minimum_common_intervals=profile['minimumCommonIntervals'])
    resources = flood_resources(after_rows, profile['windowSeconds'],
        {'node', 'api', 'agent', 'collector', 'selected'}, profile['resourceCaps'])
    control = read_document(control_directory / 'window.private.json')
    enabled = read_document(enabled_directory / 'window.private.json')
    require(control['completed'] and enabled['completed'] and not control['cleanupFailures']
            and not enabled['cleanupFailures'], 'pressure window or cleanup incomplete')
    for field in ('fixtureIdentities', 'standardIdentities', 'fixtureMapping', 'targetBindings'):
        require(control[field] == enabled[field], 'pressure pair identity or density changed')
    require(not control['sessions'], 'trace admissions found in pressure control')
    require(sum(row['mappedContainers'] for row in enabled['fixtureMapping']) == profile['workloadContainers'],
            'pressure fixture density is incomplete')
    witness = read_samples(enabled_directory / 'witness.jsonl')
    validate_witness(witness, profile['windowSeconds'])
    producer = validate_pressure(enabled_raw, profile['producerSeconds'])
    schedule = pressure_slots(profile)
    require(len(enabled['sessions']) == len(schedule), 'pressure session inventory incomplete')
    sessions = []
    for record, (index, kind, offset) in zip(enabled['sessions'], schedule):
        require(record['index'] == index and record['case'] == kind
                and record['scheduledOffsetSeconds'] == offset, 'pressure session order changed')
        integer(record['slotLatenessNanos'], 0, profile['maximumSlotLatenessNanos'])
        require(after_rows[0]['wallNanos'] <= record['requestedClock']['wallNanos']
                < record['activeExpiresAtNanos'] <= after_rows[-1]['wallNanos'],
                'pressure session lies outside its sampled window')
        path = enabled_directory / f'delivery-pressure-{index:03}.jsonl'
        require(path.stat().st_size <= 65536, 'pressure delivery output exceeds bound')
        sessions.append(replay_pressure_session(record, path.read_text(), producer, witness, profile))
    observations = [row['snapshot'] for row in witness if row['state'] == 'observed']
    require(bool(observations), 'pressure map observations missing')
    allocations = {'unavailableSamples': len(witness) - len(observations),
        'kernelMapPeakBytes': max(row['kernelMapBytes'] for row in observations),
        'userRingReservePeakBytes': max(row['userMapBytes'] for row in observations)}
    allocations['observedMapBudgetPassed'] = (allocations['unavailableSamples'] == 0
        and allocations['kernelMapPeakBytes'] <= profile['trace']['maxMapBytes'])
    standard_before = standard_window(read_samples(control_directory / 'standard.jsonl'), profile['windowSeconds'])
    standard_after = standard_window(read_samples(enabled_directory / 'standard.jsonl'), profile['windowSeconds'])
    require(min(len(standard_before['scans']), len(standard_after['scans'])) >= profile['minimumScans'],
            'insufficient complete pressure scan observations')
    scans = comparison(standard_before['scans'], standard_after['scans'])
    passed = (resources['observedContainmentPassed'] and workload['selectedMatchedThroughputBudgetPassed']
              and allocations['observedMapBudgetPassed'] and all(row['sessionChecksPassed'] for row in sessions))
    return {'schemaVersion': 1, 'case': profile['case'], 'measuredPressureBudgetsPassed': passed,
        'workload': workload, 'resource': resources, 'allocations': allocations, 'sessions': sessions,
        'standardScans': scans,
        'nodeSchedulerLatency': paired_scheduler(control_directory, enabled_directory, profile['windowSeconds'], before_scope),
        'verifier': paired_verifier(control_directory, enabled_directory, profile['windowSeconds'], before_scope),
        'nodeSchedulerCounters': {'control': scheduler_observations(before_rows),
                                  'enabled': scheduler_observations(after_rows)},
        'controlCollectorMissingDurations': standard_before['collectorMissingDurations'],
        'enabledCollectorMissingDurations': standard_after['collectorMissingDurations'],
        'unavailableMeasurements': ['pressure event-delivery latency distribution'],
        'qualification': 'measured sustained-pressure case only; finite writer checks, missing observations, isolation, lifecycle and provider gates remain required'}
