#!/usr/bin/env python3

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parent
PROFILES = ROOT.parent / "provider-profiles"
sys.path.insert(0, str(PROFILES))

from profile_contract import EvaluationInputError, load_json, validate_profile  # noqa: E402
from managed_binding import verify_managed_binding  # noqa: E402


GKE_PROFILES = {
    "gke-cos-containerd-amd64": "COS_CONTAINERD",
    "gke-ubuntu-containerd-amd64": "UBUNTU_CONTAINERD",
}
EKS_PROFILES = {"eks-al2023-containerd-amd64"}
AKS_PROFILES = {"aks-ubuntu-containerd-amd64"}
SELF_MANAGED_PROFILES = {"self-managed-containerd", "self-managed-crio-amd64"}
SUPPORTED_PROFILES = set(GKE_PROFILES) | EKS_PROFILES | AKS_PROFILES | SELF_MANAGED_PROFILES
RECEIPT_KEYS = {"schemaVersion", "profile", "observedAt", "provider", "nodeImage", "cniName",
                "controlPlaneVersion", "proofSource", "providerChecks", "receiptDigest"}
CHECK_KEYS = {"profileCanonical", "providerMode", "nodeImage", "cni", "controlPlaneVersion",
              "contextBinding", "nodePoolBinding"}
PROOF_SOURCES = {"gcloud:control-plane+node-pool", "aws:control-plane+nodegroup+vpc-cni-addon",
                 "az:control-plane+node-pool", "kubectl:version+nodes+daemonsets",
                 "bounded-kubernetes-inventory"}
SELECTOR_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._() -]{0,127}")
DIGEST_PATTERN = re.compile(r"sha256:[a-f0-9]{64}")
TIMESTAMP_PATTERN = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z")
TEXT_PATTERN = re.compile(r"[A-Za-z0-9][-A-Za-z0-9 ._()+/@:=]{0,159}")
MAX_COMMAND_OUTPUT_BYTES = 2 * 1024 * 1024


class ReceiptError(ValueError):
    pass


