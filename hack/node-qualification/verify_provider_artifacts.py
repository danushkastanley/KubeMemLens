"""Verify proposal artefacts without starting or approving a provider run."""

import argparse
import platform
import re
import sys
from pathlib import Path

from common import ContractError, load, privacy, require, write_new
from prepare_provider import REPOSITORY
from provider_artifacts import verify
from provider_bundle import validate_bundle


def host_platform():
    system = platform.system().lower()
    architecture = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine().lower())
    require(system in {"linux", "darwin"} and architecture is not None, "unsupported verifier host platform")
    return system + "_" + architecture


def run(args):
    require(not Path(args.output).exists(), "artefact verification output already exists")
    profile = load(args.profile)
    plan = load(Path(args.proposal) / "plan.private.json")
    # Reading this digest checks file integrity. It grants no run approval.
    bundle = validate_bundle(args.proposal, profile, plan["planDigest"])
    inventory = load(REPOSITORY / "hack/provider-profiles" / (bundle.configuration["inventoryProfile"] + ".json"))
    require(re.fullmatch(inventory["expectations"]["architecturePattern"], args.architecture),
            "requested artefact architecture differs from the selected provider profile")
    result = verify(bundle, args.candidate_bundle, args.candidate_tag, args.image_archive, host_platform(),
                    {"runtime": {"architecture": args.architecture}})
    document = {"schemaVersion": 1, "scope": "candidate-artefact-verification", "qualified": False,
                "providerRunStarted": False, "planDigest": plan["planDigest"], "artefacts": result}
    privacy(document)
    write_new(args.output, document)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("proposal", "profile", "candidate-bundle", "candidate-tag", "image-archive", "output"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--architecture", required=True, choices=("amd64", "arm64"))
    args = parser.parse_args()
    try:
        run(args)
        print("Candidate artefacts verified; provider run approval and live qualification remain pending.")
        return 0
    except ContractError as error:
        print("Candidate artefact verification failed: " + str(error), file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("Candidate artefact verification failed (" + type(error).__name__ + ").", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
