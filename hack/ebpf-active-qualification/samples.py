"""Validate schema-2 observations without manufacturing missing process data."""
from pathlib import Path
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "ebpf-qualification"))
from idle_evaluate import counter_map, exact, integer, require, strict_json, validate_node

ROLES = {"node", "api", "selected", "agent", "collector", "probe"} | {
    f"nonselected-{index}" for index in range(10)}
SCHEDULING_COUNTERS = {"runtimeNanos", "runqueueWaitNanos", "timeslices"}


def read_samples(path):
    path = Path(path)
    require(path.stat().st_size <= 256 << 20, "sample stream exceeds byte bound")
    rows = []
    with path.open() as stream:
        for line in stream:
            require(len(line.encode()) <= 256 << 10 and len(rows) < 1801,
                    "sample stream exceeds record/count bound")
            rows.append(strict_json(line))
    return rows


def scheduling(value, scope):
    require(type(value) is dict, "missing scheduling state")
    source = "/proc/schedstat" if scope == "node" else "/proc/pid/task/tid/schedstat"
    require(value.get("source") == source, "scheduling source changed")
    state = value.get("state")
    if state != "observed":
        allowed = {"unsupported", "disabled", "unavailable"} if scope == "node" else {"unavailable"}
        require(state in allowed, "invalid scheduling availability")
        fields = {"state", "source"}
        if state == "disabled":
            fields.add("enabled")
            require(value.get("enabled") is False, "disabled source lacks runtime flag")
        exact(value, fields)
        return
    fields = {"state", "source"} | SCHEDULING_COUNTERS
    for key in SCHEDULING_COUNTERS:
        integer(value.get(key))
    if scope == "node":
        fields |= {"enabled", "version", "cpus"}
        require(value.get("enabled") is True, "node scheduling not enabled")
        integer(value.get("version"), 15, 17)
        integer(value.get("cpus"), 1, 4096)
    else:
        fields.add("cohort")
        require(type(value.get("cohort")) is str and
                re.fullmatch(r"[0-9a-f]{64}", value["cohort"]) is not None,
                "invalid private task cohort token")
    exact(value, fields)


def process(value):
    require(type(value) is dict, "missing process observation")
    if value.get("state") == "unavailable":
        exact(value, {"state"})
        return
    require(value.get("state") == "observed", "invalid process availability")
    integer(value.get("rssBytes"), 1)
    integer(value.get("count"), 1, 64)
    scheduling(value.get("scheduling"), "task")
    fields = {"state", "rssBytes", "count", "scheduling"}
    if value["scheduling"]["state"] == "observed":
        fields.add("tasks")
        integer(value.get("tasks"), 1, 64 * 256)
    exact(value, fields)


def group(value):
    exact(value, {"cpu", "memoryCurrent", "memory", "memoryEvents", "process", "pids"})
    counter_map(value["cpu"], {"usage_usec", "user_usec", "system_usec",
                               "nr_periods", "nr_throttled", "throttled_usec"})
    counter_map(value["memory"], {"anon", "file", "shmem", "inactive_file"})
    counter_map(value["memoryEvents"], {"oom", "oom_kill", "max", "high"})
    integer(value["memoryCurrent"], 1)
    integer(value["pids"], 1)
    process(value["process"])


def monotonic_map(before, after):
    require(before.keys() == after.keys() and all(after[k] >= v for k, v in before.items()),
            "cumulative counter reset or inventory change")


def validate_window(rows, seconds, roles):
    integer(seconds, 1, 1800)
    require(type(roles) is set and roles <= ROLES, "invalid bound role set")
    require(type(rows) is list and len(rows) == seconds + 1, "incomplete observation window")
    for index, row in enumerate(rows):
        exact(row, {"schemaVersion", "index", "elapsedNanos", "wallNanos", "readNanos", "groups", "node"})
        require(type(row["schemaVersion"]) is int and row["schemaVersion"] == 2 and
                type(row["index"]) is int and row["index"] == index, "sample ordering/version mismatch")
        integer(row["elapsedNanos"])
        integer(row["wallNanos"], 1)
        integer(row["readNanos"], 1, 100000000)
        require(abs(row["wallNanos"] - rows[0]["wallNanos"] - row["elapsedNanos"]) <= 100000000,
                "wall/monotonic clock discontinuity")
        require(abs(row["elapsedNanos"] - index * 1000000000) <= 100000000, "sampling drift")
        exact(row["groups"], roles)
        require(type(row["node"]) is dict and "scheduling" in row["node"], "missing node scheduling state")
        scheduling(row["node"]["scheduling"], "node")
        validate_node({k: v for k, v in row["node"].items() if k != "scheduling"})
        for value in row["groups"].values():
            group(value)
        if index == 0:
            require(row["elapsedNanos"] == 0, "missing initial sample")
            continue
        before = rows[index - 1]
        step = row["elapsedNanos"] - before["elapsedNanos"]
        require(900000000 <= step <= 1100000000, "sample interval outside bound")
        require(row["node"]["observerCPUUsec"] >= before["node"]["observerCPUUsec"], "observer counter reset")
        for kind in ("cpu", "memory", "io"):
            monotonic_map(before["node"]["pressure"][kind], row["node"]["pressure"][kind])
        for role in roles:
            a, b = before["groups"][role], row["groups"][role]
            for field in ("cpu", "memoryEvents"):
                monotonic_map(a[field], b[field])
            require(all(a["memoryEvents"][key] == b["memoryEvents"][key] for key in ("oom", "oom_kill")),
                    "OOM during resource observation")
    require(rows[-1]["elapsedNanos"] >= seconds * 1000000000, "short observation window")


def scheduling_deltas(rows, role=None):
    """Only comparable observed cohorts produce a delta; all other intervals persist."""
    results = []
    for index, (a, b) in enumerate(zip(rows, rows[1:])):
        if role is None:
            before, after = a["node"]["scheduling"], b["node"]["scheduling"]
            same = all(before.get(key) == after.get(key) for key in ("cpus", "version"))
        else:
            before = a["groups"][role]["process"].get("scheduling", {"state": "unavailable"})
            after = b["groups"][role]["process"].get("scheduling", {"state": "unavailable"})
            same = before.get("cohort") == after.get("cohort")
        result = {"interval": index, "beforeState": before["state"], "afterState": after["state"]}
        if before["state"] != "observed" or after["state"] != "observed":
            result["state"] = "unavailable"
        elif not same:
            result["state"] = "cohort-changed"
        elif any(after[key] < before[key] for key in SCHEDULING_COUNTERS):
            result["state"] = "counter-reset"
        else:
            result["state"] = "observed"
            result.update({key: after[key] - before[key] for key in sorted(SCHEDULING_COUNTERS)})
        results.append(result)
    return results
