#!/usr/bin/env python3
"""Read-only restricted qualification against an explicitly selected context."""

import argparse
import hashlib
import importlib.util
import json
import os
import re
import selectors
import subprocess
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

PROFILE_IDS = ("local-kind", "gke-autopilot", "eks-fargate", "aks-virtual-nodes")
MAX_OUTPUT = 64 << 20


class QualificationError(ValueError):
    """Only fixed, non-sensitive operational diagnostics may reach the caller."""


spec = importlib.util.spec_from_file_location(
    "privacy_contract", Path(__file__).parents[1] / "provider-profiles" / "privacy_contract.py"
)
privacy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(privacy)


def run(command, limit=MAX_OUTPUT, operation="command"):
    """Bound both runtime and captured bytes; raw provider errors are discarded."""
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    result = bytearray()
    deadline = time.monotonic() + 30
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise QualificationError("qualification command exceeded its time limit")
                events = selector.select(min(remaining, 0.2))
                if not events:
                    continue
                chunk = os.read(process.stdout.fileno(), 65536)
                if not chunk:
                    break
                if len(result) + len(chunk) > limit:
                    raise QualificationError("qualification command exceeded its output limit")
                result.extend(chunk)
        code = process.wait(timeout=max(0.01, deadline - time.monotonic()))
        if code:
            raise QualificationError(f"qualification {operation} failed; inspect the selected caller locally")
        return bytes(result)
    finally:
        if process.poll() is None:
            process.kill()
        process.wait()
        process.stdout.close()


def read_capture(path):
    if path.stat().st_size > MAX_OUTPUT or path.stat().st_mode & 0o777 != 0o600:
        raise QualificationError("capture exceeded its bound or was not private")
    document = json.loads(path.read_bytes())
    if document.get("schemaVersion") != 3 or document.get("redacted") is not True:
        raise QualificationError("qualification requires a redacted schema 3 capture")
    if document["observations"]["mode"] != "restricted":
        raise QualificationError("qualification changed evidence mode")
    return document


def evidence(profile, digest, version, document, permission_states):
    if profile not in PROFILE_IDS or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise QualificationError("invalid profile or binary digest")
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9._+-]+)?", version):
        raise QualificationError("invalid control-plane version")
    batch = document["observations"]
    pods = batch["pods"]
    if not pods:
        raise QualificationError("qualification needs an existing authorised Pod")
    reports = []
    for source in batch["sources"]:
        reports.append({key: source[key] for key in
                        ("source", "scope", "availability", "freshness", "completeness")})
        for key in ("apiVersion", "reason", "capability"):
            if key in source:
                reports[-1][key] = source[key]
    measured = sum(pod["workingSet"]["bytes"] is not None for pod in pods)
    result = {
        "schemaVersion": 1, "profile": profile, "mode": "restricted",
        "observedAt": datetime.now(timezone.utc).isoformat(),
        "binaryDigest": digest, "controlPlaneVersion": version,
        "outcome": "observed" if measured else "limited",
        "completeness": batch["completeness"], "sources": reports,
        "permissions": permission_states,
        "visibility": {"capturedPods": len(pods), "capturedPodsWithWorkingSet": measured,
                       "capturedNodes": len(batch["nodes"] or [])},
        "workflows": {"capture": "passed", "offlineReplay": "passed",
                      "offlineCompare": "passed", "privateFile": "passed"},
        "unavailable": ["composition", "local-oom-deltas", "psi", "history", "trace"],
        "cleanup": {"localArtifactsRemoved": True, "remoteChanges": False},
        "providerSupportQualified": False,
    }
    privacy.reject_sensitive_content(result, QualificationError)
    return result


def qualify(args):
    cli = str(Path(args.cli).resolve(strict=True))
    with open(cli, "rb") as binary:
        digest = "sha256:" + hashlib.file_digest(binary, "sha256").hexdigest()
    kubectl = ["kubectl", "--kubeconfig", args.kubeconfig, "--context", args.context,
               "--request-timeout=15s"]
    version = json.loads(run(kubectl + ["version", "-o", "json"], 1 << 20, "version"))["serverVersion"]["gitVersion"]
    permissions = {}
    for key, group, resource, verb in (
        ("podList", "", "pods", "list"), ("podMetricsList", "metrics.k8s.io", "pods", "list"),
        ("nodeGet", "", "nodes", "get"), ("nodeMetricsGet", "metrics.k8s.io", "nodes", "get"),
    ):
        attrs = {"group": group, "resource": resource, "verb": verb}
        if resource == "pods":
            attrs["namespace"] = args.namespace
        # POST the built-in review directly: kubectl schema validation can list
        # CRDs, which an otherwise valid scoped reader need not be allowed.
        # SSAR is a decision query, not an RBAC mutation. The namespaced CLI
        # capture subsequently authorises every resource read independently.
        review = {"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview",
                  "spec": {"resourceAttributes": attrs}}
        with tempfile.TemporaryDirectory(prefix="restricted-review-") as directory:
            path = Path(directory) / "review.json"
            path.write_text(json.dumps(review))
            path.chmod(0o600)
            response = json.loads(run(kubectl + ["create", "--raw", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", "-f", str(path)], 1 << 20, "access review"))
        permissions[key] = "allowed" if response["status"].get("allowed") else "denied"
    with tempfile.TemporaryDirectory(prefix="restricted-qualification-") as directory:
        path = Path(directory) / "incident.json"
        run([cli, "--kubeconfig", args.kubeconfig, "--context", args.context, "--mode=restricted",
             "capture", "-n", args.namespace, "-o", str(path)], operation="capture")
        document = read_capture(path)
        replay = [cli, "--kubeconfig=/missing/offline-qualification", "replay", str(path)]
        if run(replay, operation="replay") != run(replay, operation="replay"):
            raise QualificationError("offline replay was not deterministic")
        pods = document["observations"]["pods"]
        if not pods:
            raise QualificationError("qualification needs an existing authorised Pod")
        reference = pods[0]["namespace"] + "/" + pods[0]["name"]
        run([cli, "--kubeconfig=/missing/offline-qualification", "compare", "--before", str(path),
             "--after", str(path), "--pod", reference], operation="compare")
    result = evidence(args.profile, digest, version, document, permissions)
    # Exclusive creation prevents accidentally replacing an earlier run record.
    descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as output:
        json.dump(result, output, indent=2)
        output.write("\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=PROFILE_IDS, required=True)
    for name in ("cli", "kubeconfig", "context", "namespace", "output"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    qualify(args)
    print("Restricted evidence recorded; provider support still requires profile review.")


if __name__ == "__main__":
    try:
        main()
    except QualificationError as error:
        raise SystemExit(str(error))
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        raise SystemExit("Restricted qualification failed; no provider support claim was changed.")
