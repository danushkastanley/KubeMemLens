#!/usr/bin/env python3

import argparse
import copy
import json
import os
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
ROOT = Path(__file__).resolve().parent
PROFILES = ROOT.parent / "provider-profiles"
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(PROFILES))

from collect import (  # noqa: E402
    DIGEST_PATTERN,
    ReceiptError,
    canonical_digest,
    require_object,
    require_text,
)
from privacy_contract import reject_sensitive_content  # noqa: E402
from profile_contract import (  # noqa: E402
    ENVIRONMENT_KEYS,
    EvaluationInputError,
    environment_failures,
    load_json,
    parse_timestamp,
    validate_profile,
)
from unsupported_artifact_binding import verify_binding  # noqa: E402


SPECS = {
    "gke-autopilot": {
        "reasonCode": "hostpath_not_permitted", "method": "provider_and_server_dry_run",
        "state": "cgroup_hostpath_denied", "source": "gcloud:autopilot+dry-run",
        "probe": "baseline-accepted+cgroup-hostpath-denied",
    },
    "eks-fargate": {
        "reasonCode": "daemonset_not_supported", "method": "provider_capability",
        "state": "daemonset_capability_absent", "source": "aws:fargate+nodes",
    },
    "aks-virtual-nodes": {
        "reasonCode": "virtual_nodes_not_supported", "method": "provider_capability",
        "state": "virtual_node_daemonset_absent", "source": "az:virtual-kubelet+nodes",
    },
    "windows-deep-mode": {
        "reasonCode": "windows_nodes_not_supported", "method": "node_and_chart_capability",
        "state": "windows_cgroup_agent_incompatible", "source": "bounded-provider-node-observation",
    },
    "cgroup-v1": {
        "reasonCode": "cgroup_v1_not_supported", "method": "node_host_read",
        "state": "cgroup_v1_observed", "source": "bounded-node-host-observation",
    },
}
RECEIPT_KEYS_V2 = {
    "schemaVersion", "profile", "observedAt", "environment", "controlPlaneVersion",
    "artefacts", "proof", "providerChecks", "unsupportedObservation", "receiptDigest",
}
RECEIPT_KEYS_V3 = RECEIPT_KEYS_V2 | {"qualificationToolCommit"}
ARTEFACT_KEYS = {"sourceCommit", "chartDigest"}
ARTEFACT_BINDING_KEYS_V3 = ARTEFACT_KEYS | {"qualificationToolCommit"}
PROOF_KEYS = {"source", "sourceDigest", "observationSpecDigest"}
PROVIDER_CHECK_KEYS = {"profileCanonical", "providerMode", "nodeImage", "cni", "controlPlaneVersion"}
OBSERVATION_KEYS = {"reasonCode", "method", "state", "subjectCount", "checks"}
OBSERVATION_CHECK_KEYS = {
    "reasonMatched", "liveSubjectObserved", "unsupportedConditionObserved", "sourceBound",
}
SOURCE_TOP_LEVEL = {
    "gke-autopilot": {"schemaVersion", "profileID", "provider", "version", "nodes", "admission"},
    "eks-fargate": {"schemaVersion", "profileID", "provider", "version", "nodes"},
    "aks-virtual-nodes": {"schemaVersion", "profileID", "provider", "version", "nodes"},
    "windows-deep-mode": {"schemaVersion", "profileID", "providerProof", "version", "nodes", "chart"},
    "cgroup-v1": {
        "schemaVersion", "profileID", "providerProof", "version", "nodes", "hostObservations", "chart",
    },
}
BASELINE_POD = {
    "apiVersion": "v1", "kind": "Pod", "metadata": {"name": "memlens-admission-observation"},
    "spec": {"restartPolicy": "Never", "automountServiceAccountToken": False,
             "nodeSelector": {"kubernetes.io/os": "linux"},
             "containers": [{"name": "probe", "image": "registry.k8s.io/pause:3.10",
                             "resources": {"requests": {"cpu": "10m", "memory": "16Mi"}}}]},
}
TARGET_POD = copy.deepcopy(BASELINE_POD)
TARGET_POD["spec"]["volumes"] = [
    {"name": "cgroup", "hostPath": {"path": "/sys/fs/cgroup", "type": "Directory"}},
]
TARGET_POD["spec"]["containers"][0]["volumeMounts"] = [
    {"name": "cgroup", "mountPath": "/host/sys/fs/cgroup", "readOnly": True},
]
SPECS["gke-autopilot"]["probe"] = {"baseline": BASELINE_POD, "target": TARGET_POD}


