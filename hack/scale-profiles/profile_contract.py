#!/usr/bin/env python3

import hashlib
import json
import math
import re
from pathlib import Path


SUMMARY_ALLOWED_KEYS = {
    "schemaVersion", "orchestrationOutcome", "completedAt", "profile", "workload", "samples", "measurements",
    "privacy", "caveats", "disruptionOperational",
    "workloadReplacement",
}
SUMMARY_REQUIRED_KEYS = {
    "schemaVersion", "orchestrationOutcome", "profile", "workload", "samples", "privacy", "caveats",
    "disruptionOperational",
    "workloadReplacement",
}
REQUIRED_PRIVACY = {
    "clusterIdentifiersIncluded": False,
    "workloadIdentifiersIncluded": False,
    "rawMetricsIncluded": False,
    "rawLogsIncluded": False,
}
FORBIDDEN_KEYS = {
    "context", "cluster", "namespace", "nodename", "podname", "poduid", "containerid", "token", "kubeconfig",
    "rawlogs", "rawmetrics",
}
MAX_CAVEATS = 8
MAX_CAVEAT_LENGTH = 240


class EvaluationInputError(ValueError):
    pass


def canonical_digest(profile):
    content = dict(profile)
    content.pop("profileDigest", None)
    encoded = json.dumps(content, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()
    return "sha256:" + hashlib.sha256(encoded).hexdigest()


def load_json(path):
    try:
        with Path(path).open(encoding="utf-8") as source:
            return json.load(source)
    except (OSError, json.JSONDecodeError) as error:
        raise EvaluationInputError(f"read {path}: {error}") from error


def nonnegative_number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) and value >= 0


def number_list(value, name, minimum=1):
    if not isinstance(value, list) or len(value) < minimum:
        raise EvaluationInputError(f"{name} must contain at least {minimum} values")
    if any(not nonnegative_number(item) for item in value):
        raise EvaluationInputError(f"{name} must contain non-negative numbers")
    return value


def interval_milliseconds(value):
    match = re.fullmatch(r"([1-9][0-9]*)(ms|s)", value) if isinstance(value, str) else None
    if match is None:
        raise EvaluationInputError("profile workload.agentInterval must use positive ms or s units")
    multiplier = 1 if match.group(2) == "ms" else 1000
    return int(match.group(1)) * multiplier


