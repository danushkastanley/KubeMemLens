#!/usr/bin/env python3

import argparse
import json
import math
import sys
from pathlib import Path

from profile_contract import (
    EvaluationInputError,
    MAX_CAVEAT_LENGTH,
    canonical_digest,
    load_json,
    nonnegative_number,
    number_list,
    validate_profile,
    validate_summary,
)


REQUIRED_RECOVERIES = ("workload", "agent", "collector", "node", "api", "partialRollout")
REQUIRED_COMPONENTS = ("agent", "collector")
def nearest_rank(values, percentile, minimum=1):
    ordered = sorted(number_list(values, "percentile values", minimum))
    rank = max(1, math.ceil((percentile / 100) * len(ordered)))
    return ordered[rank - 1]


def check(name, passed, budget, observed, failures, failure):
    status = "pass" if passed else "fail"
    if not passed:
        failures.append(failure)
    return {"name": name, "status": status, "budget": budget, "observed": observed}


def unavailable_check(name, budget, required, failures):
    if required:
        failures.append(f"{name}: required telemetry is missing")
        return {"name": name, "status": "fail", "budget": budget, "observed": None}
    return {"name": name, "status": "not_evaluated", "budget": budget, "observed": None}


def evaluate_core(profile, summary, failures):
    checks = []
    outcome = summary.get("orchestrationOutcome")
    completed = outcome in ("completed", "passed")
    checks.append(check("orchestration", completed, {"accepted": ["completed", "passed"]}, outcome, failures,
                        "orchestration: qualification did not reach terminal evaluation"))
    declared = summary.get("profile")
    identity = summary.get("schemaVersion") == 1 and isinstance(declared, dict)
    identity = identity and declared.get("id") == profile["id"] and declared.get("digest") == profile["profileDigest"]
    checks.append(check("profile_identity", identity, {"id": profile["id"], "digest": profile["profileDigest"]}, declared, failures,
                        "profile_identity: raw summary does not match the selected profile"))

    observed_workload = summary.get("workload")
    workload_ok = isinstance(observed_workload, dict) and observed_workload == profile["workload"]
    checks.append(check("workload", workload_ok, profile["workload"], observed_workload, failures,
                        "workload: observed workload does not exactly match the profile"))

    samples = summary.get("samples")
    steady_samples = [sample for sample in samples if isinstance(sample, dict) and sample.get("phase") == "steady"] \
        if isinstance(samples, list) else []
    minimum = profile["evidence"]["minimumSamples"]
    sample_count = len(steady_samples)
    checks.append(check("sample_count", sample_count >= minimum, {"minimum": minimum}, {"count": sample_count}, failures,
                        "sample_count: too few steady-state samples"))

    elapsed = [sample.get("elapsedSeconds") for sample in steady_samples]
    duration = profile["workload"]["steadyStateSeconds"]
    interval = profile["workload"]["sampleIntervalSeconds"]
    ordered = bool(elapsed) and all(nonnegative_number(value) for value in elapsed)
    ordered = ordered and elapsed == sorted(elapsed)
    window_ok = ordered and elapsed[0] <= interval and elapsed[-1] - elapsed[0] >= duration
    window_observed = {"firstSeconds": elapsed[0], "lastSeconds": elapsed[-1]} if ordered else None
    checks.append(check("steady_state_window", window_ok, {"minimumSeconds": duration}, window_observed, failures,
                        "steady_state_window: samples do not cover the declared steady-state duration"))

    target = profile["workload"]["containers"]
    evidence_ok = isinstance(samples, list) and bool(samples)
    operational_ok = bool(steady_samples)
    if evidence_ok:
        for sample in samples:
            if not isinstance(sample, dict):
                evidence_ok = False
                operational_ok = False
                continue
            mapping = sample.get("mapping", {})
            reliability = sample.get("reliability", {})
            operational = sample.get("operational", {})
            evidence_ok = evidence_ok and sample.get("workloadContainers") == target
            evidence_ok = evidence_ok and mapping == {"expected": target, "mapped": target, "unmapped": 0}
            evidence_ok = evidence_ok and reliability.get("state") == "ready"
            evidence_ok = evidence_ok and reliability.get("expectedNodes", 0) > 0
            evidence_ok = evidence_ok and reliability.get("freshNodes") == reliability.get("expectedNodes")
            evidence_ok = evidence_ok and reliability.get("staleNodes") == 0 and reliability.get("missingNodes") == 0
            if sample.get("phase") == "steady":
                operational_ok = operational_ok and operational == {"unexplainedRestarts": 0, "oomKills": 0}
    checks.append(check("mapping_and_node_coverage", evidence_ok, {"containers": target, "state": "ready", "coveragePercent": 100},
                        {"samplesChecked": len(samples) if isinstance(samples, list) else 0}, failures,
                        "mapping_and_node_coverage: a sample is incomplete or not ready"))
    checks.append(check("operational_stability", operational_ok, {"unexplainedRestarts": 0, "oomKills": 0},
                        {"samplesChecked": sample_count}, failures, "operational_stability: a sample reports a restart or OOM kill"))
    disruption = summary.get("disruptionOperational")
    disruption_ok = disruption == {"unexplainedRestarts": 0, "oomKills": 0}
    checks.append(check("disruption_stability", disruption_ok, {"unexplainedRestarts": 0, "oomKills": 0},
                        disruption, failures, "disruption_stability: an unexpected restart or OOM kill occurred"))
    return checks


