"""Run the canonical provider protocol using explicitly unpublished artefacts."""

import argparse
import signal
import sys

from common import ContractError, privacy, require, write_new
from development_candidate import verify
from provider_inputs import prepare_with
from run_provider import interrupted, run_verified

ACKNOWLEDGEMENT = "run-unpublished-development-candidate"


def publish_development(path, document):
    # The outer schema is intentionally not a provider qualification record.
    # Release finalisers must reject it, including when its measurements pass.
    wrapped = {"schemaVersion": 1, "scope": "development-provider-experiment", "authority": "local-development",
               "qualified": False, "releaseQualificationGranted": False, "observation": document}
    privacy(wrapped)
    write_new(path, wrapped)


def run(args):
    require(args.acknowledge_development == ACKNOWLEDGEMENT, "explicit unpublished development acknowledgement required")
    bundle, proof, private, public = prepare_with(args, verify, publish_development)
    require(proof["authority"] == "local-development" and proof["releaseQualificationGranted"] is False,
            "development verifier returned another authority")
    result = run_verified(args, bundle, proof, private, public, publish_development)
    return {"scope": "development-provider-experiment", "authority": "local-development", "qualified": False,
            "releaseQualificationGranted": False, "protocolState": result["state"], "recordDigest": result["recordDigest"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("proposal", "profile", "plan-digest", "development-bundle", "development-manifest-digest",
                 "development-key", "development-key-digest", "cosign", "cosign-digest", "output-dir",
                 "acknowledge", "replacement-acknowledge", "acknowledge-development"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--architecture", required=True, choices=("amd64", "arm64"))
    parser.add_argument("--replacement-slot", required=True, type=int)
    args = parser.parse_args()
    previous = signal.signal(signal.SIGTERM, interrupted)
    try:
        run(args)
        print("Development provider protocol and owned Kubernetes cleanup completed; cloud cleanup remains pending. No release qualification granted.")
        return 0
    except KeyboardInterrupt:
        print("Development experiment interrupted; inspect cleanup evidence before another run.", file=sys.stderr)
        return 130
    except ContractError as error:
        print("Development experiment failed: " + str(error), file=sys.stderr)
        return 2
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("Development experiment failed (" + type(error).__name__ + "); inspect retained evidence.", file=sys.stderr)
        return 2
    finally:
        signal.signal(signal.SIGTERM, previous)


if __name__ == "__main__":
    raise SystemExit(main())
