#!/usr/bin/env python3

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = ROOT.parent
INVENTORY = ROOT / "provider-inventory"
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(INVENTORY))

from observe_unsupported import ReceiptError, validate_unsupported_receipt  # noqa: E402
from profile_contract import (  # noqa: E402
    CHECK_IDS,
    EvaluationInputError,
    load_json,
    parse_timestamp,
    validate_pending_evidence,
    validate_profile,
)
from validate import evaluate_pending  # noqa: E402
from verify_chart_archive import ArchiveError, verify_archive, verify_chart_metadata  # noqa: E402


DIGEST_PATTERN = re.compile(r"sha256:[a-f0-9]{64}")
COMMIT_PATTERN = re.compile(r"[a-f0-9]{40}")
MAX_OBSERVATION_AGE = timedelta(hours=1)
PENDING_NAME = "provider-qualification.pending.json"
RECEIPT_NAME = "provider-inventory.json"


def digest_file(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as source:
        while chunk := source.read(64 * 1024):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def run(command):
    try:
        result = subprocess.run(command, check=False, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise EvaluationInputError(f"{command[0]} verification command did not complete") from error
    if result.returncode != 0:
        raise EvaluationInputError(f"{command[0]} verification command failed")
    return result.stdout


def require_digest(value, name):
    if not isinstance(value, str) or DIGEST_PATTERN.fullmatch(value) is None:
        raise EvaluationInputError(f"{name} must be an exact lowercase SHA-256 digest")


def tracked_at_commit(path, source_commit):
    try:
        relative = path.resolve().relative_to(REPOSITORY).as_posix()
    except ValueError as error:
        raise EvaluationInputError("tracked qualification input is outside the repository") from error
    content = run(["git", "show", f"{source_commit}:{relative}"]).encode()
    if content != path.read_bytes():
        raise EvaluationInputError(f"tracked qualification input differs from source commit: {relative}")


def chart_version(archive):
    for line in run(["helm", "show", "chart", str(archive)]).splitlines():
        if line.startswith("version:"):
            return line.split(":", 1)[1].strip()
    raise EvaluationInputError("chart archive has no version")


def verify_inputs(profile_path, receipt_path, chart_archive, expected_chart_digest,
                  values_path, expected_values_digest, source_commit, image_digest):
    require_digest(expected_chart_digest, "chart digest")
    require_digest(expected_values_digest, "values digest")
    require_digest(image_digest, "image digest")
    if COMMIT_PATTERN.fullmatch(source_commit) is None:
        raise EvaluationInputError("source commit must be 40 lowercase hexadecimal characters")
    if run(["git", "rev-parse", "HEAD"]).strip() != source_commit:
        raise EvaluationInputError("source commit is not checked out")
    if run(["git", "status", "--porcelain", "--untracked-files=all"]).strip():
        raise EvaluationInputError("repository must be clean before recording unsupported evidence")
    profile = load_json(profile_path)
    validate_profile(profile)
    canonical_profile = ROOT / "provider-profiles" / f"{profile['id']}.json"
    if profile["expectedOutcome"] != "unsupported" or Path(profile_path).resolve() != canonical_profile.resolve():
        raise EvaluationInputError("profile must be a canonical unsupported profile")
    canonical_values = ROOT / "provider-values" / f"{profile['id']}.yaml"
    if Path(values_path).resolve() != canonical_values.resolve():
        raise EvaluationInputError("values must be the canonical unsupported-profile values")
    tracked_at_commit(canonical_profile, source_commit)
    tracked_at_commit(canonical_values, source_commit)
    if digest_file(chart_archive) != expected_chart_digest:
        raise EvaluationInputError("chart archive digest does not match")
    if digest_file(canonical_values) != expected_values_digest:
        raise EvaluationInputError("values digest does not match")
    try:
        verify_archive(chart_archive, ROOT.parent / "charts" / "kube-memlens")
        verify_chart_metadata(chart_archive, ROOT.parent / "charts" / "kube-memlens")
    except ArchiveError as error:
        raise EvaluationInputError(str(error)) from error
    receipt = load_json(receipt_path)
    try:
        validate_unsupported_receipt(profile, receipt)
    except ReceiptError as error:
        raise EvaluationInputError(str(error)) from error
    artefacts = {
        "imageDigest": image_digest,
        "chartDigest": expected_chart_digest,
        "valuesDigest": expected_values_digest,
        "providerReceiptDigest": receipt["receiptDigest"],
        "evidenceManifestDigest": None,
        "probeImageDigest": None,
        "sourceCommit": source_commit,
        "chartVersion": chart_version(chart_archive),
    }
    return profile, receipt, artefacts


def build_pending(profile, receipt, artefacts, completed_at):
    validate_profile(profile)
    try:
        validate_unsupported_receipt(profile, receipt)
    except ReceiptError as error:
        raise EvaluationInputError(str(error)) from error
    expected_binding = {
        "sourceCommit": artefacts["sourceCommit"],
        "chartDigest": artefacts["chartDigest"],
    }
    if receipt["artefacts"] != expected_binding:
        raise EvaluationInputError("unsupported receipt does not match the candidate source and chart")
    completed = parse_timestamp(completed_at)
    observed = parse_timestamp(receipt["observedAt"])
    if observed > completed or completed - observed > MAX_OBSERVATION_AGE:
        raise EvaluationInputError("unsupported observation must be no more than one hour old")
    checks = dict.fromkeys(CHECK_IDS, "not_run")
    checks["prerequisites"] = "unsupported"
    pending = {
        "schemaVersion": 1,
        "outcome": "unsupported_confirmed",
        "completedAt": completed_at,
        "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
        "artefacts": artefacts,
        "environment": receipt["environment"],
        "checks": checks,
        "reasonCode": receipt["unsupportedObservation"]["reasonCode"],
        "privacy": {
            "clusterIdentifiersIncluded": False,
            "workloadIdentifiersIncluded": False,
            "providerResourceIdentifiersIncluded": False,
            "rawErrorsIncluded": False,
            "rawLogsIncluded": False,
        },
    }
    validate_pending_evidence(pending)
    report = evaluate_pending(profile, pending)
    if report["result"] != "pass":
        raise EvaluationInputError("unsupported pending evidence does not satisfy the selected profile")
    return pending


def main():
    parser = argparse.ArgumentParser(description="Build strict pending evidence from a live unsupported receipt.")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--provider-receipt", required=True)
    parser.add_argument("--chart-archive", required=True)
    parser.add_argument("--chart-digest", required=True)
    parser.add_argument("--values", required=True)
    parser.add_argument("--values-digest", required=True)
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--image-digest", required=True)
    parser.add_argument("--output", required=True)
    arguments = parser.parse_args()
    output = Path(arguments.output)
    if output.exists() or output.is_symlink():
        raise SystemExit("refusing to overwrite unsupported pending evidence")
    receipt_path = Path(arguments.provider_receipt).resolve()
    if receipt_path.name != RECEIPT_NAME or output.resolve() != receipt_path.parent / PENDING_NAME:
        raise SystemExit("unsupported receipt and pending output must use the exact bundle filenames")
    try:
        profile, receipt, artefacts = verify_inputs(
            Path(arguments.profile), receipt_path, Path(arguments.chart_archive),
            arguments.chart_digest, Path(arguments.values), arguments.values_digest,
            arguments.source_commit, arguments.image_digest,
        )
        completed_at = datetime.now(timezone.utc).replace(microsecond=0).strftime("%Y-%m-%dT%H:%M:%SZ")
        pending = build_pending(profile, receipt, artefacts, completed_at)
    except EvaluationInputError as error:
        raise SystemExit(f"unsupported pending evidence error: {error}") from error
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as error:
        raise SystemExit("refusing to overwrite unsupported pending evidence") from error
    with os.fdopen(descriptor, "w", encoding="utf-8") as destination:
        destination.write(json.dumps(pending, indent=2) + "\n")


if __name__ == "__main__":
    main()
