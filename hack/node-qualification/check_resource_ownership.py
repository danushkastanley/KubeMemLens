"""Verify UID-safe cleanup against a real owned kind API."""

import argparse
import json
import sys
import tempfile
import time
from pathlib import Path

from check_kubernetes_observer import verify_kind_target
from common import ContractError, privacy, require, write_new
from owned_resources import OwnedResources, Resource
from process import execute


def check(kubeconfig, context, output):
    require(not Path(output).exists(), "ownership evidence already exists")
    verify_kind_target(kubeconfig, context)
    command = ["kubectl", "--kubeconfig", kubeconfig, "--context", context, "--request-timeout=10s"]
    def k(*args, **kwargs):
        return execute(command + list(args), **kwargs)
    with tempfile.TemporaryDirectory(prefix="node-ownership-") as private:
        root = Path(private)
        original = OwnedResources(k, root / "original")
        replacement = OwnedResources(k, root / "replacement")
        ns = {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "kube-memlens-qualification-ownership"}}
        secret = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "ownership-probe", "namespace": ns["metadata"]["name"]},
                  "stringData": {"fixture": "private-fixture-payload"}}
        namespace, child = Resource.from_object(ns), Resource.from_object(secret)
        original.require_absent([namespace])
        original.create(ns)
        original.create(secret)
        original.delete(child)
        deadline = time.monotonic() + 30
        while original.get(child) is not None:
            require(time.monotonic() < deadline, "original child deletion timed out")
            time.sleep(.2)
        replacement.create(secret)
        refused = False
        try:
            original.cleanup()
        except ContractError:
            refused = True
        require(refused, "cleanup accepted a replacement object")
        replacement.verify(child)
        original.verify(namespace)
        # The check owns the replacement too, through its separate creation
        # receipt. Remove it explicitly before asking the first owner to finish.
        replacement.cleanup()
        original.cleanup()
        require(original.get(namespace) is None, "owned namespace remains")
        for path in root.rglob("*.json"):
            require("private-fixture-payload" not in path.read_text(), "ownership journal retained Secret data")
    result = {"schemaVersion": 1, "scope": "local-ownership-diagnostic", "qualified": False,
              "uidPreconditions": True, "replacementPreserved": True, "parentPreservedOnConflict": True,
              "secretPayloadRetained": False, "cleanup": "passed"}
    privacy(result)
    write_new(output, result)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("kubeconfig", "context", "output"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    try:
        check(args.kubeconfig, args.context, args.output)
        return 0
    except ContractError as error:
        print(f"ownership check failed: {error}", file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"ownership check failed ({type(error).__name__})", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
