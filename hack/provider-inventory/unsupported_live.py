#!/usr/bin/env python3

import ipaddress
import re
import stat
import subprocess
from pathlib import Path
from urllib.parse import urlsplit

from collect import (
    MAX_COMMAND_OUTPUT_BYTES,
    ReceiptError,
    cni_from_daemonsets,
    require_env,
    require_object,
    require_text,
    run_json,
)
from kubectl_dry_run import post_server_dry_run


CONTEXT_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/@=+-]{0,255}")
NAMESPACE_PATTERN = re.compile(r"[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?")
SSH_USER_PATTERN = re.compile(r"[a-z_][a-z0-9_-]{0,31}")
HOSTNAME_PATTERN = re.compile(
    r"(?=.{1,253}\Z)(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)*"
    r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?"
)
LABEL_VALUE_PATTERN = re.compile(r"[A-Za-z0-9](?:[-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?")
MAX_LINUX_NODES = 100
ENVIRONMENT_HELP = """Required environment by profile:
  all: QUALIFY_CONTEXT
  gke-autopilot: QUALIFY_GKE_PROJECT, QUALIFY_GKE_LOCATION, QUALIFY_GKE_CLUSTER,
    QUALIFY_OBSERVATION_NAMESPACE
  eks-fargate: QUALIFY_EKS_REGION, QUALIFY_EKS_CLUSTER, QUALIFY_EKS_FARGATE_PROFILE
  aks-virtual-nodes: QUALIFY_AKS_SUBSCRIPTION, QUALIFY_AKS_RESOURCE_GROUP, QUALIFY_AKS_CLUSTER
  windows-deep-mode: the AKS variables above plus QUALIFY_AKS_WINDOWS_NODE_POOL
  cgroup-v1: QUALIFY_SSH_USER, QUALIFY_SSH_ADDRESS_TYPE, QUALIFY_SSH_KEY,
    QUALIFY_SSH_KNOWN_HOSTS
"""
CGROUP_V1_COMMAND = (
    "test ! -e /sys/fs/cgroup/cgroup.controllers && "
    "awk '$5 ~ \"^/sys/fs/cgroup(/|$)\" {for (i=6;i<=NF;i++) "
    "if ($i == \"-\" && $(i+1) == \"cgroup\") found=1} END {exit !found}' "
    "/proc/self/mountinfo && printf 'cgroup-v1\\n'"
)


