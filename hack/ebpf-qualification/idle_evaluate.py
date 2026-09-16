"""Fail-closed validation and calculation for complete, paired idle observations."""

import json
import math
from pathlib import Path
import re

EXPECTED_PROFILE = {
    "schemaVersion": 1, "id": "local-idle-v1", "repetitions": 5,
    "windowSeconds": 900, "warmupSeconds": 60,
    "sampleIntervalNanos": 1_000_000_000, "intervalToleranceNanos": 100_000_000,
    "maximumReadNanos": 100_000_000, "idleMeanCPUMilli": 5,
    "idlePeakWorkingSetBytes": 40 * 1024 * 1024,
    "percentileMethod": "nearest-rank", "cpuDenominator": "one-core",
    "workingSetMethod": "current-minus-inactive-file-clamped-zero",
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def strict_json(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate evidence key")
            result[key] = value
        return result

    def constant(_value):
        raise ValueError("non-finite evidence value")

    return json.loads(data, object_pairs_hook=unique, parse_constant=constant)


def exact(value, fields):
    require(type(value) is dict and set(value) == set(fields), "unexpected evidence fields")


def integer(value, minimum=0, maximum=2**64 - 1):
    require(type(value) is int and minimum <= value <= maximum, "invalid integer observation")
    return value


def counter_map(value, required):
    require(type(value) is dict and set(required) <= value.keys() and len(value) <= 128,
            "missing or unbounded counter map")
    for key, number in value.items():
        require(re.fullmatch(r"[a-z][a-z0-9_]{0,47}", key) is not None, "invalid counter name")
        integer(number)


def percentile(values, percent):
    require(bool(values), "missing percentile observations")
    return sorted(values)[math.ceil(len(values) * percent / 100) - 1]


def load_profile(path):
    value = strict_json(Path(path).read_text())
    require(value == EXPECTED_PROFILE, "measurement protocol or budgets changed")
    return value


def read_samples(path):
    require(Path(path).stat().st_size <= 8 * 1024 * 1024, "evidence file exceeds bound")
    with Path(path).open() as stream:
        rows = []
        for line in stream:
            require(len(line) <= 16384 and len(rows) <= 900, "sample count/size exceeds bound")
            rows.append(strict_json(line))
    return rows


def validate_node(node):
    exact(node, {"cpuTicks", "memoryAvailable", "pressure", "observerCPUUsec", "observerPeakRSSBytes"})
    require(type(node["cpuTicks"]) is list and len(node["cpuTicks"]) == 10, "missing node CPU")
    for v in node["cpuTicks"]:
        integer(v)
    integer(node["memoryAvailable"])
    integer(node["observerCPUUsec"])
    integer(node["observerPeakRSSBytes"], 1)
    exact(node["pressure"], {"cpu", "memory", "io"})
    for kind, counters in node["pressure"].items():
        require(set(counters) <= {"some", "full"}, "unknown pressure metric")
        counter_map(counters, {"some"} if kind == "cpu" else {"some", "full"})


def validate_group(group):
    exact(group, {"cpu", "memoryCurrent", "memory", "memoryEvents", "rssBytes", "processes", "pids"})
    counter_map(group["cpu"], {"usage_usec", "user_usec", "system_usec", "nr_periods", "nr_throttled", "throttled_usec"})
    counter_map(group["memory"], {"anon", "file", "shmem", "inactive_file"})
    counter_map(group["memoryEvents"], {"oom", "oom_kill", "max", "high"})
    integer(group["memoryCurrent"], 1)
    integer(group["rssBytes"], 1)
    integer(group["processes"], 1, 64)
    integer(group["pids"], 1)


def validate_window(rows, enabled):
    require(len(rows) == 901, "incomplete 15-minute window")
    roles = {"node", "api"} if enabled else set()
    for index, row in enumerate(rows):
        exact(row, {"schemaVersion", "index", "elapsedNanos", "wallNanos", "readNanos", "groups", "node"})
        require(type(row["schemaVersion"]) is int and row["schemaVersion"] == 1 and
                type(row["index"]) is int and row["index"] == index, "sample ordering/version mismatch")
        integer(row["elapsedNanos"])
        integer(row["wallNanos"], 1)
        require(abs(row["wallNanos"] - rows[0]["wallNanos"] - row["elapsedNanos"]) <= 100_000_000,
                "wall/monotonic clock discontinuity")
        integer(row["readNanos"], 1, EXPECTED_PROFILE["maximumReadNanos"])
        exact(row["groups"], roles)
        validate_node(row["node"])
        for group in row["groups"].values():
            validate_group(group)
            require(group["processes"] == 1, "idle service contains a worker or unexpected process")
        if index == 0:
            require(row["elapsedNanos"] == 0, "missing starting sample")
            continue
        previous = rows[index - 1]
        require(row["node"]["observerCPUUsec"] >= previous["node"]["observerCPUUsec"], "observer counter reset")
        for kind in ("cpu", "memory", "io"):
            before, after = previous["node"]["pressure"][kind], row["node"]["pressure"][kind]
            require(set(before) == set(after) and all(after[k] >= v for k, v in before.items()), "pressure counter reset")
        step = row["elapsedNanos"] - previous["elapsedNanos"]
        require(900_000_000 <= step <= 1_100_000_000, "sampling cadence changed or gap observed")
        require(abs(row["elapsedNanos"] - index * 1_000_000_000) <= 100_000_000, "sampling drift")
        for role in roles:
            before, after = previous["groups"][role], row["groups"][role]
            for field in ("cpu", "memoryEvents"):
                require(set(before[field]) == set(after[field]), "counter set changed")
                require(all(after[field][k] >= v for k, v in before[field].items()), "counter reset")
            require(before["memoryEvents"]["oom"] == after["memoryEvents"]["oom"] and
                    before["memoryEvents"]["oom_kill"] == after["memoryEvents"]["oom_kill"], "OOM during observation")
    require(rows[-1]["elapsedNanos"] >= 900_000_000_000, "window shorter than 15 minutes")


def role_summary(rows, role):
    def usage(row):
        return row["groups"][role]["cpu"]["usage_usec"]
    rates = [(usage(b) - usage(a)) * 1_000_000 / (b["elapsedNanos"] - a["elapsedNanos"])
             for a, b in zip(rows, rows[1:])]
    working_sets = [max(0, r["groups"][role]["memoryCurrent"] - r["groups"][role]["memory"]["inactive_file"])
                    for r in rows]
    return {
        "meanCPUMilli": (usage(rows[-1]) - usage(rows[0])) * 1_000_000 / rows[-1]["elapsedNanos"],
        "p95CPUMilli": percentile(rates, 95), "p99CPUMilli": percentile(rates, 99),
        "peakWorkingSetBytes": max(working_sets),
        "peakRSSBytes": max(r["groups"][role]["rssBytes"] for r in rows),
    }


def evaluate_pair(control, enabled):
    validate_window(control, False)
    validate_window(enabled, True)
    node, api = (role_summary(enabled, role) for role in ("node", "api"))
    installation_cpu = node["meanCPUMilli"] + api["meanCPUMilli"]
    installation_memory = max(sum(max(0, r["groups"][role]["memoryCurrent"] -
                                          r["groups"][role]["memory"]["inactive_file"])
                                  for role in ("node", "api")) for r in enabled)
    node_passed = (node["meanCPUMilli"] <= EXPECTED_PROFILE["idleMeanCPUMilli"] and
                   node["peakWorkingSetBytes"] <= EXPECTED_PROFILE["idlePeakWorkingSetBytes"])
    passed = (node_passed and installation_cpu <= EXPECTED_PROFILE["idleMeanCPUMilli"] and
              installation_memory <= EXPECTED_PROFILE["idlePeakWorkingSetBytes"])
    return {"idleBudgetPassed": passed, "nodeBudgetPassed": node_passed, "node": node, "api": api,
            "installationMeanCPUMilli": installation_cpu, "installationPeakWorkingSetBytes": installation_memory,
            "qualification": "idle-pair-only"}
