"""The fixed, explicitly approved Kubernetes observation footprint."""

HOST_OBSERVER = "node-qualification-host-observer"
METRICS_OBSERVER = "qualification-metrics"
IDENTITY_OBSERVER = "qualification-identity"


def security():
    return {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532,
            "allowPrivilegeEscalation": False, "privileged": False,
            "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]},
            "seccompProfile": {"type": "RuntimeDefault"}}


def host_observer(namespace, image, selector, tolerations=()):
    labels = {"app.kubernetes.io/name": HOST_OBSERVER}
    return {"apiVersion": "apps/v1", "kind": "DaemonSet",
            "metadata": {"name": HOST_OBSERVER, "namespace": namespace},
            "spec": {"selector": {"matchLabels": labels}, "template": {
                "metadata": {"labels": labels},
                "spec": {"nodeSelector": dict(selector), "tolerations": list(tolerations),
                         "automountServiceAccountToken": False, "hostPID": False, "hostNetwork": False,
                         "terminationGracePeriodSeconds": 5,
                         "containers": [{"name": "observer", "image": image, "command": ["sleep", "7200"],
                                         "securityContext": security(),
                                         "resources": {"requests": {"cpu": "1m", "memory": "4Mi"}, "limits": {"memory": "16Mi"}},
                                         "volumeMounts": [{"name": "cgroups", "mountPath": "/host/sys/fs/cgroup", "readOnly": True}]}],
                         "volumes": [{"name": "cgroups", "hostPath": {"path": "/sys/fs/cgroup", "type": "Directory"}}]}}}}


def host_policy(namespace):
    return {"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
            "metadata": {"name": HOST_OBSERVER, "namespace": namespace},
            "spec": {"podSelector": {"matchLabels": {"app.kubernetes.io/name": HOST_OBSERVER}},
                     "policyTypes": ["Ingress", "Egress"], "ingress": [], "egress": []}}


def ephemeral_observer(image, component):
    identity = component == "node-context"
    result = {"name": IDENTITY_OBSERVER if identity else METRICS_OBSERVER, "image": image,
              "command": ["sleep", "7200"], "securityContext": security()}
    if identity:
        result["volumeMounts"] = [{"name": "kubelet-token", "mountPath": "/identity", "readOnly": True}]
    # Kubernetes forbids resources/probes/restart policies on ephemeral containers.
    return result
