"""Conservatively bracket resource intervals with unchanged owned-link observations."""
from bisect import bisect_left, bisect_right
from samples import exact, integer, require

OBJECT_KINDS = {"map", "prog", "link"}
# Schema 2 adds per-target coverage; legacy one-worker observations stay schema 1.
WITNESS_SCHEMA = {1: 1, 2: 2}


def clock(value):
    exact(value, {"monotonicNanos", "wallNanos", "uncertaintyNanos"})
    integer(value["monotonicNanos"], 1, 2**63 - 1)
    integer(value["wallNanos"], 1, 2**63 - 1)
    integer(value["uncertaintyNanos"], 0, 5000000)


def snapshot(value):
    exact(value, {"workers", "excludedWorkers", "activeControls", "objects",
                  "kernelMapBytes", "userMapBytes", "clock"})
    integer(value["workers"], 0, 2)
    integer(value["excludedWorkers"], 0, 32)
    integer(value["activeControls"], 0, value["workers"])
    integer(value["kernelMapBytes"])
    integer(value["userMapBytes"])
    clock(value["clock"])
    exact(value["objects"], OBJECT_KINDS)
    for values in value["objects"].values():
        require(type(values) is list and len(values) <= 512, "unbounded owned object list")
        for number in values:
            integer(number, 1, 2**32 - 1)
        require(values == sorted(set(values)), "duplicate or unordered owned objects")
    if value["workers"] == 0:
        require(not any(value["objects"].values()) and value["kernelMapBytes"] == 0
                and value["userMapBytes"] == 0, "empty worker state contains owned objects")


def validate_witness(rows, seconds, *, expected_workers=1):
    integer(seconds, 1, 1800)
    integer(expected_workers, 1, 2)
    require(type(rows) is list and len(rows) == seconds + 1, "incomplete activity witness")
    starts, ends = [], []
    for index, row in enumerate(rows):
        require(type(row) is dict, "invalid activity record")
        fields = {"schemaVersion", "index", "elapsedNanos", "readNanos", "state", "clock",
                  "observerCPUUsec", "observerPeakRSSBytes"}
        require(row.get("state") in {"observed", "unavailable"}, "invalid activity availability")
        if row["state"] == "observed":
            fields.add("snapshot")
            snapshot(row.get("snapshot"))
            if expected_workers == 2:
                fields.add('targetWorkers')
                counts = row.get('targetWorkers')
                require(type(counts) is list and len(counts) == 2, 'two-target coverage is missing')
                for count in counts:
                    integer(count, 0, 2)
                require(sum(counts) == row['snapshot']['workers'], 'target coverage differs from worker inventory')
        exact(row, fields)
        require(type(row["schemaVersion"]) is int and row["schemaVersion"] == WITNESS_SCHEMA[expected_workers] and
                type(row["index"]) is int and row["index"] == index, "activity ordering/version mismatch")
        clock(row["clock"])
        integer(row["elapsedNanos"])
        integer(row["readNanos"], 1, 100000000)
        integer(row["observerCPUUsec"])
        integer(row["observerPeakRSSBytes"], 1)
        uncertainty = row["clock"]["uncertaintyNanos"]
        starts.append(row["clock"]["wallNanos"] - row["readNanos"] - uncertainty)
        ends.append(row["clock"]["wallNanos"] + uncertainty)
        require(abs(row["elapsedNanos"] - index * 1000000000) <= 100000000, "activity sampling drift")
        require(abs(starts[index] - starts[0] - row["elapsedNanos"]) <= 105000000,
                "activity wall/monotonic clock discontinuity")
        require(abs(row["clock"]["monotonicNanos"] - rows[0]["clock"]["monotonicNanos"]
                    - row["elapsedNanos"]) <= 105000000, "activity monotonic clock discontinuity")
        if row["state"] == "observed":
            point = row["snapshot"]["clock"]
            require(starts[-1] - point["uncertaintyNanos"] <= point["wallNanos"]
                    <= ends[-1] + point["uncertaintyNanos"], "snapshot outside its read interval")
            require(row["clock"]["monotonicNanos"] - row["readNanos"] - uncertainty - point["uncertaintyNanos"]
                    <= point["monotonicNanos"] <= row["clock"]["monotonicNanos"] + uncertainty + point["uncertaintyNanos"],
                    "snapshot outside its monotonic read interval")
        if index == 0:
            require(row["elapsedNanos"] == 0, "missing initial activity observation")
            continue
        before = rows[index - 1]
        require(900000000 <= row["elapsedNanos"] - before["elapsedNanos"] <= 1100000000,
                "activity observation gap")
        require(row["observerCPUUsec"] >= before["observerCPUUsec"] and starts[-1] > ends[-2],
                "activity observer reset or overlapping reads")
    require(rows[-1]["elapsedNanos"] >= seconds * 1000000000, "short activity witness")
    return starts, ends


def active_intervals(samples, witness, seconds, expected_objects, *, expected_workers=1):
    """Call after schema-2 sample validation; a missing bracket remains unproven."""
    starts, ends = validate_witness(witness, seconds, expected_workers=expected_workers)
    require(len(samples) == seconds + 1, "resource/witness window mismatch")
    signatures = active_signatures(witness, expected_objects, expected_workers=expected_workers)
    windows = [(before["wallNanos"], after["wallNanos"] + after["readNanos"])
               for before, after in zip(samples, samples[1:])]
    return bracket_windows(starts, ends, signatures, windows)


def active_signatures(witness, expected_objects, *, expected_workers=1):
    integer(expected_workers, 1, 2)
    exact(expected_objects, OBJECT_KINDS)
    for number in expected_objects.values():
        integer(number, 1, 512)
    signatures = []
    for row in witness:
        value = row.get("snapshot")
        signature = None
        if (value is not None and value["workers"] == expected_workers and value["excludedWorkers"] == 0
                and value["activeControls"] == expected_workers and value["kernelMapBytes"] > 0
                and value["userMapBytes"] > 0
                and (expected_workers == 1 or row.get('targetWorkers') == [1, 1])
                and all(len(value["objects"][kind]) == count for kind, count in expected_objects.items())):
            signature = tuple(tuple(value["objects"][kind]) for kind in sorted(OBJECT_KINDS))
        signatures.append(signature)
    return signatures


def bracket_windows(starts, ends, signatures, windows):
    result = []
    for begin, end in windows:
        require(type(begin) is int and type(end) is int and begin < end, "invalid activity span")
        left = bisect_right(ends, begin) - 1
        right = bisect_left(starts, end)
        proven = (left >= 0 and right < len(signatures) and left < right
                  and signatures[left] is not None
                  and all(signature == signatures[left] for signature in signatures[left:right + 1]))
        result.append(proven)
    return result


def operation_activity(observations, witness, seconds, expected_objects, *, expected_workers=1):
    validate_witness(witness, seconds, expected_workers=expected_workers)
    signatures = active_signatures(witness, expected_objects, expected_workers=expected_workers)
    starts = [r["clock"]["monotonicNanos"] - r["readNanos"] - r["clock"]["uncertaintyNanos"] for r in witness]
    ends = [r["clock"]["monotonicNanos"] + r["clock"]["uncertaintyNanos"] for r in witness]
    windows = [(r["observation"]["operationStartedMonotonicNanos"], r["observation"]["operationEndedMonotonicNanos"])
               for r in observations]
    return bracket_windows(starts, ends, signatures, windows)