def canonical_digest(value, digest_field):
    content = dict(value)
    content.pop(digest_field, None)
    encoded = json.dumps(content, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()
    return "sha256:" + hashlib.sha256(encoded).hexdigest()


def require_object(value, name):
    if not isinstance(value, dict):
        raise ReceiptError(f"{name} must be a JSON object")
    return value


def require_text(value, name):
    if not isinstance(value, str) or TEXT_PATTERN.fullmatch(value) is None:
        raise ReceiptError(f"{name} is missing or unsafe")
    return value


def require_env(name, environment):
    value = environment.get(name, "")
    if SELECTOR_PATTERN.fullmatch(value) is None:
        raise ReceiptError(f"{name} is required and must be a bounded resource selector")
    return value


def run_json(command, runner=subprocess.run):
    try:
        result = runner(command, check=False, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ReceiptError(f"{command[0]} inventory command did not complete") from error
    if result.returncode != 0:
        raise ReceiptError(f"{command[0]} inventory command failed")
    if not isinstance(result.stdout, str):
        raise ReceiptError(f"{command[0]} inventory response was not text")
    if len(result.stdout.encode()) > MAX_COMMAND_OUTPUT_BYTES:
        raise ReceiptError(f"{command[0]} inventory response exceeded the size limit")
    try:
        return require_object(json.loads(result.stdout), f"{command[0]} inventory response")
    except json.JSONDecodeError as error:
        raise ReceiptError(f"{command[0]} inventory response was not JSON") from error


def load_canonical_profile(path):
    try:
        profile = load_json(path)
        validate_profile(profile)
    except EvaluationInputError as error:
        raise ReceiptError(str(error)) from error
    profile_id = profile["id"]
    if profile_id not in SUPPORTED_PROFILES:
        raise ReceiptError("the selected profile has no provider inventory driver")
    canonical_path = PROFILES / f"{profile_id}.json"
    try:
        canonical = load_json(canonical_path)
        validate_profile(canonical)
    except EvaluationInputError as error:
        raise ReceiptError("the checked-in canonical profile is invalid") from error
    if profile != canonical:
        raise ReceiptError("the selected profile does not match its checked-in canonical content")
    return profile


def cni_from_gke(cluster):
    network = require_object(cluster.get("networkConfig", {}), "GKE networkConfig")
    if network.get("datapathProvider") == "ADVANCED_DATAPATH":
        return "GKE Dataplane V2"
    policy = require_object(cluster.get("networkPolicy", {}), "GKE networkPolicy")
    if policy.get("enabled") is True and policy.get("provider") == "CALICO":
        return "GKE Calico"
    raise ReceiptError("GKE cluster does not report an enforcing supported CNI")


def collect_gke(profile, environment, runner):
    profile_id = profile["id"]
    project = require_env("QUALIFY_GKE_PROJECT", environment)
    location = require_env("QUALIFY_GKE_LOCATION", environment)
    cluster_name = require_env("QUALIFY_GKE_CLUSTER", environment)
    pool_name = require_env("QUALIFY_GKE_NODE_POOL", environment)
    common = ["--project", project, "--location", location, "--format=json"]
    cluster = run_json(["gcloud", "container", "clusters", "describe", cluster_name, *common], runner)
    pool = run_json([
        "gcloud", "container", "node-pools", "describe", pool_name,
        "--cluster", cluster_name, *common,
    ], runner)
    autopilot = require_object(cluster.get("autopilot", {}), "GKE autopilot")
    if autopilot.get("enabled", False) is not False or cluster.get("status") != "RUNNING":
        raise ReceiptError("GKE profile requires a running Standard cluster")
    control_plane = require_text(cluster.get("currentMasterVersion"), "GKE control-plane version")
    config = require_object(pool.get("config", {}), "GKE node-pool config")
    image_type = require_text(config.get("imageType"), "GKE node image")
    if image_type != GKE_PROFILES[profile_id] or pool.get("status") != "RUNNING":
        raise ReceiptError("GKE node pool does not match the selected image family")
    verify_managed_binding(
        "gke", environment, runner, cluster, pool_name, profile["expectations"], ReceiptError,
    )
    return "gke-standard", image_type, cni_from_gke(cluster), control_plane, \
        "gcloud:control-plane+node-pool"


def addon_configuration(addon):
    raw = addon.get("configurationValues", "{}")
    if isinstance(raw, str):
        try:
            return require_object(json.loads(raw or "{}"), "EKS VPC CNI configuration")
        except json.JSONDecodeError as error:
            raise ReceiptError("EKS VPC CNI configuration is not JSON") from error
    return require_object(raw, "EKS VPC CNI configuration")


def collect_eks(profile, environment, runner):
    profile_id = profile["id"]
    region = require_env("QUALIFY_EKS_REGION", environment)
    cluster_name = require_env("QUALIFY_EKS_CLUSTER", environment)
    group_name = require_env("QUALIFY_EKS_NODEGROUP", environment)
    common = ["--region", region, "--output", "json"]
    cluster = require_object(run_json([
        "aws", "eks", "describe-cluster", "--name", cluster_name, *common,
    ], runner).get("cluster"), "EKS cluster")
    nodegroup = require_object(run_json([
        "aws", "eks", "describe-nodegroup", "--cluster-name", cluster_name,
        "--nodegroup-name", group_name, *common,
    ], runner).get("nodegroup"), "EKS node group")
    addon = require_object(run_json([
        "aws", "eks", "describe-addon", "--cluster-name", cluster_name,
        "--addon-name", "vpc-cni", *common,
    ], runner).get("addon"), "EKS VPC CNI add-on")
    if cluster.get("status") != "ACTIVE" or nodegroup.get("status") != "ACTIVE":
        raise ReceiptError("EKS profile requires an active managed cluster and node group")
    image_type = require_text(nodegroup.get("amiType"), "EKS AMI type")
    release = require_text(nodegroup.get("releaseVersion"), "EKS AMI release")
    if profile_id not in EKS_PROFILES or image_type != "AL2023_x86_64_STANDARD":
        raise ReceiptError("EKS node group is not the claimed AL2023 managed row")
    if addon.get("addonName") != "vpc-cni" or addon.get("status") != "ACTIVE":
        raise ReceiptError("EKS VPC CNI add-on is not active")
    enabled = addon_configuration(addon).get("enableNetworkPolicy")
    if enabled not in (True, "true"):
        raise ReceiptError("EKS VPC CNI network policy is not enabled")
    addon_version = require_text(addon.get("addonVersion"), "EKS VPC CNI version")
    control_plane = require_text(cluster.get("version"), "EKS control-plane version")
    cni = f"Amazon VPC CNI {addon_version} network-policy=enabled"
    verify_managed_binding(
        "eks", environment, runner, cluster, group_name, profile["expectations"], ReceiptError,
    )
    return "eks-managed-nodes", f"{image_type}@{release}", cni, control_plane, \
        "aws:control-plane+nodegroup+vpc-cni-addon"


def collect_aks(profile, environment, runner):
    profile_id = profile["id"]
    subscription = require_env("QUALIFY_AKS_SUBSCRIPTION", environment)
    resource_group = require_env("QUALIFY_AKS_RESOURCE_GROUP", environment)
    cluster_name = require_env("QUALIFY_AKS_CLUSTER", environment)
    pool_name = require_env("QUALIFY_AKS_NODE_POOL", environment)
    common = ["--subscription", subscription, "--resource-group", resource_group, "--output", "json"]
    cluster = run_json(["az", "aks", "show", "--name", cluster_name, *common], runner)
    pool = run_json([
        "az", "aks", "nodepool", "show", "--cluster-name", cluster_name,
        "--name", pool_name, *common,
    ], runner)
    power = require_object(cluster.get("powerState", {}), "AKS power state")
    if cluster.get("provisioningState") != "Succeeded" or power.get("code") != "Running":
        raise ReceiptError("AKS profile requires a running managed cluster")
    if pool.get("provisioningState") != "Succeeded" or pool.get("osType") != "Linux" \
            or pool.get("osSku") != "Ubuntu" or pool.get("type") != "VirtualMachineScaleSets":
        raise ReceiptError("AKS node pool is not the claimed managed Ubuntu Linux row")
    network = require_object(cluster.get("networkProfile", {}), "AKS network profile")
    policy = network.get("networkPolicy")
    if network.get("networkPlugin") != "azure" or policy not in {"azure", "calico", "cilium"}:
        raise ReceiptError("AKS cluster does not report an enforcing Azure CNI profile")
    dataplane = network.get("networkDataplane")
    cni = "Azure CNI"
    if dataplane == "cilium" or policy == "cilium":
        cni = "Azure CNI Cilium"
    elif policy == "calico":
        cni = "Azure CNI Calico"
    node_image = require_text(pool.get("nodeImageVersion"), "AKS node image")
    control_plane = require_text(cluster.get("currentKubernetesVersion"), "AKS current control-plane version")
    verify_managed_binding(
        "aks", environment, runner, cluster, pool_name, profile["expectations"], ReceiptError,
    )
    return "aks-node-pools", node_image, cni, control_plane, "az:control-plane+node-pool"


def image_version(image, marker):
    leaf = image.rsplit("/", 1)[-1]
    if not leaf.startswith(marker + ":"):
        return None
    return leaf.split(":", 1)[1].split("@", 1)[0]


def cni_from_daemonsets(daemonsets):
    candidates = []
    for item in daemonsets.get("items", []):
        name = item.get("metadata", {}).get("name", "").casefold()
        images = [entry.get("image", "") for entry in item.get("spec", {}).get("template", {})
                  .get("spec", {}).get("containers", [])]
        definitions = (("cilium", "cilium", "Cilium"), ("calico-node", "node", "Calico"),
                       ("antrea-agent", "antrea", "Antrea"))
        for name_marker, image_marker, display in definitions:
            if name_marker not in name:
                continue
            versions = [image_version(image, image_marker) for image in images]
            versions = [version for version in versions if version]
            if len(set(versions)) != 1:
                raise ReceiptError(f"{display} DaemonSet does not expose one bounded image version")
            candidates.append(f"{display} {versions[0]}")
    if len(candidates) != 1:
        raise ReceiptError("self-managed inventory must contain one supported CNI DaemonSet")
    return candidates[0]


def collect_self_managed(profile_id, environment, runner):
    context = require_env("QUALIFY_CONTEXT", environment)
    base = ["kubectl", "--context", context]
    inventory = {
        "version": run_json([*base, "version", "-o", "json"], runner),
        "nodes": run_json([*base, "get", "nodes", "-o", "json"], runner),
        "daemonsets": run_json([*base, "get", "daemonsets", "-n", "kube-system", "-o", "json"], runner),
    }
    nodes = require_object(inventory.get("nodes"), "self-managed nodes")
    linux = [item for item in nodes.get("items", [])
             if item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux"]
    if not linux:
        raise ReceiptError("self-managed inventory has no Linux nodes")
    infos = [require_object(item.get("status", {}).get("nodeInfo"), "self-managed nodeInfo") for item in linux]
    managed_labels = {
        "cloud.google.com/gke-nodepool", "eks.amazonaws.com/nodegroup",
        "eks.amazonaws.com/compute-type", "kubernetes.azure.com/agentpool",
        "kubernetes.azure.com/cluster",
    }
    for item in nodes.get("items", []):
        metadata = item.get("metadata", {})
        labels = metadata.get("labels", {}) if isinstance(metadata, dict) else {}
        provider_id = item.get("spec", {}).get("providerID", "")
        if any(key in labels for key in managed_labels) or provider_id.startswith(("gce://", "aws://", "azure://")):
            raise ReceiptError("self-managed inventory contains managed-provider identity")
    runtimes = {info.get("containerRuntimeVersion") for info in infos}
    images = {info.get("osImage") for info in infos}
    architectures = {info.get("architecture") for info in infos}
    expected_runtime = "containerd://" if profile_id == "self-managed-containerd" else "cri-o://"
    if len(runtimes) != 1 or not next(iter(runtimes), "").startswith(expected_runtime):
        raise ReceiptError("self-managed runtime does not match the selected profile")
    if len(images) != 1 or len(architectures) != 1:
        raise ReceiptError("self-managed Linux nodes do not form one inventory row")
    architecture = next(iter(architectures))
    if profile_id == "self-managed-crio-amd64" and architecture != "amd64":
        raise ReceiptError("self-managed CRI-O profile requires amd64")
    version = require_object(inventory.get("version"), "self-managed Kubernetes version")
    server = require_object(version.get("serverVersion"), "self-managed serverVersion")
    control_plane = require_text(server.get("gitVersion"), "self-managed control-plane version")
    cni = cni_from_daemonsets(require_object(inventory.get("daemonsets"), "self-managed DaemonSets"))
    node_image = require_text(next(iter(images)), "self-managed node image").replace("/", " ")
    return "self-managed", node_image, cni, control_plane, "kubectl:version+nodes+daemonsets"


def validate_receipt(receipt):
    if not isinstance(receipt, dict) or set(receipt) != RECEIPT_KEYS or receipt.get("schemaVersion") != 1:
        raise ReceiptError("receipt fields do not match schema version 1")
    profile = receipt["profile"]
    valid_profile = isinstance(profile, dict) and set(profile) == {"id", "digest"}
    valid_profile = valid_profile and profile["id"] in SUPPORTED_PROFILES
    valid_profile = valid_profile and isinstance(profile["digest"], str) \
        and DIGEST_PATTERN.fullmatch(profile["digest"]) is not None
    if not valid_profile:
        raise ReceiptError("receipt profile identity is invalid")
    if not isinstance(receipt["observedAt"], str) or TIMESTAMP_PATTERN.fullmatch(receipt["observedAt"]) is None:
        raise ReceiptError("receipt observedAt must use UTC second precision")
    for name in ("provider", "nodeImage", "cniName", "controlPlaneVersion"):
        require_text(receipt[name], f"receipt {name}")
    if receipt["proofSource"] not in PROOF_SOURCES:
        raise ReceiptError("receipt proofSource is invalid")
    checks = receipt["providerChecks"]
    if not isinstance(checks, dict) or set(checks) != CHECK_KEYS or any(value is not True for value in checks.values()):
        raise ReceiptError("receipt providerChecks must all be true")
    if not isinstance(receipt["receiptDigest"], str) \
            or DIGEST_PATTERN.fullmatch(receipt["receiptDigest"]) is None \
            or receipt["receiptDigest"] != canonical_digest(receipt, "receiptDigest"):
        raise ReceiptError("receiptDigest does not match canonical receipt content")


def collect_receipt(profile, environment, runner=subprocess.run, observed_at=None):
    profile_id = profile["id"]
    if profile_id in GKE_PROFILES:
        values = collect_gke(profile, environment, runner)
    elif profile_id in EKS_PROFILES:
        values = collect_eks(profile, environment, runner)
    elif profile_id in AKS_PROFILES:
        values = collect_aks(profile, environment, runner)
    else:
        values = collect_self_managed(profile_id, environment, runner)
    provider, node_image, cni, control_plane, proof = values
    expected = profile["expectations"]
    for name, value, pattern in (
        ("provider", provider, expected["providerPattern"]),
        ("node image", node_image, expected["nodeImagePattern"]),
        ("CNI", cni, expected["cniPattern"]),
    ):
        if re.fullmatch(pattern, value) is None:
            raise ReceiptError(f"observed {name} does not match the selected canonical profile")
    receipt = {
        "schemaVersion": 1,
        "profile": {"id": profile_id, "digest": profile["profileDigest"]},
        "observedAt": observed_at or datetime.now(timezone.utc).replace(microsecond=0).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "provider": provider,
        "nodeImage": node_image,
        "cniName": cni,
        "controlPlaneVersion": control_plane,
        "proofSource": proof,
        "providerChecks": dict.fromkeys(CHECK_KEYS, True),
    }
    receipt["receiptDigest"] = canonical_digest(receipt, "receiptDigest")
    validate_receipt(receipt)
    return receipt


def main():
    parser = argparse.ArgumentParser(description="Collect a sanitised provider-owned inventory receipt.")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--output", help="New receipt file. Defaults to standard output.")
    arguments = parser.parse_args()
    try:
        profile = load_canonical_profile(arguments.profile)
        receipt = collect_receipt(profile, os.environ)
        encoded = json.dumps(receipt, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
        if arguments.output:
            output = Path(arguments.output)
            output.parent.mkdir(parents=True, exist_ok=True)
            try:
                descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            except FileExistsError as error:
                raise ReceiptError("refusing to overwrite the receipt output") from error
            with os.fdopen(descriptor, "w", encoding="utf-8") as destination:
                destination.write(encoded)
        else:
            print(encoded, end="")
    except (EvaluationInputError, ReceiptError) as error:
        print(f"provider inventory error: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
