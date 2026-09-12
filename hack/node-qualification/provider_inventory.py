"""Read fresh provider inventory and bind it to the approved exact Node pool."""

import ipaddress
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlsplit

from common import digest, load, privacy, require, utc_text
from prepare_provider import REPOSITORY
from process import execute
from provider_plan import POOL_LABELS

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "provider-inventory"))
from collect import CHECK_KEYS, ReceiptError, collect_aks, collect_eks, collect_gke, collect_self_managed, validate_receipt  # noqa: E402
from profile_contract import validate_profile as validate_inventory_profile  # noqa: E402

ENVIRONMENT_KEYS = {
    "gke-standard": {"project": "QUALIFY_GKE_PROJECT", "location": "QUALIFY_GKE_LOCATION", "cluster": "QUALIFY_GKE_CLUSTER"},
    "eks-managed-nodes": {"region": "QUALIFY_EKS_REGION", "cluster": "QUALIFY_EKS_CLUSTER"},
    "aks-node-pools": {"subscription": "QUALIFY_AKS_SUBSCRIPTION", "resourceGroup": "QUALIFY_AKS_RESOURCE_GROUP", "cluster": "QUALIFY_AKS_CLUSTER"},
    "self-managed": {},
}
POOL_ENV = {"gke-standard": "QUALIFY_GKE_NODE_POOL", "eks-managed-nodes": "QUALIFY_EKS_NODEGROUP", "aks-node-pools": "QUALIFY_AKS_NODE_POOL"}


def provider_environment(profile, config):
    environment = dict(os.environ, KUBECONFIG=config["kubeconfigPath"], QUALIFY_CONTEXT=config["context"])
    for key, name in ENVIRONMENT_KEYS[profile["provider"]].items():
        environment[name] = config["providerSelectors"][key]
    if profile["provider"] in POOL_ENV:
        environment[POOL_ENV[profile["provider"]]] = config["poolName"]
    return environment


def collect_inventory(bundle, run=execute):
    p, c = bundle.profile, bundle.configuration
    inventory = load(REPOSITORY / "hack/provider-profiles" / (c["inventoryProfile"] + ".json"))
    validate_inventory_profile(inventory)
    environment = provider_environment(p, c)

    def reader(args, **kwargs):
        if args[0] == "kubectl":
            args = [args[0], "--kubeconfig", c["kubeconfigPath"], "--request-timeout=10s"] + args[1:]
        output = run(args, timeout=30, maximum=2 * 1024 * 1024, environment=environment)
        return subprocess.CompletedProcess(args, 0, stdout=output, stderr="")

    local_config = json.loads(reader(["kubectl", "--context", c["context"], "config", "view", "--minify", "-o", "json"]).stdout)
    clusters = local_config.get("clusters", [])
    require(len(clusters) == 1, "explicit context did not select one API endpoint")
    cluster = clusters[0]["cluster"]
    endpoint = urlsplit(cluster.get("server", ""))
    require(endpoint.scheme == "https" and endpoint.hostname and endpoint.username is None
            and endpoint.password is None and not endpoint.query and not endpoint.fragment
            and cluster.get("insecure-skip-tls-verify", False) is False, "verified Kubernetes API TLS is required")
    provider = p["provider"]
    if provider == "gke-standard":
        observed = collect_gke(inventory, environment, reader)
    elif provider == "eks-managed-nodes":
        observed = collect_eks(inventory, environment, reader)
    elif provider == "aks-node-pools":
        # The older cgroup profile's unsupported status remains unchanged. This
        # gathers current inventory only; it never grants Node-context support.
        observed = collect_aks(inventory, environment, reader, expected_nodes=p["workload"]["linuxNodes"])
    else:
        observed = collect_self_managed(c["inventoryProfile"], environment, reader)
    provider, image, cni, version, proof = observed
    for value, field in ((provider, "providerPattern"), (image, "nodeImagePattern"), (cni, "cniPattern")):
        require(re.fullmatch(inventory["expectations"][field], value), "provider inventory differs from its canonical profile")
    receipt = {"schemaVersion": 2, "profile": {"id": inventory["id"], "digest": inventory["profileDigest"]},
               "observedAt": utc_text(), "qualificationToolCommit": bundle.plan["qualificationToolCommit"],
               "provider": provider, "nodeImage": image, "cniName": cni, "controlPlaneVersion": version,
               "proofSource": proof, "providerChecks": dict.fromkeys(CHECK_KEYS, True)}
    receipt["receiptDigest"] = digest(receipt, "receiptDigest")
    validate_receipt(receipt); privacy(receipt)
    nodes = json.loads(reader(["kubectl", "--context", c["context"], "get", "nodes", "-o", "json"]).stdout)
    return receipt, bind_nodes(p, c, inventory, receipt, nodes)


