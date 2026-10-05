"""Exercise the production one-shot producer with isolated positive and denial identities."""

import json
import time

from common import ContractError, require
from owned_resources import Resource
from job_pods import controlled_pod

RBAC = "rbac.authorization.k8s.io/v1"
PREFIX = "kube-memlens-node-qualification"
PROBE_LABEL = "qualification.kubememlens.io/producer-probe"
# Only the producer's fixed reason vocabulary may leave its private log stream.
PROBE_FAILURES = {"node-context read failed: " + reason: "production stats probe failed: " + reason
                  for reason in ("unsupported-profile", "invalid-target", "untrusted-tls", "authentication-failed",
                                 "access-denied", "timed-out", "unreachable", "invalid-response", "response-too-large",
                                 "throttled", "source-unavailable")}


def identities(namespace):
    result = [{"apiVersion": "v1", "kind": "ServiceAccount", "metadata": {"name": PREFIX + "-" + name, "namespace": namespace},
               "automountServiceAccountToken": False} for name in ("allow", "deny")]
    for name, resources, subjects in (("read", ["nodes"], ("allow", "deny")), ("stats", ["nodes/stats"], ("allow",))):
        result += [
            {"apiVersion": RBAC, "kind": "ClusterRole", "metadata": {"name": PREFIX + "-" + name},
             "rules": [{"apiGroups": [""], "resources": resources, "verbs": ["get"]}]},
            {"apiVersion": RBAC, "kind": "ClusterRoleBinding", "metadata": {"name": PREFIX + "-" + name},
             "roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": PREFIX + "-" + name},
             "subjects": [{"kind": "ServiceAccount", "name": PREFIX + "-" + s, "namespace": namespace} for s in subjects]},
        ]
    result.append({"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": PREFIX + "-invalid-trust", "namespace": namespace},
                   "data": {"ca.crt": "invalid-certificate"}})
    return result


def resources(config):
    # Probes run before the chart is installed. Strict default-deny CNIs need
    # their own policy; its selector must never grant the later producer access.
    policy = {"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
              "metadata": {"name": PREFIX + "-probe", "namespace": config["namespace"]},
              "spec": {"podSelector": {"matchLabels": {PROBE_LABEL: "true"}},
                       "policyTypes": ["Ingress", "Egress"], "ingress": [],
                       "egress": [
                           {"to": [{"ipBlock": {"cidr": route}} for route in config["apiServerCIDRs"]],
                            "ports": [{"protocol": "TCP", "port": 443}, {"protocol": "TCP", "port": 6443}]},
                           {"to": [{"ipBlock": {"cidr": route}} for route in config["nodeCIDRs"]],
                            "ports": [{"protocol": "TCP", "port": 10250}]},
                       ]}}
    return identities(config["namespace"]) + [policy]


def pod(config, node, slot, case, target):
    namespace = config["namespace"]
    account = "deny" if case == "denied" else "allow"
    trust = PREFIX + "-invalid-trust" if case == "bad-ca" else "node-context-trust"
    return {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": f"qualification-{case}-{slot}", "namespace": namespace,
            "labels": {"app.kubernetes.io/name": "kube-memlens-node-context", PROBE_LABEL: "true"}},
            "spec": {"nodeName": node, "serviceAccountName": PREFIX + "-" + account, "automountServiceAccountToken": False,
                     "restartPolicy": "Never", "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532,
                     "fsGroup": 65532, "seccompProfile": {"type": "RuntimeDefault"}},
                     "containers": [{"name": "probe", "image": config["imageRepository"] + "@" + config["imageDigest"],
                        "imagePullPolicy": "IfNotPresent", "command": ["/memlens-node-context"],
                        "args": ["--once", "--node-name=" + target, "--kubelet-ca=/trust/ca.crt", "--kubelet-token-file=/identity/token"],
                        "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
                        "resources": {"requests": {"cpu": "10m", "memory": "32Mi"}, "limits": {"memory": "64Mi"}},
                        "volumeMounts": [{"name": "trust", "mountPath": "/trust", "readOnly": True},
                                         {"name": "identity", "mountPath": "/identity", "readOnly": True},
                                         {"name": "api", "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount", "readOnly": True}]}],
                     "volumes": [
                        {"name": "trust", "configMap": {"name": trust}},
                        {"name": "identity", "projected": {"defaultMode": 288, "sources": [{"serviceAccountToken": {
                            "path": "token", "expirationSeconds": 600, "audience": config["kubeletAudience"]}}]}},
                        {"name": "api", "projected": {"defaultMode": 288, "sources": [
                            {"serviceAccountToken": {"path": "token", "expirationSeconds": 600}},
                            {"configMap": {"name": "kube-root-ca.crt", "items": [{"key": "ca.crt", "path": "ca.crt"}]}}]}}]}}


