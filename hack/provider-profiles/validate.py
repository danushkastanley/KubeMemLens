#!/usr/bin/env python3

import argparse
import json
import sys
from datetime import datetime, timezone

from profile_contract import (
    EvaluationInputError,
    environment_failures,
    evaluate,
    load_json,
    parse_date,
    validate_pending_evidence,
    validate_profile,
)


def evaluate_pending(profile, evidence):
    validate_profile(profile)
    validate_pending_evidence(evidence)
    expected_identity = {"id": profile["id"], "digest": profile["profileDigest"]}
    if evidence["profile"] != expected_identity:
        raise EvaluationInputError("pending evidence profile identity does not match the selected profile")
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
        "profile": expected_identity,
        "outcome": outcome,
        "failures": failures,
    }


def parse_args():
    parser = argparse.ArgumentParser(description="Validate a provider qualification profile and evidence bundle.")
    parser.add_argument("--profile", required=True, help="Path to a provider profile JSON file")
    parser.add_argument("--evidence", help="Path to a provider qualification evidence JSON file")
    parser.add_argument("--pending", action="store_true", help="validate unreviewed pending evidence")
    parser.add_argument("--as-of", help="Review date in YYYY-MM-DD form. Defaults to today in UTC.")
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        profile = load_json(args.profile)
        validate_profile(profile)
        if args.pending and args.evidence is None:
            raise EvaluationInputError("--pending requires --evidence")
        if args.evidence is None:
            report = {
                "schemaVersion": 1,
                "result": "pass",
                "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
            }
        elif args.pending:
            report = evaluate_pending(profile, load_json(args.evidence))
        else:
            as_of = parse_date(args.as_of, "--as-of") if args.as_of else datetime.now(timezone.utc).date()
            report = evaluate(profile, load_json(args.evidence), as_of)
    except EvaluationInputError as error:
        print(f"provider qualification input error: {error}", file=sys.stderr)
        return 2
    print(json.dumps(report, ensure_ascii=False, separators=(",", ":"), sort_keys=True))
    return 0 if report["result"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
