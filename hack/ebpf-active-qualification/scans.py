"""Paired scan p95 with whole-scan attachment bracketing and explicit gaps."""
from activity import active_signatures, bracket_windows, validate_witness
from resources import percentile
from samples import integer, require
from standard_window import standard_window


def comparison(control, enabled):
    require(bool(control) and len(control) == len(enabled), 'unmatched complete scan count')
    before = percentile([r['durationNanos'] for r in control], 95)
    after = percentile([r['durationNanos'] for r in enabled], 95)
    return {'scans': len(control), 'controlP95Nanos': before, 'enabledP95Nanos': after,
            'regressionPercent': (after - before) * 100 / before,
            'regressionBelowFivePercent': after * 100 < before * 105}


def compare_scans(control_rows, enabled_rows, *, seconds, witness, expected_objects,
                  minimum_scans, minimum_active_scans, expected_workers=1):
    before = standard_window(control_rows, seconds)
    after = standard_window(enabled_rows, seconds)
    require(control_rows[0]['schemaVersion'] == enabled_rows[0]['schemaVersion'],
            'paired scan observation schemas differ')
    control, enabled = before['scans'], after['scans']
    integer(minimum_scans, 1, seconds)
    integer(minimum_active_scans, 1, minimum_scans)
    require(len(control) == len(enabled) and len(control) >= minimum_scans,
            'insufficient or unmatched complete scan observations')
    starts, ends = validate_witness(witness, seconds, expected_workers=expected_workers)
    signatures = active_signatures(witness, expected_objects, expected_workers=expected_workers)
    mask = bracket_windows(starts, ends, signatures,
                           [(r['earliestStartWallNanos'], r['latestEndWallNanos']) for r in enabled])
    require(sum(mask) >= minimum_active_scans, 'insufficient scans wholly bracketed by attachments')
    complete = comparison(control, enabled)
    active = comparison([r for r, yes in zip(control, mask) if yes],
                        [r for r, yes in zip(enabled, mask) if yes])
    return {'scope': 'paired standard agent scan duration only', 'allScans': complete,
            'fullyBracketedScans': active, 'unprovenOrTransitionScans': len(mask) - sum(mask),
            'normalScanBudgetPassed': complete['regressionBelowFivePercent'] and active['regressionBelowFivePercent'],
            'controlCollectorMissingDurations': before['collectorMissingDurations'],
            'enabledCollectorMissingDurations': after['collectorMissingDurations'],
            'qualification': 'incomplete: provenance, density, resources, events and lifecycle gates required'}
