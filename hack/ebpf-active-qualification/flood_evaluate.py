"""Pair complete flood windows without turning missing observations into a pass."""
from activity import clock, validate_witness
from flood_profile import KINDS, slots, stem
from flood_replay import replay_session
from flood_resources import flood_resources, validate_containment_window
from flood_session import workload_spec
from processes import read_document
from provenance import verify_envelope
from samples import integer, read_samples, require
from scans import comparison
from scheduler_observations import scheduler_observations
from scheduler_capture import paired_scheduler
from verifier_capture import paired_verifier
from standard_window import standard_window
from verify_flood import validate_flood, validate_paced_flood


def operation_in_window(receipt, anchor, resource_rows):
    clock(anchor)
    require(receipt['operationStartedMonotonicNanos'] >= anchor['monotonicNanos'],
            'workload predates its clock anchor')
    offset = anchor['wallNanos'] - anchor['monotonicNanos']
    margin = anchor['uncertaintyNanos'] + 5000000
    require(receipt['operationStartedMonotonicNanos'] + offset - margin >= resource_rows[0]['wallNanos']
            and receipt['operationEndedMonotonicNanos'] + offset + margin <= resource_rows[-1]['wallNanos'],
            'flood workload lies outside its resource window')


def sessions(directory, window, phase, profile, resource_rows):
    records = window['sessions']
    schedule = slots(profile)
    require(len(records) == len(schedule), 'flood session inventory incomplete')
    results = []
    for record, (index, kind, offset) in zip(records, schedule):
        require(record['index'] == index and record['case'] == kind
                and record['scheduledOffsetSeconds'] == offset and record['completed'] is True,
                'flood session ordering or schedule changed')
        integer(record['slotLatenessNanos'], 0, profile['maximumSlotLatenessNanos'])
        name = stem(index, kind)
        receipt = read_document(directory / (name + '-workload.json'), maximum=4096)
        validate = validate_paced_flood if kind == 'event-limit' else validate_flood
        validate(receipt, workload_spec(kind)['count'])
        require(record['workload'] == receipt, 'flood workload receipt mismatch')
        anchor = record['attachmentWitness']['clock'] if phase == 'enabled' else record['operationAnchor']
        operation_in_window(receipt, anchor, resource_rows)
        if phase == 'enabled':
            path = directory / (name + '.jsonl')
            require(path.stat().st_size <= 65536, 'flood delivery exceeds bound')
            results.append(replay_session(record, path.read_text(), receipt))
        else:
            results.append({'case': kind, 'workload': receipt})
    return results


def compare_workloads(control, enabled):
    require(len(control) == len(enabled) == 20, 'paired flood bursts missing')
    comparisons = []
    for index, (before, after) in enumerate(zip(control, enabled)):
        require(before['case'] == after['case'] == KINDS[index % 4], 'paired burst order changed')
        a, b = before['workload'], after['workload']
        require(a['readBytes'] == b['readBytes'] and a['readCalls'] == b['readCalls'],
                'paired flood workload volume differs')
        for value in (a['operationNanos'], b['operationNanos']):
            integer(value, 1)
        # Equal known byte totals: throughput is inversely proportional to time.
        passed = a['operationNanos'] * 100 > b['operationNanos'] * 98
        comparisons.append({'index': index, 'case': before['case'],
            'controlOperationNanos': a['operationNanos'], 'enabledOperationNanos': b['operationNanos'],
            'throughputRegressionPercent': (1 - a['operationNanos'] / b['operationNanos']) * 100,
            'throughputRegressionBelowTwoPercent': passed})
    return {'scope': 'fixed-volume burst throughput; excludes time waiting for controller commands',
            'pairs': comparisons, 'selectedThroughputBudgetPassed': all(
                value['throughputRegressionBelowTwoPercent'] for value in comparisons)}


def evaluate_flood_pair(control_directory, enabled_directory, profile):
    before_scope = verify_envelope(control_directory, 'control', profile)
    after_scope = verify_envelope(enabled_directory, 'enabled', profile)
    require(before_scope == after_scope, 'flood source, configuration or kernel changed')
    seconds = profile['windowSeconds']
    roles = {'agent', 'collector', 'selected'}
    before_rows = read_samples(control_directory / 'resources.jsonl')
    after_rows = read_samples(enabled_directory / 'resources.jsonl')
    validate_containment_window(before_rows, seconds, roles)
    resources = flood_resources(after_rows, seconds, roles | {'node', 'api'}, profile['resourceCaps'])
    control = read_document(control_directory / 'window.private.json')
    enabled = read_document(enabled_directory / 'window.private.json')
    require(control['completed'] and enabled['completed'] and not control['cleanupFailures']
            and not enabled['cleanupFailures'], 'incomplete flood window or cleanup')
    for field in ('fixtureIdentities', 'standardIdentities', 'fixtureMapping', 'targetBindings'):
        require(control[field] == enabled[field], 'flood pair identity or mapping changed')
    require(sum(row['mappedContainers'] for row in enabled['fixtureMapping']) == profile['workloadContainers'],
            'flood density mapping incomplete')
    before = sessions(control_directory, control, 'control', profile, before_rows)
    after = sessions(enabled_directory, enabled, 'enabled', profile, after_rows)
    workload = compare_workloads(before, after)
    witness = read_samples(enabled_directory / 'witness.jsonl')
    validate_witness(witness, seconds)
    observed = [row['snapshot'] for row in witness if row['state'] == 'observed']
    require(bool(observed), 'no flood allocation census')
    allocations = {'unavailableSamples': len(witness) - len(observed),
                   'kernelMapPeakBytes': max(row['kernelMapBytes'] for row in observed),
                   'userRingReservePeakBytes': max(row['userMapBytes'] for row in observed)}
    allocations['observedMapBudgetPassed'] = (allocations['unavailableSamples'] == 0
        and allocations['kernelMapPeakBytes'] <= profile['trace']['maxMapBytes'])
    standard_before = standard_window(read_samples(control_directory / 'standard.jsonl'), seconds)
    standard_after = standard_window(read_samples(enabled_directory / 'standard.jsonl'), seconds)
    require(min(len(standard_before['scans']), len(standard_after['scans'])) >= profile['minimumScans'],
            'insufficient complete flood scan observations')
    scans = comparison(standard_before['scans'], standard_after['scans'])
    # The 5% scan threshold is explicitly a normal-trace requirement. Retain the
    # same comparison here without describing it as that separate normal case.
    mechanism = all(result['sessionChecksPassed'] for result in after)
    passed = (mechanism and resources['observedContainmentPassed']
              and workload['selectedThroughputBudgetPassed'] and allocations['observedMapBudgetPassed'])
    return {'schemaVersion': 1, 'case': profile['case'], 'measuredFloodBudgetsPassed': passed,
            'sessionMechanismsPassed': mechanism, 'sessions': after, 'resource': resources,
            'workload': workload, 'allocations': allocations, 'standardScans': scans,
            'nodeSchedulerLatency': paired_scheduler(control_directory, enabled_directory, profile['windowSeconds'], before_scope),
            'verifier': paired_verifier(control_directory, enabled_directory, profile['windowSeconds'], before_scope),
            'nodeSchedulerCounters': {'control': scheduler_observations(before_rows),
                                      'enabled': scheduler_observations(after_rows)},
            'controlCollectorMissingDurations': standard_before['collectorMissingDurations'],
            'enabledCollectorMissingDurations': standard_after['collectorMissingDurations'],
            'unavailableMeasurements': [    'flood event-delivery latency distribution'],
            'qualification': 'measured flood case only; missing observations, isolation, lifecycle and provider gates remain required'}
