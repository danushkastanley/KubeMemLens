#!/usr/bin/env python3

import argparse
import copy
import json
import os
import sys
from datetime import date, datetime, timedelta, timezone
from pathlib import Path

INVENTORY_ROOT = Path(__file__).resolve().parents[1] / "provider-inventory"
sys.path.insert(0, str(INVENTORY_ROOT))

from collect import ReceiptError, validate_receipt  # noqa: E402
from observe_unsupported import validate_unsupported_receipt  # noqa: E402

from evidence_manifest import (  # noqa: E402
    MANIFEST_NAME,
    FINAL_NAME,
    ManifestError,
    PENDING_NAME,
    require_exact_bundle_entries,
)
from bundle_semantics import validate_supported_bundle  # noqa: E402

from profile_contract import (
    EvaluationInputError,
    MAX_REVIEW_DELAY_DAYS,
    evaluate,
    load_json,
    parse_timestamp,
    validate_pending_evidence,
    validate_profile,
)


ACKNOWLEDGEMENT = "reviewed-provider-evidence"
MAX_OBSERVATION_AGE = timedelta(hours=1)


def parse_args():
    parser = argparse.ArgumentParser(description="Review and finalise pending provider evidence.")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--input", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--provider-receipt", required=True)
    parser.add_argument("--evidence-manifest")
    parser.add_argument("--acknowledge", required=True)
    return parser.parse_args()


def normalise_kubernetes_version(value):
    return value[1:] if value.startswith("v") else value


def kubernetes_versions_match(provider, live, receipt):
    live = normalise_kubernetes_version(live)
    receipt = normalise_kubernetes_version(receipt)
    if provider in {"eks-managed-nodes", "eks-fargate"}:
        return live == receipt or live.startswith(receipt + ".")
    return live == receipt


def finalise_result(profile, pending, receipt, reviewed_at=None, bundle=None):
    validate_profile(profile)
    validate_pending_evidence(pending)
    expected_outcome = profile["expectedOutcome"]
    try:
        if expected_outcome == "pass":
            if pending["outcome"] != "passed":
                raise EvaluationInputError("supported profiles require a passed pending result")
            validate_receipt(receipt)
        else:
            if pending["outcome"] != "unsupported_confirmed":
                raise EvaluationInputError("unsupported profiles require unsupported pending evidence")
            validate_unsupported_receipt(profile, receipt)
    except ReceiptError as error:
        raise EvaluationInputError(f"provider receipt is invalid: {error}") from error
    expected_identity = {"id": profile["id"], "digest": profile["profileDigest"]}
    if pending["profile"] != expected_identity:
        raise EvaluationInputError("pending evidence profile identity does not match the selected profile")
    if receipt["profile"] != expected_identity:
        raise EvaluationInputError("provider receipt profile identity does not match the selected profile")
    if pending["artefacts"]["providerReceiptDigest"] != receipt["receiptDigest"]:
        raise EvaluationInputError("pending evidence does not match the provider receipt digest")
    if expected_outcome == "pass" and bundle is None:
        raise EvaluationInputError("supported evidence review requires a verified evidence manifest")
    if expected_outcome == "pass":
        try:
            evidence_manifest = validate_supported_bundle(bundle, pending, receipt)
        except ManifestError as error:
            raise EvaluationInputError(f"evidence manifest is invalid: {error}") from error
        if pending["artefacts"]["evidenceManifestDigest"] != evidence_manifest["manifestDigest"]:
            raise EvaluationInputError("pending evidence does not match the evidence manifest digest")
    if expected_outcome != "pass" and bundle is not None:
        raise EvaluationInputError("unsupported evidence cannot claim a supported-run manifest")
    environment = pending["environment"]
    if expected_outcome == "pass":
        receipt_values = {
            "provider": receipt["provider"],
            "nodeImage": receipt["nodeImage"],
            "cniName": receipt["cniName"],
        }
        if any(environment[name] != value for name, value in receipt_values.items()):
            raise EvaluationInputError("pending environment does not match provider-owned inventory")
        receipt_provider = receipt["provider"]
    else:
        if environment != receipt["environment"]:
            raise EvaluationInputError("pending environment does not match unsupported live observation")
        expected_binding = {
            "sourceCommit": pending["artefacts"]["sourceCommit"],
            "chartDigest": pending["artefacts"]["chartDigest"],
        }
        if receipt["artefacts"] != expected_binding:
            raise EvaluationInputError("pending candidate does not match unsupported live observation")
        observation = receipt["unsupportedObservation"]
        if pending["reasonCode"] != observation["reasonCode"] \
                or observation["reasonCode"] != profile["expectedUnsupportedReason"]:
            raise EvaluationInputError("pending reason does not match unsupported live observation")
        receipt_provider = receipt["environment"]["provider"]
    if not kubernetes_versions_match(
            receipt_provider, environment["kubernetesVersion"], receipt["controlPlaneVersion"]):
        raise EvaluationInputError("pending Kubernetes version does not match provider-owned inventory")
    observed_at = parse_timestamp(receipt["observedAt"])
    completed_at = parse_timestamp(pending["completedAt"])
    proof = receipt.get("proof", {})
    if proof.get("source") == "recorded-live-conversion" and completed_at != observed_at:
        raise EvaluationInputError("recorded-live pending completion must equal the recorded observation")
    if observed_at > completed_at:
        raise EvaluationInputError("provider inventory cannot postdate the pending result")
    if completed_at - observed_at > MAX_OBSERVATION_AGE:
        raise EvaluationInputError("provider inventory is more than one hour older than the pending result")
    reviewed_at = reviewed_at or datetime.now(timezone.utc).date()
    if type(reviewed_at) is not date:
        raise EvaluationInputError("review date must be a date")
    completed_date = completed_at.date()
    if reviewed_at < completed_date or reviewed_at > completed_date + timedelta(days=MAX_REVIEW_DELAY_DAYS):
        raise EvaluationInputError("review must occur within seven days of evidence completion")
    final = copy.deepcopy(pending)
    final["reviewedAt"] = reviewed_at.isoformat()
    final["reviewDueAt"] = (completed_date + timedelta(days=profile["requalificationDays"])).isoformat()
    report = evaluate(profile, final, reviewed_at)
    if final["outcome"] in {"passed", "unsupported_confirmed"} and report["result"] != "pass":
        raise EvaluationInputError("reviewed evidence does not satisfy the selected profile")
    return final


