"""Compare every mixed operation class without hiding it in an aggregate p99."""
from workload import latency_comparison, workload_activity
from verify_series import MIXED_MODES, validate_mixed_series
from samples import integer, require


def comparisons(before, after, mask):
    active_before = [row for row, active in zip(before, mask) if active]
    active_after = [row for row, active in zip(after, mask) if active]
    other_before = [row for row, active in zip(before, mask) if not active]
    other_after = [row for row, active in zip(after, mask) if not active]
    all_result = latency_comparison(before, after)
    active_result = latency_comparison(active_before, active_after)
    return {"allOperations": all_result, "fullyBracketedOperations": active_result,
            "unprovenOrTransitionOperations": latency_comparison(other_before, other_after) if other_before else None,
            "selectedLatencyBudgetPassed": all_result["p99RegressionBelowTwoPercent"]
                                           and active_result["p99RegressionBelowTwoPercent"]}


def compare_mixed_workloads(control_raw, enabled_raw, *, count, period_ms,
                            witness, seconds, expected_objects, minimum_active_operations_per_mode):
    control = validate_mixed_series(control_raw, count, period_ms)
    enabled = validate_mixed_series(enabled_raw, count, period_ms)
    integer(minimum_active_operations_per_mode, 1, count // len(MIXED_MODES))
    before, after = control[1:], enabled[1:]
    mask = workload_activity(enabled, witness, seconds, expected_objects)
    by_mode = {}
    for offset, mode in enumerate(MIXED_MODES):
        selected = mask[offset::len(MIXED_MODES)]
        require(sum(selected) >= minimum_active_operations_per_mode,
                "insufficient fully bracketed operations for " + mode)
        by_mode[mode] = comparisons(before[offset::len(MIXED_MODES)], after[offset::len(MIXED_MODES)], selected)
    combined = comparisons(before, after, mask)
    scheduled_nanos = count * period_ms * 1000000
    read_bytes = sum(row['observation']['readBytes'] for row in before)
    write_bytes = sum(row['observation']['writeBytes'] for row in before)
    return {"scope": "selected-mixed-operation-latency-only", "operationCycle": list(MIXED_MODES),
            "scheduledOperations": count, "scheduledNanos": scheduled_nanos,
            "completedOperationsControl": len(before), "completedOperationsEnabled": len(after),
            "scheduledReadBytes": read_bytes, "scheduledWriteBytes": write_bytes,
            "achievedScheduledBytesPerSecond": (read_bytes + write_bytes) * 1000000000 / scheduled_nanos,
            "throughputMeaning": "completed fixed workload per scheduled second, not maximum storage throughput",
            "combined": combined, "byMode": by_mode,
            "mixedSelectedLatencyBudgetPassed": combined['selectedLatencyBudgetPassed']
                                                 and all(result['selectedLatencyBudgetPassed'] for result in by_mode.values()),
            "qualification": "incomplete: resource, event, scan, collector and lifecycle gates required"}
