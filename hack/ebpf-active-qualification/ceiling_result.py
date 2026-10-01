"""Replay a bounded ceiling observation without inventing loss or resource claims."""
from samples import exact, integer, require, strict_json

TERMINATIONS = {'expired', 'cancelled', 'target_changed', 'output_limit', 'event_limit',
                'engine_failed', 'authorisation_lost'}
COUNTS = {'produced', 'sampled', 'lost', 'rejected'}
# Accepted aggregate frame protocol: 4 KiB terminal reserve, 8 KiB frame bound.
TERMINAL_RESERVE = 4096
MAX_FRAME = 8192


def evaluate_ceiling_session(raw, **bounds):
    require(type(raw) is str and len(raw.encode()) <= 65536 and raw.endswith('\n'),
            'incomplete or unbounded ceiling session output')
    lines = raw.splitlines()
    require(len(lines) == 2, 'one readiness record and one terminal observation required')
    ready = strict_json(lines[0])
    exact(ready, {'schemaVersion', 'case', 'ready'})
    integer(ready['schemaVersion'], 1, 1)
    require(ready['case'] == 'ceiling-ready' and ready['ready'] is True, 'ceiling readiness unconfirmed')
    result = evaluate_ceiling(lines[1], **bounds)
    result['readyBeforeResult'] = True
    return result


def evaluate_ceiling(raw, *, expected_reason, max_events, max_output_bytes, exit_code):
    require(type(expected_reason) is str and expected_reason in {'event_limit', 'output_limit'},
            'explicit requested ceiling required')
    integer(max_events, 1, 10000)
    integer(max_output_bytes, 1, 8 << 20)
    integer(exit_code, 0, 1)
    require(type(raw) is str and len(raw.encode()) <= 65536, 'ceiling result exceeds bound')
    document = strict_json(raw)
    exact(document, {'schemaVersion', 'case', 'sameKernelClock', 'observation'})
    integer(document['schemaVersion'], 1, 1)
    require(document['case'] == 'ceiling-observation' and document['sameKernelClock'] is True,
            'ceiling case or kernel identity mismatch')
    value = document['observation']
    exact(value, {'schemaVersion', 'metadataMatched', 'transportComplete', 'ceilingReported',
                  'frames', 'events', 'encodedBytes', 'bytesBeforeSummary', 'termination',
                  'hookCoverageIncomplete', 'counts', 'clientClockDriftNanos'})
    integer(value['schemaVersion'], 1, 1)
    require(value['metadataMatched'] is True and value['transportComplete'] is True
            and value['hookCoverageIncomplete'] is True, 'ceiling transport incomplete')
    require(type(value['ceilingReported']) is bool and type(value['termination']) is str
            and value['termination'] in TERMINATIONS,
            'invalid ceiling outcome')
    integer(value['events'], 0, max_events)
    integer(value['frames'], 2, max_events + 2)
    integer(value['encodedBytes'], 1, max_output_bytes)
    integer(value['bytesBeforeSummary'], 1, value['encodedBytes'] - 1)
    integer(value['clientClockDriftNanos'], 0, 5000000)
    require(value['frames'] == value['events'] + 2
            and value['encodedBytes'] - value['bytesBeforeSummary'] <= TERMINAL_RESERVE,
            'ceiling frame or terminal byte accounting mismatch')
    reserved = value['bytesBeforeSummary'] + TERMINAL_RESERVE
    require(reserved <= max_output_bytes, 'stream consumed its terminal reserve')
    reported = ((value['termination'] == 'event_limit' and value['events'] == max_events)
                or (value['termination'] == 'output_limit' and max_output_bytes - reserved < MAX_FRAME))
    require(value['ceilingReported'] is reported and (exit_code == 0) is reported,
            'ceiling evidence, reported verdict and process exit disagree')
    counts = value['counts']
    exact(counts, COUNTS)
    for count in counts.values():
        if count is not None:
            integer(count)
    lower_bound = value['events'] + sum(counts[key] for key in COUNTS - {'produced'} if counts[key] is not None)
    require(lower_bound <= 2**64 - 1, 'engine count lower bound overflows')
    if counts['produced'] is not None:
        require(counts['produced'] >= lower_bound, 'engine counts contradict delivered events')
    return {'scope': 'reported-admitted-ceiling-only', 'requestedCeiling': expected_reason,
            'termination': value['termination'],
            'requestedCeilingObserved': reported and value['termination'] == expected_reason,
            'transportComplete': True, 'events': value['events'], 'encodedBytes': value['encodedBytes'],
            'engineCounts': dict(counts),
            'qualification': 'incomplete: saturation, resources, isolation, lifecycle and paired campaign evidence required'}
