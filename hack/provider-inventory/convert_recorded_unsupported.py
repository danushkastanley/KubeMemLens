#!/usr/bin/env python3

import argparse
import copy
import hashlib
import json
import os
import re
import subprocess
import sys
from datetime import timedelta
from pathlib import Path


ROOT = Path(__file__).resolve().parent
PROFILES = ROOT.parent / "provider-profiles"
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(PROFILES))

from collect import ReceiptError, current_tool_commit, validate_receipt  # noqa: E402
from observe_unsupported import (  # noqa: E402
    OBSERVATION_CHECK_KEYS,
    PROVIDER_CHECK_KEYS,
    SPECS,
    canonical_digest,
    validate_unsupported_receipt,
)
from privacy_contract import reject_sensitive_content  # noqa: E402
from profile_contract import (  # noqa: E402
    EvaluationInputError,
    parse_timestamp,
    validate_pending_evidence,
    validate_profile,
)


COMMIT_PATTERN = re.compile(r"[a-f0-9]{40}")
HEX_DIGEST_PATTERN = re.compile(r"[a-f0-9]{64}")
INCOMPATIBILITY_KEYS = {
    "schemaVersion", "ticket", "provider", "observedAt", "releaseCandidate", "environment",
    "providerAuthenticationConfiguration", "candidateObservation", "classification",
}
RELEASE_KEYS = {
    "sourceCommit", "imageDigest", "chartDigest", "extensionServerSourceSha256",
}
ENVIRONMENT_KEYS = {
    "kubernetesVersion", "nodeImage", "osImage", "runtime", "architecture", "cniName",
    "linuxNodeCount", "windowsNodeCount",
}
AUTHENTICATION_KEYS = {
    "requestHeaderClientCAValid", "requestHeaderAllowedNames", "providerOwnedConfigMap",
}
CANDIDATE_KEYS = {
    "collectorLive", "collectorReady", "requestHeaderReadinessFailed", "helmInstallReachedTimeout",
    "cleanupVerified", "failedPendingSha256", "failedSummarySha256",
}
CLASSIFICATION_KEYS = {"reasonCode", "result", "securityDecision"}
SUMMARY_KEYS = {"schemaVersion", "outcome", "completedAt", "image", "checks", "measurements", "caveats"}
SOURCE_PATH = "internal/extension/server.go"
MAX_CONVERSION_DELAY = timedelta(hours=1)


def require_exact_keys(value, keys, name):
    if not isinstance(value, dict) or set(value) != keys:
        raise ReceiptError(f"{name} fields do not match the recorded-live conversion schema")


def digest_bytes(content):
    return hashlib.sha256(content).hexdigest()


def digest_file(path):
    return digest_bytes(Path(path).read_bytes())


