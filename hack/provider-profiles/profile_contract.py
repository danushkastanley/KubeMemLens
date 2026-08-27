#!/usr/bin/env python3

import hashlib
import json
import re
from datetime import date, datetime, timedelta, timezone
from pathlib import Path

from privacy_contract import reject_sensitive_content


PROFILE_KEYS = {
    "schemaVersion",
    "id",
    "expectedOutcome",
    "expectations",
    "expectedUnsupportedReason",
    "requalificationDays",
    "profileDigest",
}
EXPECTATION_KEYS = {
    "providerPattern",
    "osImagePattern",
    "runtimePattern",
    "architecturePattern",
    "cgroupPattern",
    "nodeImagePattern",
    "cniPattern",
    "cniEnforcementRequired",
}
PATTERN_FIELDS = (
    "providerPattern", "osImagePattern", "runtimePattern", "architecturePattern", "cgroupPattern",
    "nodeImagePattern", "cniPattern",
)
EVIDENCE_KEYS = {
    "schemaVersion",
    "outcome",
    "completedAt",
    "reviewedAt",
    "reviewDueAt",
    "profile",
    "artefacts",
    "environment",
    "checks",
    "reasonCode",
    "privacy",
}
PENDING_EVIDENCE_KEYS = EVIDENCE_KEYS - {"reviewedAt", "reviewDueAt"}
ENVIRONMENT_KEYS = {
    "provider",
    "osImage",
    "runtime",
    "architecture",
    "cgroupVersion",
    "cniEnforced",
    "kubernetesVersion",
    "nodeImage",
    "kernelVersion",
    "kubeletVersion",
    "cniName",
    "linuxNodeCount",
    "windowsNodeCount",
}
CHECK_IDS = (
    "prerequisites",
    "helmInstall",
    "readiness",
    "collection",
    "tui",
    "api",
    "upgrade",
    "uninstall",
    "nodeReplacement",
    "agentRestart",
    "collectorRestart",
    "mounts",
    "securityContext",
    "networkPolicy",
    "mixedOSScheduling",
)
PRIVACY_FIELDS = (
    "clusterIdentifiersIncluded",
    "workloadIdentifiersIncluded",
    "providerResourceIdentifiersIncluded",
    "rawErrorsIncluded",
    "rawLogsIncluded",
)
UNSUPPORTED_REASON_CODES = {
    "hostpath_not_permitted",
    "daemonset_not_supported",
    "virtual_nodes_not_supported",
    "windows_nodes_not_supported",
    "cgroup_v1_not_supported",
    "requestheader_proxy_identity_unavailable",
}
FAILURE_REASON_CHECKS = {
    "prerequisite_failed": "prerequisites",
    "helm_install_failed": "helmInstall",
    "readiness_failed": "readiness",
    "collection_failed": "collection",
    "tui_failed": "tui",
    "api_failed": "api",
    "upgrade_failed": "upgrade",
    "uninstall_failed": "uninstall",
    "node_replacement_failed": "nodeReplacement",
    "agent_restart_failed": "agentRestart",
    "collector_restart_failed": "collectorRestart",
    "mount_verification_failed": "mounts",
    "security_context_failed": "securityContext",
    "network_policy_failed": "networkPolicy",
    "mixed_os_scheduling_failed": "mixedOSScheduling",
}
SHA256_PATTERN = re.compile(r"sha256:[a-f0-9]{64}")
COMMIT_PATTERN = re.compile(r"[a-f0-9]{40}")
SEMVER_PATTERN = re.compile(
    r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-(?P<prerelease>[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)
IDENTIFIER_PATTERN = re.compile(r"[a-z][a-z0-9]*(?:-[a-z0-9]+)*")
DATE_PATTERN = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}")
TIMESTAMP_PATTERN = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z")
MAX_REVIEW_DELAY_DAYS = 7


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


def require_exact_keys(value, expected, name):
    if not isinstance(value, dict) or set(value) != set(expected):
        raise EvaluationInputError(f"{name} fields do not match schema version 1")


def require_digest(value, name):
    if not isinstance(value, str) or SHA256_PATTERN.fullmatch(value) is None:
        raise EvaluationInputError(f"{name} must be an exact lowercase sha256 digest")


def require_pattern(value, name):
    valid = isinstance(value, str) and 2 < len(value) <= 200
    valid = valid and value.startswith("^") and value.endswith("$")
    if not valid:
        raise EvaluationInputError(f"profile expectations.{name} must be a bounded anchored regular expression")
    try:
        re.compile(value)
    except re.error as error:
        raise EvaluationInputError(f"profile expectations.{name} is invalid: {error}") from error


def parse_date(value, name):
    if not isinstance(value, str) or DATE_PATTERN.fullmatch(value) is None:
        raise EvaluationInputError(f"{name} must use YYYY-MM-DD")
    try:
        return date.fromisoformat(value)
    except ValueError as error:
        raise EvaluationInputError(f"{name} is not a valid date") from error


def parse_timestamp(value):
    if not isinstance(value, str) or TIMESTAMP_PATTERN.fullmatch(value) is None:
        raise EvaluationInputError("evidence completedAt must use UTC second precision")
    try:
        return datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ")
    except ValueError as error:
        raise EvaluationInputError("evidence completedAt is not a valid timestamp") from error


def validate_profile(profile):
    require_exact_keys(profile, PROFILE_KEYS, "profile")
    if type(profile["schemaVersion"]) is not int or profile["schemaVersion"] != 1:
        raise EvaluationInputError("profile schemaVersion must be 1")
    if not isinstance(profile["id"], str) or IDENTIFIER_PATTERN.fullmatch(profile["id"]) is None:
        raise EvaluationInputError("profile id must use lower-case kebab-case")
    if profile["expectedOutcome"] not in {"pass", "unsupported"}:
        raise EvaluationInputError("profile expectedOutcome must be pass or unsupported")
    expectations = profile["expectations"]
    require_exact_keys(expectations, EXPECTATION_KEYS, "profile expectations")
    for name in PATTERN_FIELDS:
        require_pattern(expectations[name], name)
    if expectations["cniEnforcementRequired"] is not True:
        raise EvaluationInputError("provider profiles require CNI enforcement")
    reason = profile["expectedUnsupportedReason"]
    if profile["expectedOutcome"] == "pass" and reason is not None:
        raise EvaluationInputError("supported profiles cannot declare an unsupported reason")
    if profile["expectedOutcome"] == "unsupported" and reason not in UNSUPPORTED_REASON_CODES:
        raise EvaluationInputError("unsupported profiles require a stable unsupported reason")
    days = profile["requalificationDays"]
    if isinstance(days, bool) or not isinstance(days, int) or not 30 <= days <= 365:
        raise EvaluationInputError("profile requalificationDays must be between 30 and 365")
    require_digest(profile["profileDigest"], "profile profileDigest")
    if profile["profileDigest"] != canonical_digest(profile):
        raise EvaluationInputError("profileDigest does not match canonical profile content")


def validate_evidence(evidence, pending=False):
    if not isinstance(evidence, dict):
        raise EvaluationInputError("evidence must be a JSON object")
    reject_sensitive_content(evidence, EvaluationInputError)
    expected_keys = PENDING_EVIDENCE_KEYS if pending else EVIDENCE_KEYS
    require_exact_keys(evidence, expected_keys, "pending evidence" if pending else "evidence")
    if type(evidence["schemaVersion"]) is not int or evidence["schemaVersion"] != 1:
        raise EvaluationInputError("evidence schemaVersion must be 1")
    if evidence["outcome"] not in {"passed", "unsupported_confirmed", "failed"}:
        raise EvaluationInputError("evidence outcome is invalid")
    parse_timestamp(evidence["completedAt"])
    if not pending:
        parse_date(evidence["reviewedAt"], "evidence reviewedAt")
        parse_date(evidence["reviewDueAt"], "evidence reviewDueAt")

    identity = evidence["profile"]
    require_exact_keys(identity, {"id", "digest"}, "evidence profile")
    if not isinstance(identity["id"], str) or IDENTIFIER_PATTERN.fullmatch(identity["id"]) is None:
        raise EvaluationInputError("evidence profile id is invalid")
    require_digest(identity["digest"], "evidence profile digest")

    artefacts = evidence["artefacts"]
    require_exact_keys(
        artefacts,
        {"imageDigest", "chartDigest", "valuesDigest", "providerReceiptDigest",
         "evidenceManifestDigest", "probeImageDigest", "sourceCommit", "chartVersion"},
        "evidence artefacts",
    )
    require_digest(artefacts["imageDigest"], "evidence image digest")
    require_digest(artefacts["chartDigest"], "evidence chart digest")
    require_digest(artefacts["valuesDigest"], "evidence values digest")
    require_digest(artefacts["providerReceiptDigest"], "evidence provider receipt digest")
    manifest_digest = artefacts["evidenceManifestDigest"]
    if manifest_digest is not None:
        require_digest(manifest_digest, "evidence manifest digest")
    probe_digest = artefacts["probeImageDigest"]
    if probe_digest is not None:
        require_digest(probe_digest, "evidence probe image digest")
    if not isinstance(artefacts["sourceCommit"], str) or COMMIT_PATTERN.fullmatch(artefacts["sourceCommit"]) is None:
        raise EvaluationInputError("evidence source commit must be 40 lower-case hexadecimal characters")
    version_match = SEMVER_PATTERN.fullmatch(artefacts["chartVersion"]) \
        if isinstance(artefacts["chartVersion"], str) else None
    if version_match is None:
        raise EvaluationInputError("evidence chart version must be SemVer")
    prerelease = version_match.group("prerelease")
    if prerelease and any(part.isdigit() and len(part) > 1 and part.startswith("0") for part in prerelease.split(".")):
        raise EvaluationInputError("evidence chart version must be SemVer")

    environment = evidence["environment"]
    require_exact_keys(environment, ENVIRONMENT_KEYS, "evidence environment")
    text_fields = (
        "provider", "osImage", "runtime", "architecture", "cgroupVersion",
        "kubernetesVersion", "nodeImage", "kernelVersion", "cniName",
        "kubeletVersion",
    )
    for name in text_fields:
        value = environment[name]
        if not isinstance(value, str) or not value or len(value) > 160:
            raise EvaluationInputError(f"evidence environment.{name} must be a bounded non-empty string")
    if not isinstance(environment["cniEnforced"], bool):
        raise EvaluationInputError("evidence environment.cniEnforced must be boolean")
    for name in ("linuxNodeCount", "windowsNodeCount"):
        value = environment[name]
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise EvaluationInputError(f"evidence environment.{name} must be a non-negative integer")
    if environment["linuxNodeCount"] + environment["windowsNodeCount"] == 0:
        raise EvaluationInputError("evidence environment must include at least one Node")

    checks = evidence["checks"]
    require_exact_keys(checks, CHECK_IDS, "evidence checks")
    if any(status not in {"pass", "fail", "unsupported", "not_run"} for status in checks.values()):
        raise EvaluationInputError("evidence check statuses are invalid")

    privacy = evidence["privacy"]
    require_exact_keys(privacy, PRIVACY_FIELDS, "evidence privacy")
    if any(privacy[name] is not False for name in PRIVACY_FIELDS):
        raise EvaluationInputError("evidence privacy declarations must all be false")

    outcome = evidence["outcome"]
    reason = evidence["reasonCode"]
    if outcome == "passed":
        required_checks = {name for name in CHECK_IDS if name != "mixedOSScheduling"}
        if reason is not None or any(checks[name] != "pass" for name in required_checks):
            raise EvaluationInputError("passed evidence requires every applicable check to pass and no reason code")
        expected_mixed_os = "pass" if environment["windowsNodeCount"] > 0 else "not_run"
        if checks["mixedOSScheduling"] != expected_mixed_os:
            raise EvaluationInputError(
                "passed evidence must run mixed-OS scheduling exactly when Windows Nodes are present"
            )
    elif outcome == "unsupported_confirmed":
        expected_checks = dict.fromkeys(CHECK_IDS, "not_run")
        expected_checks["prerequisites"] = "unsupported"
        if reason not in UNSUPPORTED_REASON_CODES or checks != expected_checks:
            raise EvaluationInputError("unsupported evidence requires a stable reason and only the prerequisite check")
    else:
        failed_check = FAILURE_REASON_CHECKS.get(reason)
        if failed_check is None:
            raise EvaluationInputError("failed evidence requires a classified failure reason")
        if checks[failed_check] != "fail":
            raise EvaluationInputError("failed evidence reason does not match its failed check")


def validate_pending_evidence(evidence):
    validate_evidence(evidence, pending=True)


def environment_failures(profile, environment):
    expectations = profile["expectations"]
    mappings = {
        "provider": "providerPattern",
        "osImage": "osImagePattern",
        "runtime": "runtimePattern",
        "architecture": "architecturePattern",
        "cgroupVersion": "cgroupPattern",
        "nodeImage": "nodeImagePattern",
        "cniName": "cniPattern",
    }
    failures = []
    for field, pattern_name in mappings.items():
        if re.fullmatch(expectations[pattern_name], environment[field]) is None:
            failures.append(f"environment.{field}: value does not match the selected profile")
    if profile["expectedOutcome"] == "pass" and expectations["cniEnforcementRequired"] \
            and not environment["cniEnforced"]:
        failures.append("environment.cniEnforced: enforcing CNI evidence is required")
    return failures


def evaluate(profile, evidence, as_of=None):
    validate_profile(profile)
    validate_evidence(evidence)
    as_of = as_of or datetime.now(timezone.utc).date()
    if type(as_of) is not date:
        raise EvaluationInputError("evaluation date must be a date")
    if evidence["profile"] != {"id": profile["id"], "digest": profile["profileDigest"]}:
        raise EvaluationInputError("evidence profile identity does not match the selected profile")

    reviewed = parse_date(evidence["reviewedAt"], "evidence reviewedAt")
    due = parse_date(evidence["reviewDueAt"], "evidence reviewDueAt")
    completed = parse_timestamp(evidence["completedAt"]).date()
    if reviewed < completed:
        raise EvaluationInputError("evidence review cannot predate completion")
    if reviewed > as_of:
        raise EvaluationInputError("evidence review cannot be dated in the future")
    if reviewed > completed + timedelta(days=MAX_REVIEW_DELAY_DAYS):
        raise EvaluationInputError("evidence review is more than seven days after completion")
    if due != completed + timedelta(days=profile["requalificationDays"]):
        raise EvaluationInputError("evidence reviewDueAt does not match the profile requalification period")
    freshness = "stale" if as_of > due else "current"
    warnings = []
    if freshness == "stale":
        warnings.append(f"evidence review freshness is stale after {evidence['reviewDueAt']}")

    outcome = evidence["outcome"]
    expected = profile["expectedOutcome"]
    if expected == "unsupported" and outcome == "passed":
        raise EvaluationInputError("unsupported profile cannot report a successful installation")
    if expected == "pass" and outcome == "unsupported_confirmed":
        raise EvaluationInputError("supported profile cannot report unsupported confirmation")
    if expected == "pass" and outcome == "passed" \
            and evidence["artefacts"]["evidenceManifestDigest"] is None:
        raise EvaluationInputError("passing supported evidence requires a complete evidence manifest")
    if expected == "pass" and outcome == "passed" \
            and evidence["artefacts"]["probeImageDigest"] is None:
        raise EvaluationInputError("passing supported evidence requires an approved probe image digest")
    if outcome == "unsupported_confirmed" and evidence["reasonCode"] != profile["expectedUnsupportedReason"]:
        raise EvaluationInputError("unsupported evidence reason does not match the selected profile")

    failures = environment_failures(profile, evidence["environment"])
    if outcome == "failed":
        failures.append(f"qualification.{evidence['reasonCode']}: qualification failed")
    if expected == "pass" and outcome != "passed":
        failures.append("qualification.outcome: supported profile did not pass")
    if expected == "unsupported" and outcome != "unsupported_confirmed":
        failures.append("qualification.outcome: unsupported profile was not confirmed")
    return {
        "schemaVersion": 1,
        "result": "pass" if not failures else "fail",
        "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
        "outcome": outcome,
        "freshness": freshness,
        "warnings": warnings,
        "failures": failures,
    }
