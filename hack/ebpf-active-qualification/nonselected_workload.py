"""Require each of ten non-selected workloads to stay below 1% p99 regression."""
from workload import latency_summary, workload_activity
from verify_series import validate_series
from samples import integer, require

ROLES = tuple(f'nonselected-{index}' for index in range(10))


def latency_comparison(before, after):
    control, enabled = latency_summary(before), latency_summary(after)
    require(control['operations'] == enabled['operations'], 'unmatched non-selected operations')
    return {'control': control, 'enabled': enabled,
            'p99RegressionPercent': (enabled['p99Nanos'] - control['p99Nanos']) * 100 / control['p99Nanos'],
            'p99RegressionBelowOnePercent': enabled['p99Nanos'] * 100 < control['p99Nanos'] * 101}


def compare_nonselected(control_raw, enabled_raw, *, count, period_ms, witness, seconds,
                        expected_objects, minimum_active_operations):
    require(type(control_raw) is dict and type(enabled_raw) is dict
            and set(control_raw) == set(enabled_raw) == set(ROLES), 'exactly ten non-selected streams required')
    integer(minimum_active_operations, 1, count)
    results = {}
    for role in ROLES:
        control = validate_series(control_raw[role], 'noise', count, period_ms)
        enabled = validate_series(enabled_raw[role], 'noise', count, period_ms)
        before, after = control[1:], enabled[1:]
        mask = workload_activity(enabled, witness, seconds, expected_objects)
        require(sum(mask) >= minimum_active_operations, 'insufficient bracketed non-selected operations')
        active_before = [row for row, active in zip(before, mask) if active]
        active_after = [row for row, active in zip(after, mask) if active]
        other_before = [row for row, active in zip(before, mask) if not active]
        other_after = [row for row, active in zip(after, mask) if not active]
        complete = latency_comparison(before, after)
        active = latency_comparison(active_before, active_after)
        results[role] = {'allOperations': complete, 'fullyBracketedOperations': active,
                         'unprovenOrTransitionOperations': latency_comparison(other_before, other_after) if other_before else None,
                         'nonselectedLatencyBudgetPassed': complete['p99RegressionBelowOnePercent']
                                                           and active['p99RegressionBelowOnePercent']}
    return {'scope': 'ten-nonselected-operation-latency-only', 'workloads': results,
            'nonselectedLatencyBudgetPassed': all(r['nonselectedLatencyBudgetPassed'] for r in results.values()),
            'qualification': 'incomplete: zero non-selected events and remaining resource, event, scan and lifecycle gates required'}