def main():
    arguments = parse_args()
    if arguments.acknowledge != ACKNOWLEDGEMENT:
        raise SystemExit(f"set --acknowledge {ACKNOWLEDGEMENT} after reviewing the pending result")
    output = Path(arguments.output)
    if output.exists():
        raise SystemExit(f"refusing to overwrite {output}")
    try:
        profile = load_json(arguments.profile)
        input_path = Path(arguments.input).resolve()
        input_parent = input_path.parent
        output_path = output.resolve()
        if input_path.name != PENDING_NAME or output_path != input_parent / FINAL_NAME:
            raise EvaluationInputError("review input and output must use the exact bundle filenames")
        pending = load_json(input_path)
        receipt_path = Path(arguments.provider_receipt).resolve()
        receipt = load_json(receipt_path)
        bundle = None
        if profile["expectedOutcome"] == "pass":
            if arguments.evidence_manifest is None:
                raise EvaluationInputError("supported review requires --evidence-manifest")
            manifest_path = Path(arguments.evidence_manifest).resolve()
            if manifest_path != input_parent / MANIFEST_NAME:
                raise EvaluationInputError("evidence manifest must be the input bundle manifest")
            if receipt_path != input_parent / "provider-inventory.json":
                raise EvaluationInputError("provider receipt must be the input bundle provider-inventory.json")
            bundle = input_parent
        elif arguments.evidence_manifest is not None:
            raise EvaluationInputError("unsupported review does not accept --evidence-manifest")
        else:
            require_exact_bundle_entries(
                input_parent, {"provider-inventory.json", PENDING_NAME},
            )
        final = finalise_result(
            profile,
            pending,
            receipt,
            bundle=bundle,
        )
    except (EvaluationInputError, ManifestError) as error:
        raise SystemExit(f"provider review input error: {error}") from error
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as error:
        raise SystemExit(f"refusing to overwrite {output}") from error
    with os.fdopen(descriptor, "w", encoding="utf-8") as destination:
        destination.write(json.dumps(final, indent=2) + "\n")


if __name__ == "__main__":
    main()
