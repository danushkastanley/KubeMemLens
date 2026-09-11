"""Exercise Kubernetes observation in an owned local fixture, without qualification."""

import argparse
import json
import sys
import time
from pathlib import Path

from common import ContractError, load, privacy, require, write_new
from evidence import validate_samples
from install_observers import attach, install_host
from kubernetes_runtime import KubernetesRuntime
from process import execute
from profiles import validate_profile
from sample import Window
from workload import manifest


def verify_kind_target(kubeconfig, context):
    require(context.startswith("kind-kube-memlens-node-context-"), "unexpected local fixture context")
    # Compare local configuration before contacting its endpoint or invoking an
    # authentication plugin. A context name alone does not identify a kind API.
    exported = execute(["kind", "get", "kubeconfig", "--name", context.removeprefix("kind-")])
    view = ["kubectl", "config", "view", "--raw", "--minify", "--context", context, "-o", "json", "--kubeconfig"]
    expected = json.loads(execute(view + ["/dev/stdin"], data=exported.encode()))
    actual = json.loads(execute(view + [kubeconfig]))
    require(actual == expected, "kubeconfig does not match the owned kind endpoint and identity")


def check(args):
    profile = validate_profile(load(args.profile))
    require(profile["profileClass"] == "local-kind", "observer check requires an owned local profile")
    require(not Path(args.output).exists(), "observer evidence already exists")
    verify_kind_target(args.kubeconfig, args.context)
    k = ["kubectl", "--kubeconfig", args.kubeconfig, "--context", args.context, "--request-timeout=10s"]
    namespace = "kube-memlens-node-context"
    workload_namespace = "node-qualification-load"
    uid = json.loads(execute(k + ["get", "namespace", namespace, "-o", "json"]))["metadata"]["uid"]
    image = profile["workload"]["image"]
    if args.phase == "baseline":
        execute(k + ["create", "-f", "-"], data=json.dumps(manifest(profile, args.node)).encode())
        execute(k + ["rollout", "status", "deployment/qualification-load", "-n", workload_namespace,
                     "--timeout=120s"], timeout=125)
    workload_uid = json.loads(execute(k + ["get", "namespace", workload_namespace, "-o", "json"]))["metadata"]["uid"]
    runtime = KubernetesRuntime(args.kubeconfig, args.context, namespace, uid, args.node, args.bridge,
                                workload_namespace, workload_uid)
    if args.phase == "baseline":
        install_host(runtime, image, {"kubernetes.io/hostname": args.node}, [{"operator": "Exists"}])
        attach(runtime, image, "agent")
    else:
        attach(runtime, image, "node-context", audience=args.audience)
    # Wait for real workload mapping. A command/transport failure is never a retry.
    deadline = time.monotonic() + 120
    while runtime.workload() != (profile["workload"]["containers"],) * 2:
        require(time.monotonic() < deadline, "workload mapping did not become ready")
        time.sleep(2)
    window = Window(runtime, args.phase)
    started = time.monotonic()
    window.observe(started)
    samples = []
    for index in range(1, 3):
        time.sleep(max(0, started + index * 15 - time.monotonic()))
        samples.append(window.observe(started))
    validate_samples(samples, args.phase)
    for sample in samples:
        require(sample["kubeletCPUMilli"] is not None and sample["kubeletMemoryBytes"] > 0,
                "kubelet resource telemetry is missing")
        require(sample["agentScanSeconds"] > 0 and sample["freshNodes"] == 1,
                "agent or freshness telemetry is missing")
        require(sample["unexpectedRestarts"] == sample["unexpectedOOMKills"] == 0,
                "observed an unexpected restart or OOM")
        if args.phase == "enabled":
            require(sample["producerCPUMilli"] is not None and sample["producerMemoryBytes"] > 0
                    and sample["sourceReads"] > 0 and sample["lastResponseBytes"] > 0,
                    "producer telemetry is missing")
    if args.phase == "enabled":
        require(samples[-1]["sourceReads"] > samples[0]["sourceReads"], "producer acquisition did not advance")
        require(samples[-1]["sourceFailures"] == samples[0]["sourceFailures"], "producer acquisition failed")
        require(runtime.api("/nodecontexts/" + args.node)["record"].get("lastGood"), "Node API has no observation")
        require(runtime.api("/nodecontexts/" + args.node + "/history?limit=1").get("series"), "Node history is empty")
    result = {"schemaVersion": 1, "scope": "local-kubernetes-observer-check", "qualified": False,
              "method": "kubernetes-probes-v1", "phase": args.phase,
              "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
              "samples": samples, "projectedIdentityReadable": args.phase == "enabled",
              "outcome": "pass", "cleanup": "pending"}
    privacy(result)
    write_new(args.output, result)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("kubeconfig", "context", "node", "bridge", "profile", "output"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--phase", choices=("baseline", "enabled"), required=True)
    parser.add_argument("--audience")
    try:
        check(parser.parse_args())
        return 0
    except ContractError as error:
        print(f"Kubernetes observer check failed: {error}", file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"Kubernetes observer check failed ({type(error).__name__})", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
