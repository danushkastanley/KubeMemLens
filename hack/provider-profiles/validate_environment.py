#!/usr/bin/env python3

import argparse
import json
import sys

from profile_contract import (
    ENVIRONMENT_KEYS,
    EvaluationInputError,
    environment_failures,
    load_json,
    validate_profile,
)


def main():
    parser = argparse.ArgumentParser(description="Match live inventory to one provider profile.")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--environment", required=True)
    arguments = parser.parse_args()
    try:
        profile = load_json(arguments.profile)
        validate_profile(profile)
        environment = load_json(arguments.environment)
        if environment.pop("schemaVersion", None) != 1 or set(environment) != ENVIRONMENT_KEYS:
            raise EvaluationInputError("environment fields do not match schema version 1")
        failures = environment_failures(profile, environment)
    except EvaluationInputError as error:
        print(f"provider environment input error: {error}", file=sys.stderr)
        raise SystemExit(2) from error
    report = {
        "schemaVersion": 1,
        "result": "pass" if not failures else "fail",
        "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
        "failures": failures,
    }
    print(json.dumps(report, separators=(",", ":"), sort_keys=True))
    raise SystemExit(0 if not failures else 1)


if __name__ == "__main__":
    main()