def percentile_check(name, values, percentile, budget, exclusive, required, minimum, failures):
    if values is None:
        return unavailable_check(name, budget, required, failures)
    try:
        observed = nearest_rank(values, percentile, minimum)
    except EvaluationInputError:
        return unavailable_check(name, budget, required, failures)
    passed = observed < budget if exclusive else observed <= budget
    comparator = "below" if exclusive else "at or below"
    return check(name, passed, {"percentile": percentile, "maximum": budget, "exclusive": exclusive}, observed, failures,
                 f"{name}: observed percentile is not {comparator} budget")


def component_checks(measurements, profile, minimum, failures):
    budgets = profile["budgets"]
    required = profile["telemetryRequired"]
    components = measurements.get("components") if isinstance(measurements, dict) else None
    memory_observed = {}
    throttle_observed = {}
    valid_memory = isinstance(components, dict)
    valid_throttle = valid_memory
    if valid_memory:
        for name in REQUIRED_COMPONENTS:
            component = components.get(name)
            memory = component.get("memoryLimitRatios") if isinstance(component, dict) else None
            throttling = component.get("cpuThrottlingRatios") if isinstance(component, dict) else None
            try:
                memory_observed[name] = nearest_rank(memory, 95, minimum)
            except EvaluationInputError:
                valid_memory = False
            try:
                throttle_observed[name] = nearest_rank(throttling, 95, minimum)
            except EvaluationInputError:
                valid_throttle = False

    if valid_memory and set(memory_observed) == set(REQUIRED_COMPONENTS):
        memory = check("component_memory_p95", all(value <= budgets["memoryLimitRatio"] for value in memory_observed.values()),
                       {"maximumLimitRatio": budgets["memoryLimitRatio"]}, memory_observed, failures,
                       "component_memory_p95: a component exceeds the memory budget")
    else:
        memory = unavailable_check("component_memory_p95", {"maximumLimitRatio": budgets["memoryLimitRatio"]}, required, failures)
    if valid_throttle and set(throttle_observed) == set(REQUIRED_COMPONENTS):
        throttle = check("cpu_throttling_p95", all(value < budgets["cpuThrottlingRatioExclusive"] for value in throttle_observed.values()),
                         {"maximumRatio": budgets["cpuThrottlingRatioExclusive"], "exclusive": True}, throttle_observed, failures,
                         "cpu_throttling_p95: a component is not below the throttling budget")
    else:
        throttle = unavailable_check("cpu_throttling_p95", {"maximumRatio": budgets["cpuThrottlingRatioExclusive"], "exclusive": True}, required, failures)
    return [memory, throttle]


def recovery_check(measurements, profile, failures):
    budget = profile["budgets"]["recoverySeconds"]
    required = profile["telemetryRequired"]
    recoveries = measurements.get("recoverySeconds") if isinstance(measurements, dict) else None
    valid = isinstance(recoveries, dict) and set(recoveries) == set(REQUIRED_RECOVERIES)
    valid = valid and all(nonnegative_number(value) for value in recoveries.values())
    if not valid:
        return unavailable_check("recovery", {"maximumSeconds": budget, "events": list(REQUIRED_RECOVERIES)}, required, failures)
    return check("recovery", all(value <= budget for value in recoveries.values()),
                 {"maximumSeconds": budget, "events": list(REQUIRED_RECOVERIES)}, recoveries, failures,
                 "recovery: an event exceeded the recovery budget")


def api_check(measurements, profile, failures):
    budgets = profile["budgets"]
    required = profile["telemetryRequired"]
    observed = measurements.get("apiServer") if isinstance(measurements, dict) else None
    valid = isinstance(observed, dict) and set(observed) == {"errorDelta", "rateLimitedDelta"}
    valid = valid and all(nonnegative_number(value) for value in observed.values())
    budget = {"errorDelta": budgets["apiErrorDelta"], "rateLimitedDelta": budgets["apiRateLimitedDelta"]}
    if not valid:
        return unavailable_check("api_server", budget, required, failures)
    return check("api_server", observed == budget, budget, observed, failures,
                 "api_server: errors or rate limiting increased")


def counter_check(name, values, budget, required, minimum, failures):
    label = f"measurements.{name}"
    try:
        counters = number_list(values, label, minimum)
    except EvaluationInputError:
        return unavailable_check(name, {"maximumDelta": budget, "minimumSamples": minimum}, required, failures)
    monotonic = counters == sorted(counters)
    observed = counters[-1] - counters[0] if monotonic else None
    passed = monotonic and observed <= budget
    return check(name, passed, {"maximumDelta": budget, "minimumSamples": minimum}, observed, failures,
                 f"{name}: counter reset or failures increased")


