"""Evaluate each observed Node independently against the frozen budgets."""

import math


def percentile(values, percent):
    return sorted(values)[math.ceil(len(values) * percent / 100) - 1] if values else None


def coverage(samples, seconds, interval):
    if len(samples) < math.ceil(seconds / interval):
        return False
    times = [s["elapsedSeconds"] for s in samples]
    return 0 < times[0] <= 2 * interval and times[-1] >= seconds and all(0 < b - a <= 2 * interval for a, b in zip(times, times[1:]))


def column(samples, key):
    values = [s[key] for s in samples]
    return values if values and all(v is not None for v in values) else []


def cpu_mean(samples, key):
    if not column(samples, key) or samples[-1]["elapsedSeconds"] <= 0:
        return None
    total, previous = 0, 0
    for s in samples:
        total += s[key] * (s["elapsedSeconds"] - previous)
        previous = s["elapsedSeconds"]
    return total / previous


def measurement_checks(p, samples, rotation, producer_replicas):
    checks = []

    def check(name, passed, observed=None, limit=None):
        checks.append({"id": name, "passed": bool(passed), "observed": observed, "limit": limit})

    def maximum(name, observed, limit, exclusive=False):
        check(name, observed is not None and (observed < limit if exclusive else observed <= limit), observed, limit)

    m, b = p["measurement"], p["budgets"]
    baseline, enabled = samples["baseline"], samples["enabled"]
    for phase in ("baseline", "enabled"):
        rows = samples[phase]
        check(phase + "-coverage", coverage(rows, m[phase + "Seconds"], m["sampleIntervalSeconds"]), len(rows))
        replicas = producer_replicas if phase == "enabled" else 0
        check(phase + "-topology", bool(rows) and all(s["workloadContainers"] == p["workload"]["containers"] and s["mappedContainers"] == p["workload"]["containers"] and s["freshNodes"] == p["workload"]["linuxNodes"] and s["producerReplicas"] == replicas for s in rows))
        check(phase + "-stability", bool(rows) and all(s["unexpectedRestarts"] == 0 and s["unexpectedOOMKills"] == 0 for s in rows))
    topology, checks = checks, []
    maximum("producer-cpu", cpu_mean(enabled, "producerCPUMilli"), b["producerMeanCPUMilli"])
    memories = column(enabled, "producerMemoryBytes")
    check("producer-memory-measured", bool(memories) and all(v > 0 for v in memories))
    maximum("producer-memory", max(memories) if memories else None, b["producerPeakMemoryBytes"])
    before_cpu, after_cpu = cpu_mean(baseline, "kubeletCPUMilli"), cpu_mean(enabled, "kubeletCPUMilli")
    increase = max(0, after_cpu - before_cpu) if before_cpu is not None and after_cpu is not None else None
    maximum("kubelet-cpu-increase", increase, b["kubeletMeanCPUIncreaseMilli"])
    before_memory, after_memory = column(baseline, "kubeletMemoryBytes"), column(enabled, "kubeletMemoryBytes")
    increase = max(0, max(after_memory) - max(before_memory)) if before_memory and after_memory else None
    maximum("kubelet-memory-increase", increase, b["kubeletPeakMemoryIncreaseBytes"])
    scans = column(baseline + enabled, "agentScanSeconds")
    check("agent-scan-measured", bool(scans) and all(v > 0 for v in scans))
    maximum("agent-scan", percentile(scans, 99), b["agentScanP99Seconds"], exclusive=True)
    # A latest-value gauge is counted only when the acquisition counter advances.
    # Missing samples or any counter reset fail rather than biasing percentiles.
    reads, failures = column(enabled, "sourceReads"), column(enabled, "sourceFailures")
    monotonic = bool(reads and failures) and all(b >= a for a, b in zip(reads, reads[1:])) and all(b >= a for a, b in zip(failures, failures[1:]))
    check("source-counters", monotonic)
    check("source-failures", bool(failures) and failures[-1] == failures[0])
    acquired = [s for previous, s in zip(enabled, enabled[1:]) if previous["sourceReads"] is not None and s["sourceReads"] is not None and s["sourceReads"] > previous["sourceReads"]]
    # Jitter may put two requests between samples; at least half the intervals
    # must contain a new successful acquisition to qualify this cadence.
    check("source-acquisition-coverage", len(acquired) >= math.ceil(m["enabledSeconds"] / m["sampleIntervalSeconds"] / 2), len(acquired))
    latencies = column(acquired, "lastReadSeconds")
    maximum("read-latency", percentile(latencies, 95), b["readP95Seconds"])
    sizes = column(acquired, "lastResponseBytes")
    check("response-measured", bool(sizes) and all(v > 0 for v in sizes))
    maximum("response-size", max(sizes) if sizes else None, b["responseMaxBytes"])
    r = rotation
    check("projected-rotation", all(r[k] for k in ("observed", "sameProducer", "continuedAcquisition")) and r["elapsedSeconds"] is not None and 0 < r["elapsedSeconds"] <= m["enabledSeconds"])
    return {"topology": topology, "cost": checks}
