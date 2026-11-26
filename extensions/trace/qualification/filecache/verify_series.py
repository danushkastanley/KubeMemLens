"""Validate a complete fixed-schedule workload record; never infer missing operations."""
import json
from typing import NamedTuple

from verify_workload import validate_timed_observation


START_KEYS = {"type", "schemaVersion", "count", "periodNanos", "monotonicBeforeNanos",
              "wallNanos", "monotonicAfterNanos", "firstDueNanos"}
ROW_KEYS = {"sequence", "dueMonotonicNanos", "observation"}


class SeriesFormat(NamedTuple):
    record_type: str
    version: int
    maximum_count: int
    maximum_bytes: int


UNIFORM = SeriesFormat("series-start", 1, 1800, 2 * 1024 * 1024)
MIXED = SeriesFormat("mixed-series-start", 2, 18000, 8 * 1024 * 1024)
MIXED_MODES = ("cached", "uncached", "write")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate workload evidence key")
        result[key] = value
    return result


def validate_series(raw, mode, count, period_ms):
    if mode not in ("cached", "uncached", "write", "noise"):
        raise ValueError("invalid expected series mode")
    return _validate(raw, (mode,), count, period_ms, UNIFORM)


def validate_mixed_series(raw, count, period_ms):
    return _validate(raw, MIXED_MODES, count, period_ms, MIXED)


def _validate(raw, modes, count, period_ms, format):
    if (type(count) is not int or not 1 <= count <= format.maximum_count or count % len(modes)
            or type(period_ms) is not int or not 100 <= period_ms <= 10000
            or count * period_ms > 1800000):
        raise ValueError("invalid expected series schedule")
    if not isinstance(raw, str) or len(raw.encode()) > format.maximum_bytes:
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
    if (start["type"] != format.record_type or start["schemaVersion"] != format.version
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
        validate_timed_observation(observation, modes[index % len(modes)])
        if not due <= observation["operationStartedMonotonicNanos"] < observation["operationEndedMonotonicNanos"] < due + period:
            raise ValueError("series deadline missed")
    return rows
