#!/usr/bin/env python3
"""Evaluate measurements without converting them into reviewed support claims."""

import argparse
import json
import re
import sys

from common import ContractError, digest, instant, load, write_new
from evidence import EVENTS, measurement_sets, validate_evidence
from measurement_checks import cpu_mean, measurement_checks  # cpu_mean remains available to existing callers


def evaluate(profile, evidence):
    p, e = profile, validate_evidence(profile, evidence)
    checks = []

    def check(name, passed, observed=None, limit=None):
        checks.append({"id": name, "passed": bool(passed), "observed": observed, "limit": limit})

    m, b = p["measurement"], p["budgets"]
    observed_nodes = measurement_sets(e)
    check("orchestration", e["orchestration"] == "completed")
    environment = e["environment"]
    check("runtime-prerequisites", environment["cgroupVersion"] == "v2" and environment["architecture"] in {"amd64", "arm64"} and re.fullmatch(r"v?1\.(36|37)\.\d+(?:[-+][A-Za-z0-9.-]+)?", environment["kubernetes"]) is not None)
    unobserved = {"", "unknown", "unreported", "unavailable", "unsupported", "none", "n/a", "provider-receipt"}
    check("environment-observed", all(environment[k].strip().lower() not in unobserved
                                     for k in ("kernel", "runtime", "nodeImage", "osImage", "cni")))
    duration = (instant(e["completedAt"]) - instant(e["startedAt"])).total_seconds()
    observed_duration = max(sum(rows[-1]["elapsedSeconds"] if rows else 0 for rows in node["samples"].values())
                            for node in observed_nodes)
    check("elapsed-window", duration >= max(observed_duration, m["baselineSeconds"] + m["enabledSeconds"]) + 2 * m["settleSeconds"], duration)
    measurements = []
    for node in observed_nodes:
        replicas = 1 if e["schemaVersion"] == 2 else p["workload"]["linuxNodes"]
        measured = measurement_checks(p, node["samples"], node["rotation"], replicas)
        measurements.append(measured)
        if e["schemaVersion"] == 1:
            checks.extend(measured["topology"])
    transport = e["transport"]
    check("direct-transport", transport["result"] == "passed" and all(transport[k] for k in ("directTLS", "podBoundIdentity", "statsOnlyRBAC")) and not transport["proxyAccess"])
    if p["profileClass"] == "provider":
        check("network-policy", transport["networkPolicy"] == "passed")
        check("per-node-measurements", e["schemaVersion"] == 2)
    for measured in measurements:
        if e["schemaVersion"] == 1:
            checks.extend(measured["cost"])
    for name in sorted(EVENTS):
        event = e["lifecycle"][name]
        if name == "providerNodeReplacement" and p["profileClass"] == "local-kind":
            check(name, event["state"] == "not-applicable")
            continue
        passed = event["state"] == "passed" and event["identityVerified"] and event["freshEvidence"] and event["elapsedSeconds"] is not None and event["elapsedSeconds"] <= b["recoverySeconds"]
        if name == "sourceLoss":
            passed = passed and event["staleRetained"]
        check(name, passed, event["elapsedSeconds"], b["recoverySeconds"])
    cleanup = e["cleanup"]
    cloud = "not-applicable" if p["profileClass"] == "local-kind" else "confirmed"
    check("cleanup", cleanup["workloadsRemoved"] and cleanup["rbacRemoved"] and cleanup["cloudResources"] == cloud)
    passed = all(c["passed"] for c in checks) and all(c["passed"] for m in measurements for group in m.values() for c in group)
    result = {"schemaVersion": e["schemaVersion"], "profile": dict(e["profile"]), "profileClass": p["profileClass"], "recordDigest": e["recordDigest"], "outcome": "pass" if passed else "fail", "qualified": False, "reviewState": "pending", "sourceDirty": e["artefacts"]["sourceDirty"], "unreportedFields": sorted(k for k, v in e["fields"].items() if v == "unreported"), "provenance": e["provenance"], "checks": checks}
    if e["schemaVersion"] == 2:
        result["observation"] = dict(e["observation"])
        result["nodeChecks"] = [{"slot": slot, "checks": measured["topology"] + measured["cost"]}
                                for slot, measured in enumerate(measurements)]
    result["evaluationDigest"] = digest(result, "evaluationDigest")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", required=True)
    parser.add_argument("--evidence", required=True)
    parser.add_argument("--output")
    args = parser.parse_args()
    try:
        result = evaluate(load(args.profile), load(args.evidence))
        if args.output:
            write_new(args.output, result)
        else:
            print(json.dumps(result, indent=2))
        return 0 if result["outcome"] == "pass" else 1
    except (ContractError, OSError) as error:
        print(f"Node qualification evaluation: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