def bind_nodes(profile, config, inventory, receipt, document):
    nodes = document.get("items")
    require(isinstance(nodes, list) and len(nodes) <= 128, "invalid live Node inventory")
    linux = [n for n in nodes if n.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux"]
    require(len(linux) == profile["workload"]["linuxNodes"], "live Linux Node count differs from the profile")
    label = POOL_LABELS.get(profile["provider"])
    tuples, bindings = set(), []
    for node in linux:
        labels = node["metadata"].get("labels", {})
        require(label is None or labels.get(label) == config["poolName"], "a Linux Node is outside the approved pool")
        require(not node["metadata"].get("deletionTimestamp") and not node["spec"].get("unschedulable")
                and any(c.get("type") == "Ready" and c.get("status") == "True" for c in node["status"].get("conditions", [])),
                "selected Node is not ready and schedulable")
        require(not any(t.get("effect") in {"NoSchedule", "NoExecute"} for t in node["spec"].get("taints", [])),
                "selected Node requires an unapproved scheduling toleration")
        info = node["status"]["nodeInfo"]
        for key, pattern in (("osImage", "osImagePattern"), ("containerRuntimeVersion", "runtimePattern"), ("architecture", "architecturePattern")):
            require(re.fullmatch(inventory["expectations"][pattern], info.get(key, "")), "Node runtime differs from the inventory profile")
        require(info["kubeletVersion"].removeprefix("v") == config["kubernetesVersion"].removeprefix("v"),
                "Node Kubernetes version differs from the approved configuration")
        tuples.add(tuple(info[k] for k in ("kubeletVersion", "kernelVersion", "containerRuntimeVersion", "osImage", "architecture")))
        addresses = {str(ipaddress.ip_address(a["address"])) for a in node["status"].get("addresses", []) if a["type"] == "InternalIP"}
        selected = [route for route in config["nodeCIDRs"] if str(ipaddress.ip_network(route).network_address) in addresses]
        require(len(selected) == 1, "Node does not match exactly one approved host route")
        bindings.append({"name": node["metadata"]["name"], "uid": node["metadata"]["uid"],
                         "route": selected[0], "providerID": node["spec"].get("providerID", "")})
    require(len(tuples) == 1, "selected Nodes do not form one exact runtime profile")
    for field in ("name", "uid", "route"):
        require(len({b[field] for b in bindings}) == len(bindings), "Node binding is duplicated")
    prefixes = {"gke-standard": "gce://", "eks-managed-nodes": "aws://", "aks-node-pools": "azure://"}
    if profile["provider"] in prefixes:
        require(all(b["providerID"].startswith(prefixes[profile["provider"]]) for b in bindings)
                and len({b["providerID"] for b in bindings}) == len(bindings), "managed Node instance identity is missing or duplicated")
    live = config["kubernetesVersion"].removeprefix("v")
    control = receipt["controlPlaneVersion"].removeprefix("v")
    require(live == control or profile["provider"] == "eks-managed-nodes" and live.startswith(control + "."),
            "provider control plane differs from the approved Kubernetes version")
    return {"nodes": sorted(bindings, key=lambda n: n["name"]), "runtime": dict(zip(
        ("kubernetes", "kernel", "runtime", "osImage", "architecture"), next(iter(tuples))))}
