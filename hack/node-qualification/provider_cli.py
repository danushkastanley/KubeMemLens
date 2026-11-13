"""Exercise the verified production CLI without retaining its private response payloads."""

import json
import re

from candidate_manifest import MAX_BINARY
from common import ContractError, require
from process import execute
from provider_plan import file_digest


CHECK_NAME = re.compile(r"[A-Za-z][A-Za-z ]{0,39}")
CHECK_STATUSES = ("warn", "fail")


def doctor_report(text):
    try:
        report = json.loads(text)
    except ValueError as error:
        raise ContractError("production doctor returned no report") from error
    require(isinstance(report, dict) and isinstance(report.get("checks"), list) and report["checks"]
            and all(isinstance(check, dict) for check in report["checks"]), "production doctor returned no report")
    return report


def check_label(check):
    """Name and status only: check messages can carry private Node and workload names."""
    name, status = check.get("name"), check.get("status")
    name = name if isinstance(name, str) and CHECK_NAME.fullmatch(name) else "unnamed"
    return name + "=" + (status if status in CHECK_STATUSES else "unknown")


def verify(execution, run=execute):
    c, binding = execution.bundle.configuration, execution.binding
    require(file_digest(c["cliBinary"], MAX_BINARY) == c["cliDigest"], "frozen CLI bytes changed")
    command = [c["cliBinary"], "--kubeconfig", c["kubeconfigPath"], "--context", c["context"],
               "--collector-namespace", c["namespace"], "--connect-mode", "kubernetes-api", "--mode", "deep"]
    execution.verify_binding()
    # Strict doctor exits 1 after printing its report, so read the report to say which checks did not pass.
    doctor = doctor_report(run(command + ["doctor", "--strict", "--output", "json"], timeout=30,
                               maximum=2 * 1024 * 1024, tolerated_codes=(1,)))
    failed = [check for check in doctor["checks"] if check.get("status") != "pass"]
    require(not failed, "production doctor reported a warning or failure: " + ", ".join(map(check_label, failed)))
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
