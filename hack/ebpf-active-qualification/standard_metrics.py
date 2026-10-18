"""Bounded numeric projections from existing loopback OpenMetrics endpoints."""
from decimal import Decimal, InvalidOperation, ROUND_CEILING
import re

from samples import require

MAX_COUNTER = 2**53 - 1  # Collector renders its uint64 counters through float64.
NUMBER = re.compile(r"[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\Z")
AGENT = "kubememlens_agent_"
COLLECTOR = "kubememlens_collector_ingestion_"
RESULTS = {"accepted", "duplicate", "identity_rejected", "method_not_allowed",
           "content_encoding_rejected", "unsupported_media_type", "payload_too_large",
           "rate_limited", "concurrency_limited", "body_error", "invalid_json",
           "invalid_snapshot", "store_error", "out_of_order", "store_capacity"}
AGENT_FIELDS = {
    'scans_total{result="success"}': "scanSuccess",
    'scans_total{result="failure"}': "scanFailure",
    'snapshot_posts_total{result="success"}': "postSuccess",
    'snapshot_posts_total{result="failure"}': "postFailure",
    "last_scan_timestamp_seconds": "scanCompletedUnixSeconds",
    "last_scan_duration_seconds": "scanDurationNanos",
    'last_scan_containers{kind="found"}': "found",
    'last_scan_containers{kind="mapped"}': "mapped",
    'last_scan_containers{kind="unmapped"}': "unmapped",
    'last_scan_containers{kind="infrastructure"}': "infrastructure",
    "metadata_cache_pods": "metadataCachePods",
}


def project(text, prefix, fields, seconds_fields):
    require(type(text) is str, "metrics text required")
    try:
        size = len(text.encode("utf-8"))
    except UnicodeError:
        raise ValueError("invalid metrics encoding") from None
    lines = text.splitlines()
    require(size <= 1 << 20 and len(lines) <= 8192 and lines and lines[-1] == "# EOF" and lines.count("# EOF") == 1,
            "incomplete or unbounded metrics response")
    result = {}
    families = {name.partition("{")[0] for name in fields}
    for line in lines:
        if not line.startswith(prefix):
            continue  # Other families may contain identities; never export them.
        parts = line.split()
        name = parts[0][len(prefix):]
        if name.partition("{")[0] not in families:
            continue
        require(len(parts) == 2, "invalid selected metric")
        require(name in fields and fields[name] not in result, "unknown or duplicate selected metric")
        raw = parts[1]
        require(len(raw) <= 64 and NUMBER.fullmatch(raw) is not None, "invalid metric number")
        try:
            value = Decimal(raw)
        except InvalidOperation:
            raise ValueError("invalid metric number") from None
        output = fields[name]
        if output in seconds_fields:
            require(value.is_finite() and 0 <= value <= 1800, "invalid duration")
            result[output] = int((value * 1000000000).to_integral_value(rounding=ROUND_CEILING))
        else:
            require(value.is_finite() and 0 <= value <= MAX_COUNTER and value == int(value),
                    "invalid or imprecise counter")
            result[output] = int(value)
    return result


def agent_metrics(text):
    result = project(text, AGENT, AGENT_FIELDS, {"scanDurationNanos"})
    require(result.keys() == set(AGENT_FIELDS.values()), "missing agent metric")
    require(result["scanSuccess"] + result["scanFailure"] > 0
            and result["scanCompletedUnixSeconds"] > 0, "agent has no completed scan")
    require(result["scanDurationNanos"] > 0, "zero scan duration cannot establish a percentile")
    return result


def collector_metrics(text):
    fields = {'requests_total{result="' + name + '"}': name for name in RESULTS}
    fields["last_duration_seconds"] = "durationNanos"
    result = project(text, COLLECTOR, fields, {"durationNanos"})
    require("durationNanos" in result and len(result) > 1, "missing collector metric family")
    duration = result.pop("durationNanos")
    require(duration > 0, "zero ingestion duration cannot establish latency")
    # Store.RecordIngestion creates result keys on their first occurrence. Only
    # these closed, absent result series are defined as zero by that renderer.
    counts = {name: result.get(name, 0) for name in sorted(RESULTS)}
    require(0 < sum(counts.values()) <= MAX_COUNTER, "invalid total ingestion count")
    return {"durationNanos": duration, "results": counts}
