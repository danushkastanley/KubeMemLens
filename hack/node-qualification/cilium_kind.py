"""Install a reviewed, immutable CNI only in the newly owned local 1.36 fixture."""

import argparse
import hashlib
import json
from pathlib import Path

from check_kubernetes_observer import verify_kind_target
from common import digest, require, write_new
from process import execute

CHART = "oci://quay.io/cilium/charts/cilium@sha256:906ce40d35daad838d12add8a5ba7033e767767f51799a93c7eace2cec9cdc05"
ARCHIVE_SHA256 = "06210eef7c23d15f7699c79e2fe3a1ec9c389024c5c5c006ea04022d322449a2"
VALUES = {"ipam": {"mode": "kubernetes"}, "operator": {"replicas": 1}, "policyCIDRMatchMode": ["nodes"],
          "hubble": {"enabled": False}, "envoy": {"enabled": False}, "l7Proxy": False}
IMAGES = {
    "quay.io/cilium/cilium:v1.20.1@sha256:ae9ea21f7427fe24bc6ea7247eb552157a1b0a431744045d3f641545ca71d11b",
    "quay.io/cilium/operator-generic:v1.20.1@sha256:6c3885fc7b629099fdbe2a5c87869c86feb825fa18fae299eac0f61918d16ecf",
}


def verify_render(documents):
    images = set()
    require(isinstance(documents, list) and 0 < len(documents) <= 128, "CNI render count is invalid")
    for document in documents:
        require(document["metadata"].get("namespace", "") in {"", "kube-system", "cilium-secrets"},
                "CNI render targets an unexpected namespace")
        spec = document.get("spec", {}).get("template", {}).get("spec", {})
        for container in spec.get("containers", []) + spec.get("initContainers", []):
            images.add(container["image"])
    require(images == IMAGES, "CNI render image inventory differs from the pinned protocol")
    configs = [d for d in documents if d["kind"] == "ConfigMap" and d["metadata"]["name"] == "cilium-config"]
    require(len(configs) == 1 and configs[0]["data"]["policy-cidr-match-mode"] == "nodes"
            and configs[0]["data"]["enable-policy"] == "default", "CNI policy settings differ from the fixed protocol")


def prepare(root, inventory_binary, run=execute):
    destination = Path(root) / "cilium"
    destination.mkdir(mode=0o700)
    run(["helm", "pull", CHART, "--destination", str(destination)], timeout=120)
    archives = list(destination.glob("*.tgz"))
    require(len(archives) == 1 and archives[0].stat().st_size <= 4 * 1024 * 1024, "CNI archive inventory is invalid")
    archive = archives[0]
    require(hashlib.sha256(archive.read_bytes()).hexdigest() == ARCHIVE_SHA256, "CNI archive differs from the reviewed package")
    values = destination / "values.json"
    write_new(values, VALUES)
    rendered = run(["helm", "template", "cilium", str(archive), "--namespace", "kube-system", "--values", str(values)], timeout=60)
    documents = json.loads(run([inventory_binary], data=rendered.encode(), maximum=2 * 1024 * 1024))
    verify_render(documents)
    return archive, values


def install(args):
    verify_kind_target(args.kubeconfig, args.context)
    command = ["kubectl", "--kubeconfig", args.kubeconfig, "--context", args.context, "--request-timeout=10s"]
    nodes = json.loads(execute(command + ["get", "nodes", "-o", "json"]))["items"]
    require(len(nodes) == 2 and all(n["status"]["nodeInfo"]["kubeletVersion"] == "v1.36.1" for n in nodes),
            "CNI fixture requires the pinned two-Node Kubernetes 1.36.1 topology")
    daemonsets = json.loads(execute(command + ["get", "daemonsets", "-n", "kube-system", "-o", "json"]))["items"]
    require({d["metadata"]["name"] for d in daemonsets} <= {"kube-proxy"}, "refusing to replace an existing local CNI")
    archive, values = prepare(args.private, str(Path(args.private) / "chart-inventory"))
    execute(["helm", "--kubeconfig", args.kubeconfig, "--kube-context", args.context,
             "install", "cilium", str(archive), "--namespace", "kube-system", "--values", str(values),
             "--wait", "--timeout", "180s"], timeout=195)
    execute(command + ["wait", "--for=condition=Ready", "nodes", "--all", "--timeout=120s"], timeout=125)
    write_new(Path(args.private) / "cni.json", {"name": "cilium", "version": "1.20.1", "chart": CHART,
              "archiveDigest": "sha256:" + ARCHIVE_SHA256, "valuesDigest": digest(VALUES, "valuesDigest"), "images": sorted(IMAGES)})


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("kubeconfig", "context", "private"):
        parser.add_argument("--" + name, required=True)
    install(parser.parse_args())
