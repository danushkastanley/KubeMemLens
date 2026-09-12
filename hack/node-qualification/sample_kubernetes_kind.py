"""Measure the fixed Kubernetes-observer protocol in a verified owned kind fixture."""

import argparse
import json
import sys
from pathlib import Path

from check_kubernetes_observer import verify_kind_target
from common import ContractError, exact, load, require, write_new
from install_observers import attach, install_host
from kubernetes_runtime import KubernetesRuntime
from observer_specs import ephemeral_observer, host_observer, host_policy
from process import execute
from profiles import validate_profile
from sample_nodes import measure_nodes
from window_contract import validate_window


def measure(args):
    p = validate_profile(load(args.profile))
    require(p["profileClass"] == "local-kind" and p["workload"]["linuxNodes"] == 1, "owned single-Node kind profile required")
    require(not Path(args.output).exists(), "measurement output already exists")
    verify_kind_target(args.kubeconfig, "kind-" + args.cluster)
    root = Path(args.kubeconfig).resolve().parent
    k = ["kubectl", "--kubeconfig", args.kubeconfig, "--context", "kind-" + args.cluster, "--request-timeout=10s"]
    namespace, workload = "kube-memlens-node-context", "node-qualification-load"
    current = {}
    for key, kind, name in (("namespaceUID", "namespace", namespace), ("workloadUID", "namespace", workload),
                            ("nodeUID", "node", args.node)):
        current[key] = json.loads(execute(k + ["get", kind, name, "-o", "json"]))["metadata"]["uid"]
    current["node"] = args.node
    binding = root / "qualification-pool-binding.private.json"
    if args.phase == "baseline":
        write_new(binding, current)
    else:
        expected = load(binding)
        exact(expected, set(current), "private pool binding")
        require(current == expected, "selected Node or namespace identity changed between phases")
    runtime = KubernetesRuntime(args.kubeconfig, "kind-" + args.cluster, namespace, current["namespaceUID"],
                                args.node, str(root / "api-bridge"), workload, current["workloadUID"], node_uid=current["nodeUID"])
    image = p["workload"]["image"]
    selector, tolerations = {"kubernetes.io/hostname": args.node}, [{"operator": "Exists"}]
    settings = {"host": host_observer(namespace, image, selector, tolerations), "policy": host_policy(namespace),
                "agent": ephemeral_observer(image, "agent"), "producer": ephemeral_observer(image, "node-context")}
    settings_path = root / "qualification-observer-settings.json"
    if args.phase == "baseline":
        write_new(settings_path, settings)
        install_host(runtime, image, selector, tolerations)
        attach(runtime, image, "agent")
    else:
        require(load(settings_path) == settings, "observation settings changed between phases")
        attach(runtime, image, "node-context", audience=args.audience)
    result = measure_nodes([runtime], p, args.phase)
    validate_window(p, result, args.phase)
    write_new(args.output, result)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("cluster", "node", "kubeconfig", "profile", "output", "audience"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--phase", choices=("baseline", "enabled"), required=True)
    try:
        measure(parser.parse_args())
        return 0
    except ContractError as error:
        print(f"Kubernetes measurement failed: {error}", file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"Kubernetes measurement failed ({type(error).__name__})", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
