"""Keep both selected latency verdicts under the same two-target witness."""
from samples import require
from workload import compare_workloads


TARGETS = ('target-0', 'target-1')


def compare_concurrent_workloads(control_raw, enabled_raw, *, mode, count, period_ms,
                                 witness, seconds, expected_objects, minimum_active_operations):
    require(type(control_raw) is dict and type(enabled_raw) is dict
            and set(control_raw) == set(enabled_raw) == set(TARGETS),
            'exactly two selected workload streams required')
    results = {}
    for target in TARGETS:
        results[target] = compare_workloads(
            control_raw[target], enabled_raw[target], mode=mode, count=count,
            period_ms=period_ms, witness=witness, seconds=seconds,
            expected_objects=expected_objects, minimum_active_operations=minimum_active_operations,
            expected_workers=2)
    return {
        'scope': 'two-selected-operation-latency-only',
        'workloads': results,
        'concurrentSelectedLatencyBudgetPassed': all(
            result['normalSelectedLatencyBudgetPassed'] for result in results.values()),
        'qualification': 'incomplete: stream-to-target identity bindings, resource, event, scan and lifecycle gates required',
    }
