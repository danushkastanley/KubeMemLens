#!/usr/bin/env python3

import argparse
import copy
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

from evidence_manifest import (
    MANIFEST_NAME, ManifestError, PENDING_NAME, read_json_file, require_bundle,
    require_exact_bundle_entries,
)
from profile_contract import EvaluationInputError, evaluate, load_json, parse_date, validate_profile
from review_result import finalise_result


ROOT = Path(__file__).resolve().parent
SUPPORTED_PROFILE_IDS = frozenset({
    "eks-al2023-containerd-amd64",
    "gke-cos-containerd-amd64",
    "gke-ubuntu-containerd-amd64",
    "self-managed-containerd",
    "self-managed-crio-amd64",
})
UNSUPPORTED_PROFILE_IDS = frozenset({
    "aks-ubuntu-containerd-amd64",
    "aks-virtual-nodes",
    "cgroup-v1",
    "eks-fargate",
    "gke-autopilot",
    "windows-deep-mode",
})
PROFILE_IDS = SUPPORTED_PROFILE_IDS | UNSUPPORTED_PROFILE_IDS
RELEASE_FIELDS = ("sourceCommit", "imageDigest", "chartDigest", "chartVersion")
FINAL_NAME = "provider-qualification.json"
RECEIPT_NAME = "provider-inventory.json"


class MatrixInputError(EvaluationInputError):
    pass


def load_canonical_profiles(root=ROOT):
    profile_paths = {path.stem: path for path in root.glob("*.json")}
    if set(profile_paths) != PROFILE_IDS:
        raise MatrixInputError("canonical provider profile set does not match the qualification matrix")
    profiles = {}
    for profile_id, path in profile_paths.items():
        profile = load_json(path)
        validate_profile(profile)
        expected = "pass" if profile_id in SUPPORTED_PROFILE_IDS else "unsupported"
        if profile["id"] != profile_id or profile["expectedOutcome"] != expected:
            raise MatrixInputError(f"canonical profile {profile_id} has the wrong identity or outcome")
        profiles[profile_id] = profile
    return profiles


def index_records(records, profiles):
    indexed = {}
    for record in records:
        if not isinstance(record, dict):
            raise MatrixInputError("matrix evidence must contain JSON objects")
        if "reviewedAt" not in record or "reviewDueAt" not in record:
            raise MatrixInputError("matrix requires reviewed final evidence, not pending run results")
        identity = record.get("profile")
        profile_id = identity.get("id") if isinstance(identity, dict) else None
        if profile_id not in profiles:
            raise MatrixInputError("matrix evidence references an unknown profile")
        if profile_id in indexed:
            raise MatrixInputError(f"matrix contains duplicate evidence for {profile_id}")
        indexed[profile_id] = record
    missing = PROFILE_IDS - set(indexed)
    if missing:
        raise MatrixInputError("matrix is missing evidence for: " + ", ".join(sorted(missing)))
    if len(indexed) != len(PROFILE_IDS):
        raise MatrixInputError("matrix evidence row count is invalid")
    return indexed


def load_reviewed_bundle(final_path, profiles):
    final_path = Path(final_path)
    if final_path.name != FINAL_NAME:
        raise MatrixInputError(f"matrix input must be an exact {FINAL_NAME} bundle path")
    bundle = require_bundle(final_path.parent)
    final = read_json_file(final_path)
    identity = final.get("profile") if isinstance(final, dict) else None
    profile_id = identity.get("id") if isinstance(identity, dict) else None
    if profile_id not in profiles:
        raise MatrixInputError("reviewed bundle references an unknown profile")
    profile = profiles[profile_id]
    receipt = read_json_file(bundle / RECEIPT_NAME)
    reviewed_at = parse_date(final.get("reviewedAt"), "reviewed evidence reviewedAt")
    if "reviewDueAt" not in final:
        raise MatrixInputError("matrix requires reviewed final evidence")
    pending = copy.deepcopy(final)
    pending.pop("reviewedAt")
    pending.pop("reviewDueAt")
    manifest_path = bundle / MANIFEST_NAME
    if profile_id in SUPPORTED_PROFILE_IDS:
        regenerated = finalise_result(profile, pending, receipt, reviewed_at, bundle=bundle)
    else:
        if manifest_path.exists() or manifest_path.is_symlink():
            raise MatrixInputError("unsupported reviewed bundle cannot contain a supported evidence manifest")
        require_exact_bundle_entries(
            bundle, {RECEIPT_NAME, FINAL_NAME}, {PENDING_NAME},
        )
        if (bundle / PENDING_NAME).exists() and read_json_file(bundle / PENDING_NAME) != pending:
            raise MatrixInputError("unsupported bundled pending evidence differs from the reviewed final")
        regenerated = finalise_result(profile, pending, receipt, reviewed_at)
    if regenerated != final:
        raise MatrixInputError("reviewed evidence differs from strict bundle reconstruction")
    return final


