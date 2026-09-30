"""Resource accounting retains starts and gaps; it never grants qualification."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "ebpf-qualification"))
from idle_evaluate import integer, percentile, require


def working_set(group):
    return max(0, group["memoryCurrent"] - group["memory"]["inactive_file"])


def resource_summary(rows, active_intervals, minimum_active_seconds):
    require(len(active_intervals) == len(rows) - 1 and
            all(type(v) is bool for v in active_intervals), "missing attachment intervals")
    integer(minimum_active_seconds, 1, 1800)
    require(all({"node", "api"} <= row["groups"].keys() for row in rows), "missing installation services")
    elapsed = rows[-1]["elapsedNanos"] - rows[0]["elapsedNanos"]
    spans = [b["elapsedNanos"] - a["elapsedNanos"] for a, b in zip(rows, rows[1:])]
    active_nanos = sum(span for span, active in zip(spans, active_intervals) if active)
    require(active_nanos >= minimum_active_seconds * 1000000000, "insufficient observed attachment time")
    require(all(span > 0 for span in spans), "non-positive interval")
    combined = [sum(row["groups"][role]["cpu"]["usage_usec"] for role in ("node", "api")) for row in rows]
    delta = [b - a for a, b in zip(combined, combined[1:])]
    require(all(value >= 0 for value in delta), "installation CPU reset")
    rates = [value * 1000000 / span for value, span in zip(delta, spans)]
    active_rates = [rate for rate, active in zip(rates, active_intervals) if active]
    whole_mean = sum(delta) * 1000000 / elapsed
    conservative_mean = sum(delta) * 1000000 / active_nanos
    return {"scope": "resource-accounting-only", "windowNanos": elapsed,
            "observedActiveNanos": active_nanos, "activeIntervals": sum(active_intervals),
            "unprovenIntervals": len(active_intervals) - sum(active_intervals),
            "installationCPUUsec": sum(delta), "wholeWindowMeanCPUMilli": whole_mean,
            "allCostPerObservedActiveTimeCPUMilli": conservative_mean,
            "wholeWindowP99CPUMilli": percentile(rates, 99),
            "activeIntervalP99CPUMilli": percentile(active_rates, 99),
            "installationPeakWorkingSetBytes": max(sum(working_set(row["groups"][role])
                                                         for role in ("node", "api")) for row in rows),
            "normalCPUBudgetPassed": conservative_mean <= 10 and percentile(rates, 99) <= 30
                                     and percentile(active_rates, 99) <= 30,
            "qualification": "incomplete: paired workload, incremental memory, event, scan and lifecycle gates required"}
