#!/usr/bin/env python3
"""Finalise a measured Node profile only with an independent review attestation."""

import argparse
import re
import sys
from datetime import timedelta
from pathlib import Path

from common import ContractError, DIGEST, digest, exact, instant, load, privacy, require, utc_now, write_new
from evaluate import evaluate

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "provider-inventory"))
from collect import ReceiptError, validate_receipt  # noqa: E402
from profile_contract import validate_profile as validate_inventory_profile  # noqa: E402

ACKNOWLEDGEMENT = "reviewed-node-stats-evidence"
RECEIPT_PROFILES = {
    "gke-standard": {"gke-cos-containerd-amd64", "gke-ubuntu-containerd-amd64"},
    "eks-managed-nodes": {"eks-al2023-containerd-amd64"},
    "aks-node-pools": {"aks-ubuntu-containerd-amd64"},
    "self-managed": {"self-managed-containerd", "self-managed-crio-amd64"},
}


def review(profile, evidence, attestation, receipt=None, now=None):
    result = evaluate(profile, evidence)
    require(result["outcome"] == "pass", "failed measurements cannot be qualified")
    require(not evidence["artefacts"]["sourceDirty"], "qualification requires immutable source")
    exact(attestation, {"schemaVersion", "recordDigest", "profileDigest", "independentReview", "decision", "reviewedAt", "attestationDigest"}, "review attestation")
    privacy(attestation)
    require(type(attestation["schemaVersion"]) is int and attestation["schemaVersion"] == 1 and attestation["independentReview"] is True and attestation["decision"] == "approve", "independent approval is required")
    require(attestation["recordDigest"] == evidence["recordDigest"] and attestation["profileDigest"] == profile["profileDigest"], "review does not bind this evidence and profile")
    require(isinstance(attestation["attestationDigest"], str) and DIGEST.fullmatch(attestation["attestationDigest"]) and attestation["attestationDigest"] == digest(attestation, "attestationDigest"), "review attestation digest mismatch")
    completed, reviewed = instant(evidence["completedAt"]), instant(attestation["reviewedAt"])
    current = now or utc_now()
    require(completed <= reviewed <= completed + timedelta(days=7) and reviewed <= current, "review date is outside the allowed window")
    expires = completed + timedelta(days=profile["requalificationDays"])
    require(current < expires, "qualification evidence has expired")
    if profile["profileClass"] == "provider":
        bind_receipt(evidence, receipt)
    else:
        require(receipt is None, "local evidence cannot import a provider receipt")
    result.update(qualified=True, reviewState="reviewed", reviewedAt=attestation["reviewedAt"], expiresAt=expires.strftime("%Y-%m-%dT%H:%M:%SZ"), attestationDigest=attestation["attestationDigest"])
    result.update(environment=dict(evidence["environment"]), artefacts=dict(evidence["artefacts"]), fieldAvailability=dict(evidence["fields"]), transport=dict(evidence["transport"]))
    result["evaluationDigest"] = digest(result, "evaluationDigest")
    return result


def bind_receipt(evidence, receipt):
    require(isinstance(receipt, dict), "provider receipt is required")
    privacy(receipt)
    try:
        validate_receipt(receipt)
    except (ReceiptError, TypeError) as error:
        raise ContractError("provider receipt failed its existing contract") from error
    env, artifacts = evidence["environment"], evidence["artefacts"]
    require(receipt["schemaVersion"] == 2 and receipt["qualificationToolCommit"] == artifacts["toolCommit"], "provider receipt is not bound to this qualification tool")
    require(receipt["profile"]["id"] in RECEIPT_PROFILES[env["provider"]] and receipt["provider"] == env["provider"], "provider receipt scope mismatch")
    inventory_profile = load(Path(__file__).resolve().parents[1] / "provider-profiles" / (receipt["profile"]["id"] + ".json"))
    try:
        validate_inventory_profile(inventory_profile)
    except ValueError as error:
        raise ContractError("inventory profile failed its canonical contract") from error
    require(receipt["profile"]["digest"] == inventory_profile["profileDigest"], "receipt inventory profile digest mismatch")
    for field, pattern in (("provider", "providerPattern"), ("osImage", "osImagePattern"), ("runtime", "runtimePattern"), ("architecture", "architecturePattern"), ("cgroupVersion", "cgroupPattern"), ("nodeImage", "nodeImagePattern"), ("cni", "cniPattern")):
        require(re.fullmatch(inventory_profile["expectations"][pattern], env[field]) is not None, "environment does not match the canonical inventory profile")
    require(receipt["receiptDigest"] == env["providerReceiptDigest"] and receipt["nodeImage"] == env["nodeImage"] and receipt["cniName"] == env["cni"], "provider receipt environment mismatch")
    live, control = env["kubernetes"].removeprefix("v"), receipt["controlPlaneVersion"].removeprefix("v")
    matches = live == control or env["provider"] == "eks-managed-nodes" and live.startswith(control + ".")
    require(matches, "provider control-plane version mismatch")
    observed = instant(receipt["observedAt"])
    require(instant(evidence["startedAt"]) - timedelta(hours=1) <= observed <= instant(evidence["completedAt"]), "provider receipt is outside this run's observation window")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("profile", "evidence", "attestation", "output", "acknowledge"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--provider-receipt")
    args = parser.parse_args()
    try:
        require(args.acknowledge == ACKNOWLEDGEMENT, "explicit independent-review acknowledgement is required")
        result = review(load(args.profile), load(args.evidence), load(args.attestation), load(args.provider_receipt) if args.provider_receipt else None)
        write_new(args.output, result)
        return 0
    except (ContractError, OSError) as error:
        print(f"Node qualification review: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