def validate_profile(profile):
    required = {"schemaVersion", "id", "mode", "telemetryRequired", "workload", "evidence", "budgets", "profileDigest"}
    if not isinstance(profile, dict) or set(profile) != required:
        raise EvaluationInputError("profile fields do not match schema version 1")
    if profile["schemaVersion"] != 1 or profile["mode"] not in ("development", "qualification"):
        raise EvaluationInputError("profile schemaVersion or mode is invalid")
    if not isinstance(profile["id"], str) or not profile["id"]:
        raise EvaluationInputError("profile id is required")
    if not isinstance(profile["telemetryRequired"], bool):
        raise EvaluationInputError("profile telemetryRequired must be boolean")
    if profile["mode"] == "qualification" and not profile["telemetryRequired"]:
        raise EvaluationInputError("qualification profiles require telemetry")
    if profile["profileDigest"] != canonical_digest(profile):
        raise EvaluationInputError("profileDigest does not match canonical profile content")
    workload = profile["workload"]
    workload_keys = {
        "containers", "containersPerPod", "creationBatchPods", "steadyStateSeconds", "sampleIntervalSeconds",
        "agentInterval", "image", "canaryMiB",
    }
    if not isinstance(workload, dict) or set(workload) != workload_keys:
        raise EvaluationInputError("profile workload fields do not match schema version 1")
    for key in (
        "containers", "containersPerPod", "creationBatchPods", "steadyStateSeconds", "sampleIntervalSeconds", "canaryMiB",
    ):
        if isinstance(workload[key], bool) or not isinstance(workload[key], int) or workload[key] <= 0:
            raise EvaluationInputError(f"profile workload.{key} must be a positive integer")
    if not 1 <= workload["containersPerPod"] <= 50:
        raise EvaluationInputError("profile workload.containersPerPod must be between 1 and 50")
    if not 5 <= workload["sampleIntervalSeconds"] <= 300:
        raise EvaluationInputError("profile workload.sampleIntervalSeconds must be between 5 and 300")
    image = workload["image"]
    if not isinstance(image, str) or re.fullmatch(r"[^\s@]+@sha256:[a-f0-9]{64}", image) is None:
        raise EvaluationInputError("profile workload.image must be digest-pinned")
    if workload["containers"] % workload["containersPerPod"] != 0:
        raise EvaluationInputError("profile container count must divide exactly by containersPerPod")
    pod_count = workload["containers"] // workload["containersPerPod"]
    if workload["creationBatchPods"] > pod_count:
        raise EvaluationInputError("profile workload.creationBatchPods cannot exceed the workload Pod count")
    if workload["steadyStateSeconds"] % workload["sampleIntervalSeconds"] != 0:
        raise EvaluationInputError("profile steady-state duration must divide exactly by the sample interval")
    if profile["mode"] == "qualification" and (
        workload["containers"] < 5000 or workload["steadyStateSeconds"] < 1800
    ):
        raise EvaluationInputError("qualification profiles require at least 5,000 containers for 1,800 seconds")
    evidence = profile["evidence"]
    if not isinstance(evidence, dict) or set(evidence) != {"minimumSamples", "canaryControlSamples"}:
        raise EvaluationInputError("profile evidence fields do not match schema version 1")
    for key in ("minimumSamples", "canaryControlSamples"):
        if isinstance(evidence[key], bool) or not isinstance(evidence[key], int) or evidence[key] <= 0:
            raise EvaluationInputError(f"profile evidence.{key} must be a positive integer")
    expected_minimum = workload["steadyStateSeconds"] // workload["sampleIntervalSeconds"] + 1
    if evidence["minimumSamples"] < expected_minimum:
        raise EvaluationInputError("profile evidence.minimumSamples does not cover the steady-state window")
    budget_names = {
        "agentScanP99MillisecondsExclusive", "cliP95Milliseconds", "tuiP95Milliseconds", "recoverySeconds",
        "memoryLimitRatio", "cpuThrottlingRatioExclusive", "agentPostFailureDelta", "agentScanFailureDelta",
        "apiErrorDelta", "apiRateLimitedDelta", "nodeMemoryPressureNodes", "canaryRegressionPercent",
    }
    budgets = profile["budgets"]
    if not isinstance(budgets, dict) or set(budgets) != budget_names:
        raise EvaluationInputError("profile budgets do not match schema version 1")
    if any(not nonnegative_number(value) for value in budgets.values()):
        raise EvaluationInputError("profile budgets must be non-negative numbers")
    if budgets["agentScanP99MillisecondsExclusive"] != interval_milliseconds(workload["agentInterval"]) * 0.8:
        raise EvaluationInputError("agent scan budget must equal 80 percent of the declared agent interval")


def reject_forbidden_keys(value):
    if isinstance(value, dict):
        for key, child in value.items():
            if not isinstance(key, str):
                raise EvaluationInputError("raw summary object keys must be strings")
            if key.casefold() in FORBIDDEN_KEYS:
                raise EvaluationInputError(f"raw summary contains forbidden key: {key}")
            reject_forbidden_keys(child)
    elif isinstance(value, list):
        for child in value:
            reject_forbidden_keys(child)


def validate_summary(summary):
    if not isinstance(summary, dict):
        raise EvaluationInputError("raw summary must be a JSON object")
    keys = set(summary)
    if not SUMMARY_REQUIRED_KEYS.issubset(keys) or not keys.issubset(SUMMARY_ALLOWED_KEYS):
        raise EvaluationInputError("raw summary top-level fields do not match schema version 1")
    privacy = summary["privacy"]
    valid_privacy = isinstance(privacy, dict) and set(privacy) == set(REQUIRED_PRIVACY)
    valid_privacy = valid_privacy and all(privacy[key] is False for key in REQUIRED_PRIVACY)
    if not valid_privacy:
        raise EvaluationInputError("raw summary privacy declaration is missing or invalid")
    caveats = summary["caveats"]
    valid_caveats = isinstance(caveats, list) and len(caveats) <= MAX_CAVEATS
    valid_caveats = valid_caveats and all(
        isinstance(item, str) and 0 < len(item) <= MAX_CAVEAT_LENGTH for item in caveats
    )
    if not valid_caveats:
        raise EvaluationInputError("raw summary caveats must be bounded non-empty strings")
    reject_forbidden_keys(summary)
