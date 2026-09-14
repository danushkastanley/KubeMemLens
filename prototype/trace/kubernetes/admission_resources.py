"""Explicit Kubernetes resources for the single-node admission prototype."""


def resource(kind, name, namespace, api="v1", **fields):
    metadata = {"name": name}
    if namespace:
        metadata["namespace"] = namespace
    return {"apiVersion": api, "kind": kind, "metadata": metadata, **fields}


def workload(name, namespace, node, image, args, port, mounts, volumes, security, seccomp):
    labels = {"app": name}
    container = {
        "name": name, "image": image, "imagePullPolicy": "Never", "args": args,
        "securityContext": security, "volumeMounts": mounts,
        "resources": {"requests": {"cpu": "100m", "memory": "64Mi"},
                      "limits": {"cpu": "2", "memory": "512Mi"}},
        "ports": [{"containerPort": port}],
    }
    if name == "admission-api":
        container["readinessProbe"] = {
            "httpGet": {"path": "/readyz", "port": port, "scheme": "HTTPS"},
            "periodSeconds": 2,
        }
    pod = {
        "nodeName": node, "serviceAccountName": name,
        "automountServiceAccountToken": name == "admission-api",
        "terminationGracePeriodSeconds": 10,
        "securityContext": {"fsGroup": security["runAsGroup"], "seccompProfile": seccomp},
        "containers": [container], "volumes": volumes,
    }
    deployment = resource("Deployment", name, namespace, "apps/v1", spec={
        "replicas": 1, "strategy": {"type": "Recreate"},
        "selector": {"matchLabels": labels},
        "template": {"metadata": {"labels": labels}, "spec": pod},
    })
    service = resource("Service", name, namespace, spec={
        "selector": labels, "ports": [{"port": port, "targetPort": port}],
    })
    return [deployment, service]


def deployments(image, node, uid, namespace, kubelet_root, control_pin):
    items = []
    paths = {
        "btf": "/sys/kernel/btf", "tracing": "/sys/kernel/tracing",
        "security": "/sys/kernel/security", "bpf": "/sys/fs/bpf",
        "cgroup": "/sys/fs/cgroup",
    }
    node_args = [
        "binding-node", "--node-uid", uid, "--node-name", node,
        "--kubelet-cgroup-root", kubelet_root, "--tls-cert", "/tls/tls.crt",
        "--tls-key", "/tls/tls.key", "--control-ca", "/tls/control-ca.crt",
        "--control-certificate-sha256", control_pin,
    ]
    api_args = [
        "admission-api", "--tls-cert", "/tls/tls.crt", "--tls-key", "/tls/tls.key",
        "--node-client-cert", "/tls/node-client.crt",
        "--node-client-key", "/tls/node-client.key",
        "--node-registry", "/registry/nodes.json",
    ]
    for name, args, port in (("binding-node", node_args, 9443),
                             ("admission-api", api_args, 8443)):
        security = {
            "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
            "runAsUser": 65532, "runAsGroup": 65532, "capabilities": {"drop": ["ALL"]},
        }
        mounts = [{"name": "tls", "mountPath": "/tls", "readOnly": True}]
        volumes = [{"name": "tls", "secret": {"secretName": name + "-tls", "defaultMode": 0o440}}]
        seccomp = {"type": "RuntimeDefault"}
        if name == "binding-node":
            security.update({"runAsUser": 0, "runAsGroup": 0,
                             "capabilities": {"drop": ["ALL"], "add": ["BPF", "PERFMON"]}})
            seccomp = {"type": "Localhost", "localhostProfile": "kube-memlens-trace/binding-node.json"}
            mounts += [{"name": n, "mountPath": path, "readOnly": True} for n, path in paths.items()]
            volumes += [{"name": n, "hostPath": {"path": path, "type": "Directory"}} for n, path in paths.items()]
        else:
            mounts.append({"name": "registry", "mountPath": "/registry", "readOnly": True})
            volumes.append({"name": "registry", "configMap": {"name": "node-registry"}})
        items += workload(name, namespace, node, image, args, port, mounts, volumes, security, seccomp)
    return items


def permissions(namespace):
    api = "rbac.authorization.k8s.io/v1"
    subject = {"kind": "ServiceAccount", "namespace": namespace, "name": "admission-api"}
    role = "kml-r6-admission"
    items = [
        resource("ServiceAccount", "admission-api", namespace, automountServiceAccountToken=True),
        resource("ServiceAccount", "binding-node", namespace, automountServiceAccountToken=False),
        resource("ClusterRole", role, None, api, rules=[
            {"apiGroups": [""], "resources": ["pods", "nodes"], "verbs": ["get"]},
            {"apiGroups": ["authorization.k8s.io"], "resources": ["subjectaccessreviews"], "verbs": ["create"]},
        ]),
        resource("ClusterRoleBinding", role, None, api,
                 roleRef={"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": role},
                 subjects=[subject]),
        resource("RoleBinding", "kml-r6-admission-auth", "kube-system", api,
                 roleRef={"apiGroup": "rbac.authorization.k8s.io", "kind": "Role",
                          "name": "extension-apiserver-authentication-reader"}, subjects=[subject]),
    ]
    return items