def _run(command, runner, input_text=None):
    try:
        result = runner(
            command, input=input_text, check=False, capture_output=True, text=True, timeout=30,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ReceiptError(f"{command[0]} observation command did not complete") from error
    if any(not isinstance(value, str) or len(value.encode()) > MAX_COMMAND_OUTPUT_BYTES
           for value in (result.stdout, result.stderr)):
        raise ReceiptError(f"{command[0]} observation response is invalid")
    return result


def _run_text(command, runner, input_text=None):
    result = _run(command, runner, input_text)
    if result.returncode != 0:
        raise ReceiptError(f"{command[0]} observation command failed")
    return result.stdout


def _context(environment):
    value = environment.get("QUALIFY_CONTEXT", "")
    if not isinstance(value, str) or CONTEXT_PATTERN.fullmatch(value) is None:
        raise ReceiptError("QUALIFY_CONTEXT is required and must be bounded")
    return value


def _kubectl(environment):
    return ["kubectl", "--context", _context(environment)]


def _host(value):
    if not isinstance(value, str) or not value:
        return None
    parsed = urlsplit(value if "://" in value else "https://" + value)
    if parsed.scheme != "https" or parsed.username or parsed.password or parsed.query or parsed.fragment:
        return None
    return parsed.hostname.casefold().rstrip(".") if parsed.hostname else None


def _bind_context(base, provider_endpoints, runner):
    config = run_json([*base, "config", "view", "--minify", "-o", "json"], runner)
    clusters = config.get("clusters")
    if not isinstance(clusters, list) or len(clusters) != 1:
        raise ReceiptError("kubectl context did not expose one cluster")
    cluster = clusters[0].get("cluster", {}) if isinstance(clusters[0], dict) else {}
    live = _host(cluster.get("server")) if isinstance(cluster, dict) else None
    expected = {_host(value) for value in provider_endpoints if _host(value)}
    if not live or live not in expected:
        raise ReceiptError("live Kubernetes context does not match the provider cluster endpoint")


def _live_kubernetes(base, runner, selector=None):
    version = run_json([*base, "version", "-o", "json"], runner)
    command = [*base, "get", "nodes"]
    if selector:
        command.extend(["-l", selector])
    nodes = run_json([*command, "-o", "json"], runner)
    return version, nodes


def collect_gke(environment, probe, runner, dry_run):
    project = require_env("QUALIFY_GKE_PROJECT", environment)
    location = require_env("QUALIFY_GKE_LOCATION", environment)
    cluster_name = require_env("QUALIFY_GKE_CLUSTER", environment)
    namespace = environment.get("QUALIFY_OBSERVATION_NAMESPACE", "")
    if not isinstance(namespace, str) or NAMESPACE_PATTERN.fullmatch(namespace) is None:
        raise ReceiptError("QUALIFY_OBSERVATION_NAMESPACE must be an existing DNS-label namespace")
    provider = run_json([
        "gcloud", "container", "clusters", "describe", cluster_name,
        "--project", project, "--location", location, "--format=json",
    ], runner)
    base = _kubectl(environment)
    endpoints = [provider.get("endpoint")]
    dns = provider.get("controlPlaneEndpointsConfig", {}).get("dnsEndpointConfig", {})
    private = provider.get("privateClusterConfig", {})
    endpoints.extend([dns.get("endpoint"), private.get("privateEndpoint")])
    _bind_context(base, endpoints, runner)
    if _run_text([*base, "auth", "can-i", "create", "pods", "-n", namespace], runner).strip() != "yes":
        raise ReceiptError("current identity cannot run the GKE admission observation")
    baseline_code, baseline = dry_run(base, namespace, probe["baseline"])
    if baseline_code not in {200, 201} or baseline.get("kind") != "Pod":
        raise ReceiptError("GKE baseline server dry-run was not accepted")
    target_code, target = dry_run(base, namespace, probe["target"])
    if target_code != 403 or target.get("kind") != "Status":
        raise ReceiptError("GKE cgroup hostPath server dry-run did not return a forbidden Status")
    version, nodes = _live_kubernetes(base, runner, "kubernetes.io/os=linux")
    return {"schemaVersion": 1, "profileID": "gke-autopilot", "provider": provider,
            "version": version, "nodes": nodes,
            "admission": {"canCreatePods": True, "baselineAccepted": True,
                          "targetStatus": target}}


def collect_eks(environment, runner):
    region = require_env("QUALIFY_EKS_REGION", environment)
    cluster_name = require_env("QUALIFY_EKS_CLUSTER", environment)
    profile_name = require_env("QUALIFY_EKS_FARGATE_PROFILE", environment)
    if LABEL_VALUE_PATTERN.fullmatch(profile_name) is None:
        raise ReceiptError("QUALIFY_EKS_FARGATE_PROFILE must be a Kubernetes label value")
    common = ["--region", region, "--output", "json"]
    cluster = require_object(run_json([
        "aws", "eks", "describe-cluster", "--name", cluster_name, *common,
    ], runner).get("cluster"), "EKS cluster")
    fargate = require_object(run_json([
        "aws", "eks", "describe-fargate-profile", "--cluster-name", cluster_name,
        "--fargate-profile-name", profile_name, *common,
    ], runner).get("fargateProfile"), "EKS Fargate profile")
    base = _kubectl(environment)
    _bind_context(base, [cluster.get("endpoint")], runner)
    pods = run_json([
        *base, "get", "pods", "--all-namespaces", "-l",
        f"eks.amazonaws.com/fargate-profile={profile_name}", "-o", "json",
    ], runner)
    pod_items = pods.get("items", [])
    if not isinstance(pod_items, list) or any(not isinstance(item, dict) for item in pod_items):
        raise ReceiptError("Fargate Pod inventory is invalid")
    subject_nodes = {
        item.get("spec", {}).get("nodeName") for item in pod_items
        if item.get("metadata", {}).get("labels", {}).get("eks.amazonaws.com/fargate-profile") == profile_name
        and item.get("status", {}).get("phase") == "Running"
        and any(condition.get("type") == "Ready" and condition.get("status") == "True"
                for condition in item.get("status", {}).get("conditions", []))
    }
    if not subject_nodes or None in subject_nodes:
        raise ReceiptError("selected Fargate profile has no Ready live subjects")
    version, nodes = _live_kubernetes(base, runner, "eks.amazonaws.com/compute-type=fargate")
    node_items = nodes.get("items", [])
    matched = [item for item in node_items if item.get("metadata", {}).get("name") in subject_nodes]
    if {item.get("metadata", {}).get("name") for item in matched} != subject_nodes:
        raise ReceiptError("selected Fargate subjects do not map to the observed Fargate Nodes")
    nodes = dict(nodes)
    nodes["items"] = matched
    return {"schemaVersion": 1, "profileID": "eks-fargate",
            "provider": {"cluster": cluster, "fargateProfile": fargate},
            "version": version, "nodes": nodes}


def collect_aks_virtual(environment, runner):
    subscription = require_env("QUALIFY_AKS_SUBSCRIPTION", environment)
    group = require_env("QUALIFY_AKS_RESOURCE_GROUP", environment)
    cluster_name = require_env("QUALIFY_AKS_CLUSTER", environment)
    provider = run_json([
        "az", "aks", "show", "--subscription", subscription, "--resource-group", group,
        "--name", cluster_name, "--output", "json",
    ], runner)
    base = _kubectl(environment)
    _bind_context(base, [provider.get("fqdn"), provider.get("privateFqdn")], runner)
    version, nodes = _live_kubernetes(base, runner, "type=virtual-kubelet")
    return {"schemaVersion": 1, "profileID": "aks-virtual-nodes", "provider": provider,
            "version": version, "nodes": nodes}


def _aks_cni(provider):
    network = require_object(provider.get("networkProfile"), "AKS network profile")
    if network.get("networkPlugin") != "azure":
        raise ReceiptError("AKS observation requires Azure CNI")
    if network.get("networkDataplane") == "cilium" or network.get("networkPolicy") != "calico":
        raise ReceiptError("AKS Windows observation requires Azure CNI with Calico")
    return "Azure CNI Calico"


def _chart_contract(repo_root, runner):
    chart = Path(repo_root) / "charts" / "kube-memlens"
    rendered = _run_text([
        "helm", "template", "unsupported-observation", str(chart),
        "--show-only", "templates/daemonset.yaml",
    ], runner)
    linux = re.search(r"nodeSelector:\s+(?:\s*\n)*\s*kubernetes\.io/os:\s*linux", rendered)
    cgroup = re.search(
        r"hostPath:\s*\n\s*path:\s*[\"']?/sys/fs/cgroup[\"']?\s*\n\s*type:\s*Directory",
        rendered,
    )
    volumes = [] if not cgroup else [{"hostPath": {"path": "/sys/fs/cgroup", "type": "Directory"}}]
    selector = {"kubernetes.io/os": "linux"} if linux else {}
    return {"daemonSet": {"spec": {"template": {"spec": {
        "nodeSelector": selector, "volumes": volumes,
    }}}}}


def collect_windows(environment, runner, repo_root):
    subscription = require_env("QUALIFY_AKS_SUBSCRIPTION", environment)
    group = require_env("QUALIFY_AKS_RESOURCE_GROUP", environment)
    cluster_name = require_env("QUALIFY_AKS_CLUSTER", environment)
    pool_name = require_env("QUALIFY_AKS_WINDOWS_NODE_POOL", environment)
    common = ["--subscription", subscription, "--resource-group", group, "--output", "json"]
    provider = run_json(["az", "aks", "show", "--name", cluster_name, *common], runner)
    pool = run_json([
        "az", "aks", "nodepool", "show", "--cluster-name", cluster_name,
        "--name", pool_name, *common,
    ], runner)
    sku = pool.get("osSku")
    if pool.get("provisioningState") != "Succeeded" or pool.get("osType") != "Windows" \
            or not isinstance(sku, str) or re.fullmatch(r"Windows(?:2019|2022|2025)", sku) is None:
        raise ReceiptError("selected AKS node pool is not a current Windows pool")
    base = _kubectl(environment)
    _bind_context(base, [provider.get("fqdn"), provider.get("privateFqdn")], runner)
    version, nodes = _live_kubernetes(base, runner, f"kubernetes.azure.com/agentpool={pool_name}")
    control_plane = provider.get("currentKubernetesVersion")
    proof = {"provider": "aks-node-pools",
             "nodeImage": require_text(pool.get("nodeImageVersion"), "AKS Windows node image"),
             "cniName": _aks_cni(provider), "cniEnforced": False,
             "controlPlaneVersion": require_text(control_plane, "AKS current control-plane version")}
    return {"schemaVersion": 1, "profileID": "windows-deep-mode", "providerProof": proof,
            "version": version, "nodes": nodes, "chart": _chart_contract(repo_root, runner)}


def _secure_file(environment, name):
    raw = environment.get(name, "")
    if not isinstance(raw, str) or not raw or len(raw) > 1024:
        raise ReceiptError(f"{name} is required and must be a bounded file path")
    path = Path(raw)
    try:
        details = path.lstat()
    except OSError as error:
        raise ReceiptError(f"{name} could not be inspected") from error
    if stat.S_ISLNK(details.st_mode) or not stat.S_ISREG(details.st_mode):
        raise ReceiptError(f"{name} must be a direct regular file")
    if name == "QUALIFY_SSH_KEY" and details.st_mode & 0o077:
        raise ReceiptError("QUALIFY_SSH_KEY must not be accessible by group or other users")
    return str(path.resolve())


def _ssh_host(node, address_type):
    addresses = node.get("status", {}).get("addresses", [])
    matches = [item.get("address") for item in addresses if item.get("type") == address_type]
    if len(matches) != 1 or not isinstance(matches[0], str):
        raise ReceiptError(f"each Linux Node must expose one {address_type} SSH address")
    value = matches[0]
    try:
        return str(ipaddress.ip_address(value))
    except ValueError:
        if HOSTNAME_PATTERN.fullmatch(value) is None:
            raise ReceiptError("Node SSH address is invalid")
        return value


def collect_cgroup_v1(environment, runner, repo_root):
    base = _kubectl(environment)
    version, nodes = _live_kubernetes(base, runner)
    daemonsets = run_json([*base, "get", "daemonsets", "-n", "kube-system", "-o", "json"], runner)
    all_items = require_object(nodes, "Kubernetes Nodes").get("items", [])
    if not isinstance(all_items, list) or any(not isinstance(item, dict) for item in all_items):
        raise ReceiptError("Kubernetes Nodes must contain a bounded item list")
    items = [item for item in all_items
             if item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux"]
    if not isinstance(items, list) or not items or len(items) > MAX_LINUX_NODES:
        raise ReceiptError("cgroup v1 observation requires 1 to 100 Linux Nodes")
    managed_labels = {
        "cloud.google.com/gke-nodepool", "eks.amazonaws.com/nodegroup",
        "eks.amazonaws.com/compute-type", "kubernetes.azure.com/agentpool",
        "kubernetes.azure.com/cluster",
    }
    managed_identity = any(
        item.get("spec", {}).get("providerID", "").startswith(("gce://", "aws://", "azure://"))
        or any(key in item.get("metadata", {}).get("labels", {}) for key in managed_labels)
        for item in items
    )
    if managed_identity:
        raise ReceiptError("cgroup v1 observation requires self-managed Linux Nodes")
    user = environment.get("QUALIFY_SSH_USER", "")
    if not isinstance(user, str) or SSH_USER_PATTERN.fullmatch(user) is None:
        raise ReceiptError("QUALIFY_SSH_USER is required and must be a bounded POSIX user")
    address_type = environment.get("QUALIFY_SSH_ADDRESS_TYPE", "")
    if address_type not in {"InternalIP", "ExternalIP", "Hostname"}:
        raise ReceiptError("QUALIFY_SSH_ADDRESS_TYPE must be InternalIP, ExternalIP or Hostname")
    key = _secure_file(environment, "QUALIFY_SSH_KEY")
    known_hosts = _secure_file(environment, "QUALIFY_SSH_KNOWN_HOSTS")
    observations = []
    for node in items:
        uid = node.get("metadata", {}).get("uid")
        if not isinstance(uid, str) or not uid:
            raise ReceiptError("each Linux Node must expose a UID")
        destination = f"{user}@{_ssh_host(node, address_type)}"
        command = [
            "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
            "-o", "ConnectionAttempts=1", "-o", "StrictHostKeyChecking=yes",
            "-o", f"UserKnownHostsFile={known_hosts}", "-o", "GlobalKnownHostsFile=/dev/null",
            "-i", key, destination, "--", "/bin/sh", "-s",
        ]
        if _run_text(command, runner, CGROUP_V1_COMMAND + "\n").strip() != "cgroup-v1":
            raise ReceiptError("SSH host did not return the exact cgroup v1 observation")
        observations.append({"nodeUID": uid, "filesystem": "cgroup", "controllersPresent": False})
    infos = [require_object(item.get("status", {}).get("nodeInfo"), "Linux nodeInfo") for item in items]
    images = {item.get("osImage") for item in infos}
    if len(images) != 1:
        raise ReceiptError("cgroup v1 Linux Nodes must report one node image")
    control_plane = require_text(
        require_object(version.get("serverVersion"), "Kubernetes serverVersion").get("gitVersion"),
        "Kubernetes control-plane version",
    )
    proof = {"provider": "self-managed", "nodeImage": require_text(next(iter(images)), "node image"),
             "cniName": cni_from_daemonsets(daemonsets), "cniEnforced": False,
             "controlPlaneVersion": control_plane}
    return {"schemaVersion": 1, "profileID": "cgroup-v1", "providerProof": proof,
            "version": version, "nodes": nodes, "hostObservations": observations,
            "chart": _chart_contract(repo_root, runner)}


def collect_live_source(profile_id, environment, probe, runner=subprocess.run, repo_root=None,
                        dry_run=post_server_dry_run):
    repo_root = Path(repo_root or Path(__file__).resolve().parents[2])
    if profile_id == "gke-autopilot":
        return collect_gke(environment, probe, runner, dry_run)
    if profile_id == "eks-fargate":
        return collect_eks(environment, runner)
    if profile_id == "aks-virtual-nodes":
        return collect_aks_virtual(environment, runner)
    if profile_id == "windows-deep-mode":
        return collect_windows(environment, runner, repo_root)
    if profile_id == "cgroup-v1":
        return collect_cgroup_v1(environment, runner, repo_root)
    raise ReceiptError("the selected profile has no live unsupported observation driver")
