"""Exercise the production one-shot producer with isolated positive and denial identities."""

import json
import time

from common import require
from owned_resources import Resource

RBAC = "rbac.authorization.k8s.io/v1"
PREFIX = "kube-memlens-node-qualification"


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


def pod(config, node, slot, case, target):
    namespace = config["namespace"]
    account = "deny" if case == "denied" else "allow"
    trust = PREFIX + "-invalid-trust" if case == "bad-ca" else "node-context-trust"
    return {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": f"qualification-{case}-{slot}", "namespace": namespace,
            "labels": {"app.kubernetes.io/name": "kube-memlens-node-context"}},
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


def run_probes(config, bindings, ownership, k, timeout=90):
    require(len(bindings) >= 2, "cross-Node denial requires at least two bound Nodes")
    manifests = identities(config["namespace"])
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
            manifest = pod(config, binding["name"], slot, case, target)
            created = ownership.create(manifest)
            resource = Resource.from_object(created)
            deadline = time.monotonic() + timeout
            while True:
                current = ownership.verify(resource)
                phase = current.get("status", {}).get("phase")
                if phase in {"Succeeded", "Failed"}:
                    break
                require(time.monotonic() < deadline, "production probe did not finish within its deadline")
                time.sleep(1)
            output = k("logs", resource.name, "-n", resource.namespace, "-c", "probe", maximum=32 * 1024)
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
            ownership.delete(resource)
    return observations