def source_at_commit(source_commit, path=SOURCE_PATH):
    try:
        result = subprocess.run(
            ["git", "show", f"{source_commit}:{path}"], check=False, capture_output=True,
            timeout=30,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ReceiptError("candidate source could not be read") from error
    if result.returncode != 0:
        raise ReceiptError("candidate source could not be read")
    return result.stdout


def validate_record_shape(record):
    require_exact_keys(record, INCOMPATIBILITY_KEYS, "incompatibility record")
    if type(record["schemaVersion"]) is not int or record["schemaVersion"] != 1 \
            or not isinstance(record["ticket"], str) \
            or not re.fullmatch(r"PROD-[0-9]{3}", record["ticket"]):
        raise ReceiptError("incompatibility record identity is invalid")
    require_exact_keys(record["releaseCandidate"], RELEASE_KEYS, "release candidate")
    require_exact_keys(record["environment"], ENVIRONMENT_KEYS, "recorded environment")
    require_exact_keys(
        record["providerAuthenticationConfiguration"], AUTHENTICATION_KEYS,
        "provider authentication configuration",
    )
    require_exact_keys(record["candidateObservation"], CANDIDATE_KEYS, "candidate observation")
    require_exact_keys(record["classification"], CLASSIFICATION_KEYS, "classification")
    release = record["releaseCandidate"]
    if COMMIT_PATTERN.fullmatch(release["sourceCommit"]) is None \
            or HEX_DIGEST_PATTERN.fullmatch(release["extensionServerSourceSha256"]) is None:
        raise ReceiptError("incompatibility record source binding is invalid")
    if any(not isinstance(release[name], str) or re.fullmatch(r"sha256:[a-f0-9]{64}", release[name]) is None
           for name in ("imageDigest", "chartDigest")):
        raise ReceiptError("incompatibility record artefact binding is invalid")
    observation = record["candidateObservation"]
    if any(HEX_DIGEST_PATTERN.fullmatch(observation[name] or "") is None
           for name in ("failedPendingSha256", "failedSummarySha256")):
        raise ReceiptError("incompatibility record input digests are invalid")


def validate_conversion_facts(profile, record, failed, legacy_receipt, summary,
                              failed_digest, summary_digest, source_content):
    validate_profile(profile)
    validate_record_shape(record)
    validate_pending_evidence(failed)
    validate_receipt(legacy_receipt)
    require_exact_keys(summary, SUMMARY_KEYS, "qualification summary")
    if type(summary["schemaVersion"]) is not int or summary["schemaVersion"] != 1:
        raise ReceiptError("qualification summary schemaVersion is invalid")
    for value in (record, failed, legacy_receipt, summary):
        reject_sensitive_content(value, ReceiptError)
    spec = SPECS.get(profile["id"])
    classification = record["classification"]
    if profile["expectedOutcome"] != "unsupported" or spec is None \
            or classification["reasonCode"] != profile["expectedUnsupportedReason"] \
            or classification["reasonCode"] != spec["reasonCode"]:
        raise ReceiptError("recorded classification does not match the canonical unsupported profile")
    if classification != {
        "reasonCode": spec["reasonCode"],
        "result": "unsupported-for-current-candidate",
        "securityDecision": "do-not-weaken-request-header-client-name-validation",
    }:
        raise ReceiptError("recorded classification is not the approved security decision")
    authentication = record["providerAuthenticationConfiguration"]
    if authentication != {
        "requestHeaderClientCAValid": True,
        "requestHeaderAllowedNames": [],
        "providerOwnedConfigMap": True,
    }:
        raise ReceiptError("recorded provider authentication state does not prove the incompatibility")
    observation = record["candidateObservation"]
    expected_observation = {
        "collectorLive": True,
        "collectorReady": False,
        "requestHeaderReadinessFailed": True,
        "helmInstallReachedTimeout": True,
        "cleanupVerified": True,
    }
    if any(observation[name] != value for name, value in expected_observation.items()):
        raise ReceiptError("recorded candidate observation does not prove the incompatibility")
    if observation["failedPendingSha256"] != failed_digest \
            or observation["failedSummarySha256"] != summary_digest:
        raise ReceiptError("recorded conversion input digest does not match")
    if failed["profile"] != legacy_receipt["profile"] or failed["profile"]["id"] != profile["id"]:
        raise ReceiptError("failed evidence and provider receipt profile identity differs")
    if failed["artefacts"]["providerReceiptDigest"] != legacy_receipt["receiptDigest"]:
        raise ReceiptError("failed evidence is not bound to the provider receipt")
    release = record["releaseCandidate"]
    for name in ("sourceCommit", "imageDigest", "chartDigest"):
        if failed["artefacts"][name] != release[name]:
            raise ReceiptError(f"failed evidence {name} differs from the recorded candidate")
    recorded_environment = record["environment"]
    if any(failed["environment"][name] != value for name, value in recorded_environment.items()):
        raise ReceiptError("failed evidence environment differs from the incompatibility record")
    if legacy_receipt["provider"] != record["provider"] \
            or legacy_receipt["nodeImage"] != failed["environment"]["nodeImage"] \
            or legacy_receipt["cniName"] != failed["environment"]["cniName"]:
        raise ReceiptError("provider receipt differs from the incompatibility environment")
    if failed["outcome"] != "failed" or failed["reasonCode"] != "helm_install_failed" \
            or failed["checks"]["helmInstall"] != "fail":
        raise ReceiptError("recorded pending result is not the failed candidate installation")
    if summary["outcome"] != "failed" or summary["completedAt"] != failed["completedAt"] \
            or summary["image"] != {"repository": "redacted", "digest": release["imageDigest"]}:
        raise ReceiptError("qualification summary differs from the failed candidate result")
    observed = parse_timestamp(record["observedAt"])
    completed = parse_timestamp(failed["completedAt"])
    provider_observed = parse_timestamp(legacy_receipt["observedAt"])
    if provider_observed > completed or completed > observed or observed - completed > MAX_CONVERSION_DELAY:
        raise ReceiptError("recorded live facts do not form one bounded qualification attempt")
    if digest_bytes(source_content) != release["extensionServerSourceSha256"]:
        raise ReceiptError("candidate source hash does not match the incompatibility record")


def convert_record(profile, record, failed, legacy_receipt, summary, *, failed_digest,
                   summary_digest, qualification_tool_commit, source_content):
    validate_conversion_facts(
        profile, record, failed, legacy_receipt, summary, failed_digest, summary_digest,
        source_content,
    )
    if COMMIT_PATTERN.fullmatch(qualification_tool_commit or "") is None:
        raise ReceiptError("qualification tool commit is invalid")
    spec = SPECS[profile["id"]]
    release = record["releaseCandidate"]
    environment = copy.deepcopy(failed["environment"])
    subject_count = environment["linuxNodeCount"] + environment["windowsNodeCount"]
    receipt = {
        "schemaVersion": 3,
        "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
        "observedAt": record["observedAt"],
        "qualificationToolCommit": qualification_tool_commit,
        "environment": environment,
        "controlPlaneVersion": legacy_receipt["controlPlaneVersion"],
        "artefacts": {"sourceCommit": release["sourceCommit"], "chartDigest": release["chartDigest"]},
        "proof": {
            "source": spec["source"],
            "sourceDigest": canonical_digest(record, "unused"),
            "observationSpecDigest": canonical_digest(spec, "unused"),
        },
        "providerChecks": dict.fromkeys(PROVIDER_CHECK_KEYS, True),
        "unsupportedObservation": {
            "reasonCode": spec["reasonCode"], "method": spec["method"], "state": spec["state"],
            "subjectCount": subject_count, "checks": dict.fromkeys(OBSERVATION_CHECK_KEYS, True),
        },
    }
    receipt["receiptDigest"] = canonical_digest(receipt, "receiptDigest")
    validate_unsupported_receipt(profile, receipt)
    return receipt


def main():
    parser = argparse.ArgumentParser(
        description="Convert bounded recorded-live qualification facts into an unsupported receipt.",
    )
    parser.add_argument("--profile", required=True)
    parser.add_argument("--incompatibility-record", required=True)
    parser.add_argument("--failed-pending", required=True)
    parser.add_argument("--provider-receipt", required=True)
    parser.add_argument("--failed-summary", required=True)
    parser.add_argument("--output", required=True)
    arguments = parser.parse_args()
    output = Path(arguments.output)
    if output.name != "provider-inventory.json" or output.exists() or output.is_symlink():
        raise SystemExit("recorded-live conversion requires a new provider-inventory.json output")
    try:
        profile_path = Path(arguments.profile).resolve()
        profile = json.loads(profile_path.read_text(encoding="utf-8"))
        if profile_path != (PROFILES / f"{profile.get('id')}.json").resolve():
            raise ReceiptError("profile must be the canonical provider profile")
        record_path = Path(arguments.incompatibility_record)
        failed_path = Path(arguments.failed_pending)
        legacy_path = Path(arguments.provider_receipt)
        summary_path = Path(arguments.failed_summary)
        record = json.loads(record_path.read_text(encoding="utf-8"))
        failed = json.loads(failed_path.read_text(encoding="utf-8"))
        legacy = json.loads(legacy_path.read_text(encoding="utf-8"))
        summary = json.loads(summary_path.read_text(encoding="utf-8"))
        commit = current_tool_commit(require_clean=True)
        receipt = convert_record(
            profile, record, failed, legacy, summary,
            failed_digest=digest_file(failed_path), summary_digest=digest_file(summary_path),
            qualification_tool_commit=commit,
            source_content=source_at_commit(record["releaseCandidate"]["sourceCommit"]),
        )
    except (OSError, json.JSONDecodeError, EvaluationInputError, ReceiptError) as error:
        raise SystemExit(f"recorded-live conversion error: {error}") from error
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as error:
        raise SystemExit("refusing to overwrite the recorded-live conversion output") from error
    with os.fdopen(descriptor, "w", encoding="utf-8") as destination:
        destination.write(json.dumps(receipt, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
