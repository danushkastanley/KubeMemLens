"""Exercise the verified production CLI without retaining its private response payloads."""

import json

from candidate_manifest import MAX_BINARY
from common import require
from process import execute
from provider_plan import file_digest


def verify(execution, run=execute):
    c, binding = execution.bundle.configuration, execution.binding
    require(file_digest(c["cliBinary"], MAX_BINARY) == c["cliDigest"], "frozen CLI bytes changed")
    command = [c["cliBinary"], "--kubeconfig", c["kubeconfigPath"], "--context", c["context"],
               "--collector-namespace", c["namespace"], "--connect-mode", "kubernetes-api", "--mode", "deep"]
    execution.verify_binding()
    doctor = json.loads(run(command + ["doctor", "--strict", "--output", "json"], timeout=30, maximum=2 * 1024 * 1024,
                            failure_codes={1: "production doctor reported a warning or failure"}))
    require(doctor.get("checks") and all(check.get("status") == "pass" for check in doctor["checks"]),
            "production doctor reported a warning or failure")
    expected = {node["name"] for node in binding["nodes"]}
    require({node["nodeName"] for node in doctor.get("nodes", [])} == expected
            and all(node.get("stale") is False for node in doctor["nodes"]), "production doctor Node coverage differs")
    mapping = doctor["mapping"]
    require(mapping["containers"] >= execution.bundle.profile["workload"]["containers"]
            and mapping["mapped"] == mapping["containers"] and mapping["unmapped"] == 0, "production doctor mapping is incomplete")
    for node in binding["nodes"]:
        result = json.loads(run(command + ["explain", "node", node["name"], "--output", "json"],
                                timeout=30, maximum=2 * 1024 * 1024, failure_codes={1: "production Node explain failed"}))
        record = result["record"]
        require(record["nodeName"] == node["name"] and record["nodeUID"] == node["uid"]
                and record["freshness"] == "fresh" and record.get("lastGood", {}).get("nodeUID") == node["uid"],
                "production Node explain returned missing or different evidence")
        history = json.loads(run(command + ["history", "node", node["name"], "--output", "json"],
                                 timeout=30, maximum=2 * 1024 * 1024, failure_codes={1: "production Node history failed"}))
        require(history["nodeName"] == node["name"] and history.get("generation")
                and any(series["nodeUID"] == node["uid"] and series.get("points") for series in history.get("series", [])),
                "production Node history has no evidence for the current identity")
    execution.verify_binding()
    return {"doctor": True, "nodeExplain": len(expected), "nodeHistory": len(expected), "rawResponsesRetained": False}