def agent_failure_checks(measurements, profile, minimum, failures):
    required = profile["telemetryRequired"]
    post = measurements.get("agentPostFailures") if isinstance(measurements, dict) else None
    scans = measurements.get("agentScanFailures") if isinstance(measurements, dict) else None
    return [
        counter_check("agent_post_failures", post, profile["budgets"]["agentPostFailureDelta"], required, minimum, failures),
        counter_check("agent_scan_failures", scans, profile["budgets"]["agentScanFailureDelta"], required, minimum, failures),
    ]


def node_memory_pressure_check(measurements, profile, minimum, failures):
    budget = profile["budgets"]["nodeMemoryPressureNodes"]
    required = profile["telemetryRequired"]
    observed = measurements.get("nodeMemoryPressureNodes") if isinstance(measurements, dict) else None
    try:
        values = number_list(observed, "measurements.nodeMemoryPressureNodes", minimum)
    except EvaluationInputError:
        return unavailable_check("node_memory_pressure", {"maximumNodes": budget}, required, failures)
    return check("node_memory_pressure", all(value == 0 for value in values),
                 {"maximumNodes": budget}, values, failures,
                 "node_memory_pressure: one or more samples report memory pressure")


def canary_check(measurements, profile, minimum, failures):
    budget = profile["budgets"]["canaryRegressionPercent"]
    required = profile["telemetryRequired"]
    canary = measurements.get("canary") if isinstance(measurements, dict) else None
    if not isinstance(canary, dict) or canary.get("higherIsBetter") is not False:
        return unavailable_check("canary_regression", {"maximumPercent": budget}, required, failures)
    try:
        control = nearest_rank(canary.get("control"), 50, profile["evidence"]["canaryControlSamples"])
        observed = nearest_rank(canary.get("observed"), 50, minimum)
    except EvaluationInputError:
        return unavailable_check("canary_regression", {"maximumPercent": budget}, required, failures)
    if control <= 0 or observed <= 0:
        return unavailable_check("canary_regression", {"maximumPercent": budget}, required, failures)
    regression = ((observed - control) / control) * 100
    regression = max(0.0, regression)
    result = {"controlMedian": control, "observedMedian": observed, "regressionPercent": regression,
              "higherIsBetter": canary["higherIsBetter"]}
    return check("canary_regression", regression <= budget, {"maximumPercent": budget}, result, failures,
                 "canary_regression: representative workload regression exceeds budget")


def evaluate(profile, summary):
    validate_summary(summary)
    failures = []
    checks = evaluate_core(profile, summary, failures)
    measurements = summary.get("measurements")
    budgets = profile["budgets"]
    required = profile["telemetryRequired"]
    minimum = profile["evidence"]["minimumSamples"]
    checks.extend([
        percentile_check("agent_scan_p99", measurements.get("agentScanMilliseconds") if isinstance(measurements, dict) else None,
                         99, budgets["agentScanP99MillisecondsExclusive"], True, required, minimum, failures),
        percentile_check("cli_latency_p95", measurements.get("cliLatencyMilliseconds") if isinstance(measurements, dict) else None,
                         95, budgets["cliP95Milliseconds"], False, required, minimum, failures),
        percentile_check("tui_latency_p95", measurements.get("tuiLatencyMilliseconds") if isinstance(measurements, dict) else None,
                         95, budgets["tuiP95Milliseconds"], False, required, minimum, failures),
        recovery_check(measurements, profile, failures),
    ])
    checks.extend(component_checks(measurements, profile, minimum, failures))
    checks.extend(agent_failure_checks(measurements, profile, minimum, failures))
    checks.extend([api_check(measurements, profile, failures),
                   node_memory_pressure_check(measurements, profile, minimum, failures),
                   canary_check(measurements, profile, minimum, failures)])
    return {
        "schemaVersion": 1,
        "profile": {"id": profile["id"], "digest": profile["profileDigest"], "mode": profile["mode"]},
        "result": "pass" if not failures else "fail",
        "checks": checks,
        "failures": failures,
    }


def write_report(report, output):
    content = json.dumps(report, ensure_ascii=False, indent=2, sort_keys=False) + "\n"
    if output:
        Path(output).write_text(content, encoding="utf-8")
    else:
        sys.stdout.write(content)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Evaluate a KubeMemLens scale result against a versioned profile.")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--summary")
    parser.add_argument("--output")
    parser.add_argument("--validate-profile", action="store_true")
    args = parser.parse_args(argv)
    try:
        profile = load_json(args.profile)
        validate_profile(profile)
        if args.validate_profile:
            if args.summary or args.output:
                raise EvaluationInputError("profile validation does not accept summary or output")
            return 0
        if not args.summary:
            raise EvaluationInputError("--summary is required for evaluation")
        summary = load_json(args.summary)
        report = evaluate(profile, summary)
        write_report(report, args.output)
        return 0 if report["result"] == "pass" else 1
    except EvaluationInputError as error:
        print(f"scale evaluation input error: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