def load_canonical_profile(path):
    try:
        profile = load_json(path)
        validate_profile(profile)
        canonical = load_json(PROFILES / f"{profile['id']}.json")
        validate_profile(canonical)
    except EvaluationInputError as error:
        raise ReceiptError(str(error)) from error
    if profile["id"] not in SPECS or profile != canonical or profile["expectedOutcome"] != "unsupported":
        raise ReceiptError("the selected profile is not a canonical unsupported profile")
    return profile


def kubernetes_version(source):
    server = require_object(require_object(source["version"], "Kubernetes version").get("serverVersion"), "serverVersion")
    return require_text(server.get("gitVersion"), "Kubernetes server version")


def validate_source(profile_id, source):
    if not isinstance(source, dict) or set(source) != SOURCE_TOP_LEVEL[profile_id] \
            or source.get("schemaVersion") != 1 or source.get("profileID") != profile_id:
        raise ReceiptError("live observation source fields do not match the selected profile")


def node_environment(source, selector, provider, node_image, cni, cgroup, cni_enforced):
    nodes = require_object(source["nodes"], "Kubernetes Nodes")
    items = nodes.get("items", [])
    if not isinstance(items, list) or any(not isinstance(item, dict) for item in items):
        raise ReceiptError("Kubernetes Nodes must contain a bounded item list")
    selected = [item for item in items if selector(item)]
    if not selected:
        raise ReceiptError("live observation has no matching Ready Nodes")
    selected = [item for item in selected if any(
        condition.get("type") == "Ready" and condition.get("status") == "True"
        for condition in item.get("status", {}).get("conditions", []))]
    if not selected:
        raise ReceiptError("live observation has no matching Ready Nodes")
    infos = [require_object(item.get("status", {}).get("nodeInfo"), "Node nodeInfo") for item in selected]
    fields = ("osImage", "containerRuntimeVersion", "architecture", "kernelVersion", "kubeletVersion")
    values = {name: {info.get(name) for info in infos} for name in fields}
    if any(len(observed) != 1 for observed in values.values()):
        raise ReceiptError("observed Nodes do not form one unsupported environment row")
    observed = {name: require_text(next(iter(values[name])), f"Node {name}") for name in fields}
    linux = sum(item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux" for item in items)
    windows = sum(item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "windows" for item in items)
    return {
        "provider": provider, "osImage": observed["osImage"], "runtime": observed["containerRuntimeVersion"],
        "architecture": observed["architecture"], "cgroupVersion": cgroup, "cniEnforced": cni_enforced,
        "kubernetesVersion": kubernetes_version(source), "nodeImage": node_image,
        "kernelVersion": observed["kernelVersion"], "kubeletVersion": observed["kubeletVersion"],
        "cniName": cni, "linuxNodeCount": linux, "windowsNodeCount": windows,
    }, len(selected)


def parse_gke(source):
    provider = require_object(source["provider"], "GKE provider response")
    autopilot = require_object(provider.get("autopilot"), "GKE Autopilot mode")
    if provider.get("status") != "RUNNING" or autopilot.get("enabled") is not True:
        raise ReceiptError("GKE observation requires a running Autopilot cluster")
    admission = require_object(source["admission"], "GKE admission observation")
    status = require_object(admission.get("targetStatus"), "GKE target admission Status")
    message = status.get("message", "")
    denial = (status.get("reason"), status.get("code"))
    constraint = {
        ("Forbidden", 403): "autogke-disallow-hostpath",
        ("GKE Warden constraints violations", 400): "autogke-no-write-mode-hostpath",
    }.get(denial)
    valid_denial = status.get("status") == "Failure" and constraint is not None \
        and constraint in message and "/sys/fs/cgroup" in message
    if admission.get("canCreatePods") is not True or admission.get("baselineAccepted") is not True or not valid_denial:
        raise ReceiptError("GKE admission result is not an Autopilot cgroup hostPath denial")
    network = provider.get("networkConfig", {})
    if network.get("datapathProvider") != "ADVANCED_DATAPATH":
        raise ReceiptError("GKE Autopilot observation requires Dataplane V2")
    environment, count = node_environment(
        source, lambda item: item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux",
        "gke-autopilot", "AUTOPILOT", "GKE Dataplane V2", "unreported", True,
    )
    return environment, require_text(provider.get("currentMasterVersion"), "GKE control-plane version"), count


def parse_eks(source):
    provider = require_object(source["provider"], "EKS provider response")
    cluster = require_object(provider.get("cluster"), "EKS cluster")
    fargate = require_object(provider.get("fargateProfile"), "EKS Fargate profile")
    selectors = fargate.get("selectors")
    valid_selectors = isinstance(selectors, list) and selectors and all(
        isinstance(item, dict) and isinstance(item.get("namespace"), str) and item["namespace"]
        for item in selectors)
    if cluster.get("status") != "ACTIVE" or fargate.get("status") != "ACTIVE" or not valid_selectors:
        raise ReceiptError("EKS observation requires an active Fargate profile")
    environment, count = node_environment(
        source, lambda item: item.get("metadata", {}).get("labels", {}).get(
            "eks.amazonaws.com/compute-type") == "fargate",
        "eks-fargate", "AWS Fargate managed runtime", "AWS Fargate pod networking", "unreported", False,
    )
    return environment, require_text(cluster.get("version"), "EKS control-plane version"), count


def parse_aks(source):
    provider = require_object(source["provider"], "AKS provider response")
    addons = require_object(provider.get("addonProfiles"), "AKS add-on profiles")
    addon = require_object(addons.get("aciConnectorLinux"), "AKS ACI connector")
    network = require_object(provider.get("networkProfile"), "AKS network profile")
    if provider.get("provisioningState") != "Succeeded" or addon.get("enabled") is not True \
            or network.get("networkPlugin") != "azure":
        raise ReceiptError("AKS observation requires enabled Azure virtual nodes")
    def selected(item):
        return item.get("metadata", {}).get("labels", {}).get("type") == "virtual-kubelet" \
            and any(taint.get("key") == "virtual-kubelet.io/provider" and taint.get("value") == "azure"
                    for taint in item.get("spec", {}).get("taints", []))

    normalised = copy.deepcopy(source)
    nodes = require_object(normalised["nodes"], "AKS virtual Nodes")
    items = nodes.get("items", [])
    if not isinstance(items, list) or any(not isinstance(item, dict) for item in items):
        raise ReceiptError("AKS virtual Nodes must contain a bounded item list")
    for item in items:
        if not selected(item):
            continue
        labels = require_object(item.get("metadata", {}).get("labels"), "AKS virtual Node labels")
        if labels.get("kubernetes.io/os") != "linux":
            raise ReceiptError("AKS virtual Nodes must report the standard Linux label")
        info = require_object(item.get("status", {}).get("nodeInfo"), "AKS virtual Node nodeInfo")
        label_architecture = labels.get("kubernetes.io/arch")
        reported_architecture = info.get("architecture")
        if label_architecture not in (None, ""):
            label_architecture = require_text(
                label_architecture, "AKS virtual Node architecture label",
            )
        if label_architecture and reported_architecture not in (None, "") \
                and reported_architecture != label_architecture:
            raise ReceiptError("AKS virtual Node architecture conflicts with its standard label")
        info["architecture"] = label_architecture or reported_architecture or "unreported"
        for field in ("osImage", "containerRuntimeVersion", "kernelVersion"):
            if info.get(field) in (None, ""):
                info[field] = "unreported"

    environment, count = node_environment(
        normalised, selected,
        "aks-virtual-nodes", "AKS virtual-node ACI", "Azure CNI virtual nodes", "unreported", False,
    )
    return environment, require_text(
        provider.get("currentKubernetesVersion"), "AKS current control-plane version",
    ), count


def chart_agent_contract(chart):
    daemonset = require_object(require_object(chart, "candidate chart").get("daemonSet"), "rendered agent DaemonSet")
    pod = daemonset.get("spec", {}).get("template", {}).get("spec", {})
    linux_only = pod.get("nodeSelector", {}).get("kubernetes.io/os") == "linux"
    cgroup_v2 = any(volume.get("hostPath", {}).get("path") == "/sys/fs/cgroup"
                    and volume.get("hostPath", {}).get("type") == "Directory"
                    for volume in pod.get("volumes", []))
    return linux_only, cgroup_v2


def parse_windows(source):
    proof = require_object(source["providerProof"], "Windows provider proof")
    if not chart_agent_contract(source["chart"])[0]:
        raise ReceiptError("candidate chart does not prove a Linux-only agent")
    provider = require_text(proof.get("provider"), "Windows provider")
    prefixes = {"gke-standard": "gce://", "eks-managed-nodes": "aws://", "aks-node-pools": "azure://"}
    def selected(item):
        labels = item.get("metadata", {}).get("labels", {})
        provider_id = item.get("spec", {}).get("providerID", "")
        provider_matches = not provider_id if provider == "self-managed" else \
            provider_id.startswith(prefixes.get(provider, "invalid"))
        return labels.get("kubernetes.io/os") == "windows" and provider_matches
    environment, count = node_environment(
        source, selected, provider, require_text(proof.get("nodeImage"), "Windows node image"),
        require_text(proof.get("cniName"), "Windows CNI"), "unreported", proof.get("cniEnforced") is True,
    )
    return environment, require_text(proof.get("controlPlaneVersion"), "Windows control-plane version"), count


def parse_cgroup_v1(source):
    proof = require_object(source["providerProof"], "cgroup v1 provider proof")
    if proof.get("provider") != "self-managed" or chart_agent_contract(source["chart"]) != (True, True):
        raise ReceiptError("cgroup v1 observation requires self-managed Nodes and the v2 chart contract")
    nodes = require_object(source["nodes"], "cgroup v1 Nodes")
    items = nodes.get("items", [])
    if not isinstance(items, list) or any(not isinstance(item, dict) for item in items):
        raise ReceiptError("cgroup v1 Nodes must contain a bounded item list")
    linux = [item for item in items
             if item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux"]
    expected_uids = {item.get("metadata", {}).get("uid") for item in linux}
    observations = source["hostObservations"]
    valid_observations = isinstance(observations, list) and all(isinstance(item, dict) for item in observations)
    observed_uids = {item.get("nodeUID") for item in observations} if valid_observations else set()
    valid = valid_observations and bool(expected_uids) and None not in expected_uids and None not in observed_uids
    valid = valid and expected_uids == observed_uids and len(observations) == len(expected_uids)
    valid = valid and all(item.get("filesystem") == "cgroup" and item.get("controllersPresent") is False
                          for item in observations)
    if not valid:
        raise ReceiptError("cgroup v1 host observations do not cover every Linux Node")
    environment, count = node_environment(
        source, lambda item: item.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux",
        "self-managed", require_text(proof.get("nodeImage"), "cgroup v1 node image"),
        require_text(proof.get("cniName"), "cgroup v1 CNI"), "v1", proof.get("cniEnforced") is True,
    )
    return environment, require_text(proof.get("controlPlaneVersion"), "cgroup v1 control-plane version"), count


PARSERS = {
    "gke-autopilot": parse_gke, "eks-fargate": parse_eks, "aks-virtual-nodes": parse_aks,
    "windows-deep-mode": parse_windows, "cgroup-v1": parse_cgroup_v1,
}


def validate_environment(profile, environment):
    if not isinstance(environment, dict) or set(environment) != ENVIRONMENT_KEYS:
        raise ReceiptError("unsupported receipt environment fields do not match schema version 1")
    if any(environment_failures(profile, environment)):
        raise ReceiptError("unsupported receipt environment does not match the selected profile")
    if type(environment["cniEnforced"]) is not bool:
        raise ReceiptError("unsupported receipt CNI enforcement state must be boolean")
    if any(type(environment[name]) is not int or environment[name] < 0
           for name in ("linuxNodeCount", "windowsNodeCount")):
        raise ReceiptError("unsupported receipt Node counts must be non-negative integers")
    for name in ENVIRONMENT_KEYS - {"cniEnforced", "linuxNodeCount", "windowsNodeCount"}:
        require_text(environment[name], f"unsupported environment {name}")


def validate_unsupported_receipt(profile, receipt):
    validate_profile(profile)
    if profile["id"] not in SPECS or profile["expectedOutcome"] != "unsupported":
        raise ReceiptError("unsupported receipt requires a canonical unsupported profile")
    reject_sensitive_content(receipt, ReceiptError)
    version = receipt.get("schemaVersion") if isinstance(receipt, dict) else None
    expected_keys = {2: RECEIPT_KEYS_V2, 3: RECEIPT_KEYS_V3}.get(version)
    if expected_keys is None or set(receipt) != expected_keys:
        raise ReceiptError("unsupported receipt fields do not match supported schema version 2 or 3")
    if version == 3:
        tool_commit = receipt["qualificationToolCommit"]
        if not isinstance(tool_commit, str) or re.fullmatch(r"[a-f0-9]{40}", tool_commit) is None:
            raise ReceiptError("unsupported receipt qualificationToolCommit is invalid")
    if receipt["profile"] != {"id": profile["id"], "digest": profile["profileDigest"]}:
        raise ReceiptError("unsupported receipt profile identity does not match")
    parse_timestamp(receipt["observedAt"])
    validate_environment(profile, receipt["environment"])
    require_text(receipt["controlPlaneVersion"], "unsupported control-plane version")
    artefacts = receipt["artefacts"]
    if not isinstance(artefacts, dict) or set(artefacts) != ARTEFACT_KEYS \
            or re.fullmatch(r"[a-f0-9]{40}", artefacts.get("sourceCommit", "")) is None \
            or DIGEST_PATTERN.fullmatch(artefacts.get("chartDigest", "")) is None:
        raise ReceiptError("unsupported receipt artefact binding is invalid")
    proof = receipt["proof"]
    if not isinstance(proof, dict) or set(proof) != PROOF_KEYS:
        raise ReceiptError("unsupported receipt proof fields are invalid")
    spec = SPECS[profile["id"]]
    if proof["source"] != spec["source"] or proof["observationSpecDigest"] != canonical_digest(spec, "unused"):
        raise ReceiptError("unsupported receipt proof does not match the checked-in observation spec")
    for name in ("sourceDigest", "observationSpecDigest"):
        if not isinstance(proof[name], str) or DIGEST_PATTERN.fullmatch(proof[name]) is None:
            raise ReceiptError("unsupported receipt proof digests are invalid")
    checks = receipt["providerChecks"]
    if not isinstance(checks, dict) or set(checks) != PROVIDER_CHECK_KEYS \
            or any(value is not True for value in checks.values()):
        raise ReceiptError("unsupported receipt provider checks must all be true")
    observation = receipt["unsupportedObservation"]
    if not isinstance(observation, dict) or set(observation) != OBSERVATION_KEYS:
        raise ReceiptError("unsupported observation fields are invalid")
    if any(observation[name] != spec[name] for name in ("reasonCode", "method", "state")):
        raise ReceiptError("unsupported observation does not match the selected profile")
    if type(observation["subjectCount"]) is not int or observation["subjectCount"] <= 0:
        raise ReceiptError("unsupported observation subjectCount must be positive")
    observation_checks = observation["checks"]
    if not isinstance(observation_checks, dict) or set(observation_checks) != OBSERVATION_CHECK_KEYS \
            or any(value is not True for value in observation_checks.values()):
        raise ReceiptError("unsupported observation checks must all be true")
    if not isinstance(receipt["receiptDigest"], str) or DIGEST_PATTERN.fullmatch(receipt["receiptDigest"]) is None \
            or receipt["receiptDigest"] != canonical_digest(receipt, "receiptDigest"):
        raise ReceiptError("unsupported receiptDigest does not match canonical content")


def build_receipt(profile, source, observed_at=None, artefact_binding=None):
    validate_source(profile["id"], source)
    binding_keys = set(artefact_binding) if isinstance(artefact_binding, dict) else set()
    version = {frozenset(ARTEFACT_KEYS): 2, frozenset(ARTEFACT_BINDING_KEYS_V3): 3}.get(
        frozenset(binding_keys),
    )
    if version is None:
        raise ReceiptError("unsupported receipt requires an exact artefact binding")
    environment, control_plane, count = PARSERS[profile["id"]](source)
    spec = SPECS[profile["id"]]
    receipt = {
        "schemaVersion": version, "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
        "observedAt": observed_at or datetime.now(timezone.utc).replace(microsecond=0).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "environment": environment, "controlPlaneVersion": control_plane,
        "artefacts": {name: artefact_binding[name] for name in ARTEFACT_KEYS},
        "proof": {"source": spec["source"], "sourceDigest": canonical_digest(source, "unused"),
                  "observationSpecDigest": canonical_digest(spec, "unused")},
        "providerChecks": dict.fromkeys(PROVIDER_CHECK_KEYS, True),
        "unsupportedObservation": {
            "reasonCode": spec["reasonCode"], "method": spec["method"], "state": spec["state"],
            "subjectCount": count, "checks": dict.fromkeys(OBSERVATION_CHECK_KEYS, True),
        },
    }
    if version == 3:
        receipt["qualificationToolCommit"] = artefact_binding["qualificationToolCommit"]
    receipt["receiptDigest"] = canonical_digest(receipt, "receiptDigest")
    validate_unsupported_receipt(profile, receipt)
    return receipt


def main(argv=None, environment=None, runner=None, dry_run=None, binding_verifier=None):
    from unsupported_live import ENVIRONMENT_HELP, collect_live_source

    parser = argparse.ArgumentParser(
        description="Create a sanitised unsupported-profile observation receipt.",
        epilog=ENVIRONMENT_HELP, formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--profile", required=True)
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--chart-archive", required=True)
    parser.add_argument("--chart-digest", required=True)
    parser.add_argument("--output", required=True)
    arguments = parser.parse_args(argv)
    output = Path(arguments.output)
    if output.exists():
        raise SystemExit("refusing to overwrite the unsupported observation receipt")
    try:
        profile = load_canonical_profile(arguments.profile)
        verifier = binding_verifier or verify_binding
        artefact_binding = verifier(
            arguments.source_commit, arguments.chart_archive, arguments.chart_digest,
        )
        options = {"repo_root": ROOT.parents[1]}
        if runner is not None:
            options["runner"] = runner
        if dry_run is not None:
            options["dry_run"] = dry_run
        source = collect_live_source(
            profile["id"], environment if environment is not None else os.environ,
            SPECS[profile["id"]].get("probe"), **options,
        )
        receipt = build_receipt(profile, source, artefact_binding=artefact_binding)
    except (EvaluationInputError, ReceiptError, ValueError) as error:
        raise SystemExit(f"unsupported observation error: {error}") from error
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as error:
        raise SystemExit("refusing to overwrite the unsupported observation receipt") from error
    with os.fdopen(descriptor, "w", encoding="utf-8") as destination:
        destination.write(json.dumps(receipt, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
