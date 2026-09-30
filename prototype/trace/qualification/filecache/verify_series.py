"""Validate a complete fixed-schedule workload record; never infer missing operations."""
import json

from verify_workload import validate_timed_observation


START_KEYS = {"type", "schemaVersion", "count", "periodNanos", "monotonicBeforeNanos",
              "wallNanos", "monotonicAfterNanos", "firstDueNanos"}
ROW_KEYS = {"sequence", "dueMonotonicNanos", "observation"}


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate workload evidence key")
        result[key] = value
    return result


def validate_series(raw, mode, count, period_ms):
    if (mode not in ("cached", "uncached", "write", "noise")
            or type(count) is not int or not 1 <= count <= 1800
            or type(period_ms) is not int or not 100 <= period_ms <= 10000
            or count * period_ms > 1800000):
        raise ValueError("invalid expected series schedule")
    if not isinstance(raw, str) or len(raw.encode()) > 2 * 1024 * 1024:
        raise ValueError("invalid series output size")
    lines = raw.splitlines()
    if len(lines) != count + 1 or not raw.endswith("\n"):
        raise ValueError("incomplete series output")
    rows = [json.loads(line, object_pairs_hook=unique_object) for line in lines]
    start = rows[0]
    if not isinstance(start, dict) or set(start) != START_KEYS:
        raise ValueError("invalid series clock record")
    if any(type(start[key]) is not int or not 0 < start[key] < 2**64
           for key in START_KEYS - {"type"}):
        raise ValueError("invalid series clock values")
    period = period_ms * 1000000
    if (start["type"] != "series-start" or start["schemaVersion"] != 1
            or start["count"] != count or start["periodNanos"] != period
            or not 0 <= start["monotonicAfterNanos"] - start["monotonicBeforeNanos"] <= 1000000
            or start["firstDueNanos"] != start["monotonicAfterNanos"] + 5000000000):
        raise ValueError("series schedule or clock alignment mismatch")
    for index, row in enumerate(rows[1:]):
        if not isinstance(row, dict) or set(row) != ROW_KEYS:
            raise ValueError("invalid series observation record")
        due = start["firstDueNanos"] + index * period
        if (type(row["sequence"]) is not int or row["sequence"] != index
                or type(row["dueMonotonicNanos"]) is not int or row["dueMonotonicNanos"] != due
                or not isinstance(row["observation"], dict)):
            raise ValueError("missing, duplicate or rescheduled operation")
        observation = row["observation"]
        validate_timed_observation(observation, mode)
        if not due <= observation["operationStartedMonotonicNanos"] < observation["operationEndedMonotonicNanos"] < due + period:
            raise ValueError("series deadline missed")
    return rows
