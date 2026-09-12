"""Exercise the provider coordinator components on an owned two-Node kind fixture."""

import argparse
import json
import sys
from pathlib import Path
from types import SimpleNamespace

from check_kubernetes_observer import verify_kind_target
from common import load, privacy, require, write_new
from kubernetes_commands import KubernetesCommands
from measurement_checks import measurement_checks
from network_probes import NetworkChecks
from observer_specs import host_observer, host_policy
from provider_execution import Execution
from provider_recovery import PoolRecovery
from source_summary import summarise
from workload import deployment


def prepare(args):
    profile = load(args.profile)
    require(profile["id"] in {"kind-137-execution", "kind-136-network"} and profile["profileClass"] == "local-kind"
            and profile["workload"]["linuxNodes"] == 2, "owned two-Node diagnostic profile required")
    verify_kind_target(args.kubeconfig, args.context)
    root, output = Path(args.private), Path(args.output)
    require(not output.exists(), "execution evidence already exists")
    k = KubernetesCommands(args.kubeconfig, args.context)
    nodes = json.loads(k("get", "nodes", "-o", "json"))["items"]
    require(len(nodes) == 2, "local execution requires exactly two Nodes")
    bindings = []
    for node in nodes:
        addresses = [a["address"] for a in node["status"]["addresses"] if a["type"] == "InternalIP" and ":" not in a["address"]]
        require(len(addresses) == 1, "local fixture requires one IPv4 Node address")
        bindings.append({"name": node["metadata"]["name"], "uid": node["metadata"]["uid"], "route": addresses[0] + "/32",
                         "providerID": node["spec"].get("providerID", "")})
    infos = [{"kubernetes": n["status"]["nodeInfo"]["kubeletVersion"], "kernel": n["status"]["nodeInfo"]["kernelVersion"],
              "runtime": n["status"]["nodeInfo"]["containerRuntimeVersion"], "osImage": n["status"]["nodeInfo"]["osImage"],
              "architecture": n["status"]["nodeInfo"]["architecture"]} for n in nodes]
    require(infos[0] == infos[1], "local Nodes differ in runtime")
    binding = {"nodes": sorted(bindings, key=lambda n: n["name"]), "runtime": infos[0]}
    namespace = "kube-memlens-qualification-execution"
    audience = json.loads(k("get", "--raw", "/.well-known/openid-configuration"))["issuer"]
    service = json.loads(k("get", "service", "kubernetes", "-n", "default", "-o", "json"))["spec"]["clusterIP"]
    config = {"namespace": namespace, "kubeconfigPath": args.kubeconfig, "context": args.context,
              "inventoryProfile": "self-managed-containerd", "kubernetesVersion": infos[0]["kubernetes"],
              "chartArchive": str(root / "chart.tgz"), "imageRepository": args.image_repository,
              "imageDigest": args.image_digest, "kubeletAudience": audience}
    proposal = root / "local-proposal"; proposal.mkdir(mode=0o700)
    selector, tolerations = {"kubernetes.io/os": "linux"}, [{"operator": "Exists"}]
    for phase in ("baseline", "enabled"):
        values = {"namespace": {"name": namespace}, "image": {"repository": args.image_repository, "digest": args.image_digest},
                  "agent": {"nodeSelector": selector, "tolerations": tolerations, "tokenExpirationSeconds": 600},
                  "collector": {"store": {"maxNodes": 10, "maxContainers": 2000}, "resources": {"limits": {"memory": "512Mi"}}},
                  "nodeContext": {"enabled": phase == "enabled", "kubeletCAConfigMap": "node-context-trust", "kubeletAudience": audience,
                                  "apiServerCIDRs": sorted({service + "/32"} | {n["route"] for n in bindings}),
                                  "nodeCIDRs": [n["route"] for n in bindings]}}
        write_new(proposal / (phase + "-values.json"), values)
    trust = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "node-context-trust", "namespace": namespace},
             "data": {"ca.crt": (root / "serving-ca.crt").read_text()}}
    write_new(proposal / "serving-trust.json", trust)
    write_new(proposal / "workload.json", deployment(profile["workload"], namespace, {"nodeSelector": selector, "tolerations": tolerations}))
    write_new(proposal / "host-observers.json", {"apiVersion": "v1", "kind": "List", "items": [host_policy(namespace), host_observer(namespace, profile["workload"]["image"], selector, tolerations)]})
    bundle = SimpleNamespace(directory=proposal, profile=profile, configuration=config)
    execution = Execution(bundle, binding, k, root, str(root / "chart-inventory"), root / "api-bridge")
    return execution, profile, binding


def run(args):
    execution, profile, binding = prepare(args)
    additional = {}
    try:
        print("local coordinator: preparation and production probes", flush=True)
        execution.prepare()
        execution.measure("baseline")
        print("local coordinator: enable optional producer", flush=True)
        execution.enable()
        execution.measure("enabled")
        measured = execution.measurements()
        recovery = PoolRecovery(execution)
        lifecycle = {}
        for name, observe in (("sourceLoss", recovery.source_loss),
                              ("agentRestart", lambda: recovery.restart("agent")),
                              ("collectorRestart", lambda: recovery.restart("collector"))):
            print("local coordinator: " + name, flush=True)
            lifecycle[name] = observe()
            require(lifecycle[name]["state"] == "passed", "local pool recovery failed: " + name)
        if profile["id"] == "kind-136-network":
            print("local coordinator: network policy controls", flush=True)
            network = NetworkChecks(execution).run()
            print("local network policy: " + json.dumps(network), flush=True)
            require(network["passed"], "local network policy controls failed")
            additional = {"networkPolicy": network, "cni": load(Path(args.private) / "cni.json")}
    finally:
        print("local coordinator: UID-owned resource cleanup", flush=True)
        execution.cleanup()
    checks = [measurement_checks(profile, n["samples"], n["rotation"], 1) for n in measured["nodes"]]
    passed = all(c["passed"] for groups in checks for group in groups.values() for c in group)
    result = {"schemaVersion": 1, "scope": "local-provider-coordinator-diagnostic", "qualified": False,
              "profile": {"id": profile["id"], "digest": profile["profileDigest"]}, "linuxNodes": 2,
              "positiveTransportProbes": len(execution.observations), "negativeTransportProbes": 6,
              "measurementChecksPassed": passed, "cleanup": "passed", "measurements": measured,
              "lifecycle": lifecycle,
              **summarise(execution.observations, [n["name"] for n in binding["nodes"]]), **additional}
    privacy(result)
    write_new(args.output, result)
    require(passed, "local pool measurement checks failed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("kubeconfig", "context", "private", "output", "profile", "image-repository", "image-digest"):
        parser.add_argument("--" + name, required=True)
    run(parser.parse_args())


if __name__ == "__main__":
    main()