def load_reviewed_bundles(paths, profiles=None):
    profiles = profiles or load_canonical_profiles()
    return [load_reviewed_bundle(path, profiles) for path in paths]


def evaluate_matrix(records, as_of=None, profiles=None):
    profiles = profiles or load_canonical_profiles()
    if set(profiles) != PROFILE_IDS:
        raise MatrixInputError("matrix requires exactly the canonical provider profiles")
    as_of = as_of or datetime.now(timezone.utc).date()
    indexed = index_records(records, profiles)
    release = None
    failures = []
    warnings = []
    rows = []
    for profile_id in sorted(PROFILE_IDS):
        evidence = indexed[profile_id]
        report = evaluate(profiles[profile_id], evidence, as_of)
        if report["result"] != "pass":
            failures.append(f"{profile_id}: reviewed evidence did not pass")
        warnings.extend(f"{profile_id}: {warning}" for warning in report["warnings"])
        if release is None:
            release = {field: evidence["artefacts"][field] for field in RELEASE_FIELDS}
        mismatched = [field for field in RELEASE_FIELDS if evidence["artefacts"][field] != release[field]]
        if mismatched:
            failures.append(f"{profile_id}: release identity differs for {', '.join(mismatched)}")
        rows.append({
            "id": profile_id,
            "outcome": evidence["outcome"],
            "reviewedAt": evidence["reviewedAt"],
            "reviewDueAt": evidence["reviewDueAt"],
            "freshness": report["freshness"],
            "warnings": report["warnings"],
            "bundlePath": f"{profile_id}/{FINAL_NAME}",
            "recordDigest": "sha256:" + hashlib.sha256(json.dumps(
                evidence, ensure_ascii=False, separators=(",", ":"), sort_keys=True,
            ).encode()).hexdigest(),
            "providerReceiptDigest": evidence["artefacts"]["providerReceiptDigest"],
            "evidenceManifestDigest": evidence["artefacts"]["evidenceManifestDigest"],
        })
    supported_probe_digests = {
        indexed[profile_id]["artefacts"]["probeImageDigest"]
        for profile_id in SUPPORTED_PROFILE_IDS
    }
    if len(supported_probe_digests) != 1 or None in supported_probe_digests:
        failures.append("supported matrix rows do not share one approved probe image digest")
    return {
        "schemaVersion": 1,
        "result": "pass" if not failures else "fail",
        "freshness": "stale" if any(row["freshness"] == "stale" for row in rows) else "current",
        "release": release,
        "supportedProbeImageDigest": next(iter(supported_probe_digests), None)
        if len(supported_probe_digests) == 1 else None,
        "rows": rows,
        "warnings": warnings,
        "failures": failures,
    }


def main():
    parser = argparse.ArgumentParser(description="Evaluate the complete reviewed provider qualification matrix.")
    parser.add_argument("--as-of", help="Evaluation date in YYYY-MM-DD form. Defaults to today in UTC.")
    parser.add_argument("evidence", nargs="+", help="Bundle-contained provider-qualification.json records")
    arguments = parser.parse_args()
    try:
        as_of = parse_date(arguments.as_of, "--as-of") if arguments.as_of else None
        profiles = load_canonical_profiles()
        report = evaluate_matrix(load_reviewed_bundles(arguments.evidence, profiles), as_of, profiles)
    except (EvaluationInputError, ManifestError) as error:
        print(f"provider matrix input error: {error}", file=sys.stderr)
        return 2
    print(json.dumps(report, ensure_ascii=False, separators=(",", ":"), sort_keys=True))
    return 0 if report["result"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