def require_no_extra_access(k, namespace, account, bindings):
    for verb, resource in (("get", "nodes/proxy"), ("create", "nodes/proxy"), ("get", "nodes/metrics"),
                               ("get", "nodes/log"), ("get", "pods/exec"), ("create", "pods/exec"),
                               ("get", "secrets"), ("list", "secrets")):
        parts = resource.split("/")
        names = [b["name"] for b in bindings] if parts[0] == "nodes" else [""]
        for name in names:
            allowed = k.allowed(f"system:serviceaccount:{namespace}:{account}", verb, parts[0],
                                parts[1] if len(parts) == 2 else "", "" if parts[0] == "nodes" else namespace, name)
            require(allowed is False, "qualification identity has an unexpected permission")


def job(config, node, slot, case, target, timeout=90):
    require(type(timeout) is int and 1 <= timeout <= 90, "production probe deadline exceeds its bound")
    document = pod(config, node, slot, case, target)
    return {"apiVersion": "batch/v1", "kind": "Job",
            "metadata": {k: document["metadata"][k] for k in ("name", "namespace")},
            "spec": {"backoffLimit": 0, "activeDeadlineSeconds": timeout,
                     "template": {"metadata": {"labels": document["metadata"]["labels"]}, "spec": document["spec"]}}}


def terminal_pod(parent, ownership, k, node, timeout):
    resource = Resource.from_object(parent)
    deadline = time.monotonic() + timeout
    child = None
    while True:
        current_parent = ownership.verify(resource)
        candidates = json.loads(k("get", "pods", "-n", resource.namespace, "-l",
                                  "batch.kubernetes.io/controller-uid=" + parent["metadata"]["uid"], "-o", "json"))["items"]
        current = controlled_pod(current_parent, candidates, node)
        if current is not None:
            found = Resource.from_object(current)
            require(found.kind == "Pod" and found.api_version == "v1", "production probe child is not a Pod")
            if child is None:
                child = found
                ownership.remember(child, current["metadata"]["uid"])
            require(found == child and current["metadata"]["uid"] == ownership.owned[child],
                    "production probe Pod was replaced")
            if current.get("status", {}).get("phase") in {"Succeeded", "Failed"}:
                return child, current
        require(time.monotonic() < deadline, "production probe did not finish within its deadline")
        time.sleep(1)


def run_probes(config, bindings, ownership, k, timeout=90):
    require(len(bindings) >= 2, "cross-Node denial requires at least two bound Nodes")
    manifests = resources(config)
    ownership.require_absent([Resource.from_object(m) for m in manifests])
    for manifest in manifests:
        ownership.create(manifest)
    for account in ("allow", "deny"):
        require_no_extra_access(k, config["namespace"], PREFIX + "-" + account, bindings)
    observations = []
    for slot, binding in enumerate(bindings):
        for case, expected, reason in (("allowed", "Succeeded", None), ("denied", "Failed", "access-denied"),
                                       ("bad-ca", "Failed", "untrusted-tls"), ("wrong-node", "Failed", "invalid-target")):
            target = bindings[(slot + 1) % len(bindings)]["name"] if case == "wrong-node" else binding["name"]
            manifest = job(config, binding["name"], slot, case, target, timeout)
            created = ownership.create(manifest)
            parent = Resource.from_object(created)
            resource, current = terminal_pod(created, ownership, k, binding["name"], timeout)
            phase = current["status"]["phase"]
            output = k("logs", resource.name, "-n", resource.namespace, "-c", "probe", maximum=32 * 1024)
            require(controlled_pod(ownership.verify(parent), [ownership.verify(resource)], binding["name"]) is not None,
                    "production probe changed during its log read")
            if case == "allowed" and phase == "Failed" and output.strip() in PROBE_FAILURES:
                raise ContractError(PROBE_FAILURES[output.strip()])
            require(phase == expected, "production probe reached the wrong terminal state")
            if reason:
                require(output.strip() == "node-context read failed: " + reason, "production probe returned an unexpected failure")
            else:
                document = json.loads(output)
                require(document["kind"] == "NodeContextDiagnostic" and document["schemaVersion"] == 1 and document["redacted"] is True,
                        "production probe did not return its bounded diagnostic contract")
                observation = document["observation"]
                require(observation["nodeName"] == binding["name"] and observation["availability"] == "available"
                        and observation["evidence"]["freshness"] == "fresh", "production probe target or freshness differs")
                observations.append(observation)
            ownership.delete(parent)
            ownership.delete(resource)
    return observations
