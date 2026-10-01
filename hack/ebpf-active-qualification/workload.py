"""Compare complete fixed-schedule operations with their exact paired indices."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[2] /
                       "prototype/trace/qualification/filecache"))
from verify_series import validate_series
from activity import operation_activity
from samples import integer, require
from resources import percentile


def latency_summary(rows):
    values = [row["observation"]["operationNanos"] for row in rows]
    require(bool(values), "no matched workload observations")
    return {"operations": len(values), "meanNanos": sum(values) / len(values),
            "p50Nanos": percentile(values, 50), "p95Nanos": percentile(values, 95),
            "p99Nanos": percentile(values, 99), "maximumNanos": max(values),
            "totalOperationNanos": sum(values)}


def latency_comparison(control, enabled):
    before, after = latency_summary(control), latency_summary(enabled)
    require(before["operations"] == after["operations"], "unmatched operation count")
    # The protocol says below 2%, so equality fails. Integer arithmetic keeps the
    # decision exact even when its displayed percentage has floating-point noise.
    passed = after["p99Nanos"] * 100 < before["p99Nanos"] * 102
    return {"control": before, "enabled": after,
            "p99RegressionPercent": (after["p99Nanos"] - before["p99Nanos"]) * 100 / before["p99Nanos"],
            "p99RegressionBelowTwoPercent": passed}


def compare_workloads(control_raw, enabled_raw, *, mode, count, period_ms,
                      witness, seconds, expected_objects, minimum_active_operations):
    control = validate_series(control_raw, mode, count, period_ms)
    enabled = validate_series(enabled_raw, mode, count, period_ms)
    integer(minimum_active_operations, 1, count)
    before, after = control[1:], enabled[1:]
    mask = workload_activity(enabled, witness, seconds, expected_objects)
    require(sum(mask) >= minimum_active_operations, "insufficient fully bracketed workload operations")
    active_before = [row for row, active in zip(before, mask) if active]
    active_after = [row for row, active in zip(after, mask) if active]
    all_result = latency_comparison(before, after)
    active_result = latency_comparison(active_before, active_after)
    other_before = [row for row, active in zip(before, mask) if not active]
    other_after = [row for row, active in zip(after, mask) if not active]
    scheduled_nanos = count * period_ms * 1000000
    read_bytes = sum(r["observation"]["readBytes"] for r in before)
    written_bytes = sum(r["observation"]["writeBytes"] for r in before)
    return {"scope": "selected-operation-latency-only", "mode": mode,
            "scheduledOperations": count, "scheduledNanos": scheduled_nanos,
            "completedOperationsControl": len(before), "completedOperationsEnabled": len(after),
            "scheduledReadBytes": read_bytes, "scheduledWriteBytes": written_bytes,
            "achievedScheduledBytesPerSecond": (read_bytes + written_bytes) * 1000000000 / scheduled_nanos,
            "throughputMeaning": "completed fixed workload per scheduled second, not maximum storage throughput",
            "allOperations": all_result, "fullyBracketedOperations": active_result,
            "unprovenOrTransitionOperations": latency_comparison(other_before, other_after) if other_before else None,
            "normalSelectedLatencyBudgetPassed": all_result["p99RegressionBelowTwoPercent"]
                                                 and active_result["p99RegressionBelowTwoPercent"],
            "qualification": "incomplete: resource, event, scan, collector and lifecycle gates required"}


def workload_activity(enabled, witness, seconds, expected_objects):
    mask = operation_activity(enabled[1:], witness, seconds, expected_objects)
    alignment = enabled[0]["wallNanos"] - (enabled[0]["monotonicBeforeNanos"] + enabled[0]["monotonicAfterNanos"]) // 2
    witness_alignment = witness[0]["clock"]["wallNanos"] - witness[0]["clock"]["monotonicNanos"]
    require(abs(alignment - witness_alignment) <= 105500000,
            "workload and attachment witness clocks are not aligned")
    return mask
