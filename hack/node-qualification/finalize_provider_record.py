"""Confirm provider cleanup from an independent attestation, without granting review."""

import argparse
import copy
import sys
from pathlib import Path

from common import ContractError, digest, exact, instant, load, privacy, require, utc_now, write_new
from evaluate import evaluate
from evidence import validate_evidence
from review import bind_receipt

ACKNOWLEDGEMENT = "confirm-independent-provider-cleanup"


def finalize(profile, evidence, receipt, attestation, now=None):
    validate_evidence(profile, evidence)
    require(profile["profileClass"] == "provider", "provider cleanup requires provider evidence")
    bind_receipt(evidence, receipt)
    require(evidence["cleanup"] == {"workloadsRemoved": True, "rbacRemoved": True, "cloudResources": "pending"},
            "provider confirmation requires completed Kubernetes cleanup and pending provider cleanup")
    exact(attestation, {"schemaVersion", "recordDigest", "profileDigest", "independentCheck", "cloudResourcesRemoved",
                        "checkedAt", "attestationDigest"}, "provider cleanup attestation")
    privacy(attestation)
    require(type(attestation["schemaVersion"]) is int and attestation["schemaVersion"] == 1
            and attestation["independentCheck"] is True and attestation["cloudResourcesRemoved"] is True,
            "independent confirmation of all disposable provider resources is required")
    require(attestation["recordDigest"] == evidence["recordDigest"]
            and attestation["profileDigest"] == profile["profileDigest"]
            and attestation["attestationDigest"] == digest(attestation, "attestationDigest"),
            "provider cleanup attestation does not bind this record and profile")
    require(instant(evidence["completedAt"]) <= instant(attestation["checkedAt"]) <= (now or utc_now()),
            "provider cleanup check must follow collection and cannot be in the future")
    result = copy.deepcopy(evidence)
    result["cleanup"]["cloudResources"] = "confirmed"
    result["recordDigest"] = digest(result, "recordDigest")
    evaluation = evaluate(profile, result)
    return result, evaluation


def run(args):
    require(args.acknowledge == ACKNOWLEDGEMENT, "explicit independent-cleanup acknowledgement is required")
    root = Path(args.output_dir)
    require(root.is_absolute() and not root.exists(), "cleanup output must be a new absolute directory")
    profile, evidence, receipt, attestation = (load(path) for path in
        (args.profile, args.evidence, args.provider_receipt, args.cleanup_attestation))
    result, evaluation = finalize(profile, evidence, receipt, attestation)
    # Keep the submitted attestation alongside the final record. Its record
    # digest binds the unchanged intermediate record, which is retained too.
    root.mkdir(mode=0o700)
    for name, document in (("qualification-observations.json", evidence), ("cleanup-attestation.json", attestation),
                           ("provider-inventory.json", receipt), ("qualification.json", result),
                           ("qualification-evaluation.json", evaluation)):
        write_new(root / name, document)
    return evaluation


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("profile", "evidence", "provider-receipt", "cleanup-attestation", "output-dir", "acknowledge"):
        parser.add_argument("--" + name, required=True)
    try:
        result = run(parser.parse_args())
        print("Provider cleanup confirmed; measurements " + result["outcome"] + "; independent qualification review pending.")
        return 0 if result["outcome"] == "pass" else 1
    except ContractError as error:
        print("Provider cleanup confirmation failed: " + str(error), file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("Provider cleanup confirmation failed (" + type(error).__name__ + ").", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
