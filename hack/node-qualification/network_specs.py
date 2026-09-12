"""Fixed token-free targets for measuring the optional producer's NetworkPolicy."""

import re

from common import require
from observer_specs import security
from owned_resources import Resource

LABEL = "kubememlens.io/qualification-network"
APP = {"app.kubernetes.io/name": "kube-memlens-node-context"}
PORT = 18080
MARKER = "node-qualification-network-probe"
PREFIX = "node-qualification-network"


def _pod_spec(image, node, server):
    container = {"name": "probe", "image": image, "securityContext": security(),
                 "resources": {"requests": {"cpu": "1m", "memory": "4Mi"}, "limits": {"memory": "16Mi"}}}
    spec = {"nodeName": node, "automountServiceAccountToken": False, "hostNetwork": False, "hostPID": False,
            "restartPolicy": "Never", "terminationGracePeriodSeconds": 1,
            "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532,
                                "fsGroup": 65532, "seccompProfile": {"type": "RuntimeDefault"}},
            "containers": [container]}
    if server:
        container.update(command=["sh", "-c", "mkdir -p /probe/www; printf '%s' '" + MARKER +
                                  "' > /probe/www/index.html; exec httpd -f -p " + str(PORT) + " -h /probe/www"],
                         volumeMounts=[{"name": "response", "mountPath": "/probe"}],
                         readinessProbe={"httpGet": {"path": "/", "port": PORT}, "periodSeconds": 2, "timeoutSeconds": 1})
        spec["volumes"] = [{"name": "response", "emptyDir": {"medium": "Memory", "sizeLimit": "1Mi"}}]
    else:
        container["command"] = ["sleep", "600"]
    return spec


def policy(namespace, name, selector, direction, peers):
    key = "from" if direction == "ingress" else "to"
    return {"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
            "metadata": {"name": PREFIX + "-" + name, "namespace": namespace},
            "spec": {"podSelector": {"matchLabels": selector}, "policyTypes": ["Ingress", "Egress"],
                     "ingress": [], "egress": [], direction: [{key: peers, "ports": [{"protocol": "TCP", "port": PORT}]}]}}


def manifests(namespace, image, node_names):
    require(len(node_names) == 2 and len(set(node_names)) == 2, "network checks require the fixed two-Node pool")
    Resource("v1", "Namespace", namespace)
    for name in node_names:
        Resource("v1", "Namespace", name)  # Node names use the same path-safe grammar.
    require(isinstance(image, str) and re.fullmatch(r"[A-Za-z0-9./_-]+@sha256:[a-f0-9]{64}", image),
            "network probe image must be pinned")
    result = [
        policy(namespace, "ingress", {**APP, LABEL: "ingress"}, "ingress",
               [{"podSelector": {"matchLabels": {LABEL: "control"}}}]),
        policy(namespace, "egress", APP, "egress", [{"podSelector": {"matchLabels": {LABEL: "egress"}}}]),
        policy(namespace, "control", {LABEL: "control"}, "egress", [{"podSelector": {"matchExpressions": [
            {"key": LABEL, "operator": "In", "values": ["ingress", "egress"]}]}}]),
    ]
    for slot, node in enumerate(node_names):
        for role in ("ingress", "egress"):
            labels = {LABEL: role, **(APP if role == "ingress" else {})}
            # A Job owns the serving Pod so the producer DaemonSet cannot adopt
            # an ingress target that deliberately matches its policy selector.
            result.append({"apiVersion": "batch/v1", "kind": "Job",
                           "metadata": {"name": f"{PREFIX}-{role}-{slot}", "namespace": namespace},
                           "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 600,
                                    "template": {"metadata": {"labels": labels}, "spec": _pod_spec(image, node, True)}}})
        result.append({"apiVersion": "v1", "kind": "Pod",
                       "metadata": {"name": f"{PREFIX}-control-{slot}", "namespace": namespace, "labels": {LABEL: "control"}},
                       "spec": _pod_spec(image, node, False)})
    return result


def preview(namespace, image):
    return {"reviewOnly": True, "items": manifests(namespace, image, ["selected-node-0", "selected-node-1"])}
