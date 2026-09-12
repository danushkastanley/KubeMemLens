"""Reversible disruptions guarded by the run's UID and the exact observed version."""

import json
import uuid
from contextlib import contextmanager

from common import require
from owned_resources import Resource


def _patch(ownership, resource, document, changes):
    require(resource in ownership.owned and Resource.from_object(document) == resource,
            "mutation requires a resource owned by this run")
    metadata = document["metadata"]
    require(metadata.get("uid") == ownership.owned[resource], "mutation target was replaced")
    version = metadata.get("resourceVersion")
    require(isinstance(version, str) and version, "mutation target has no resource version")
    patch = [{"op": "test", "path": "/metadata/uid", "value": ownership.owned[resource]},
             {"op": "test", "path": "/metadata/resourceVersion", "value": version}] + changes
    args = ["patch", resource.kind, resource.name, "--type=json", "--patch-file=/dev/stdin", "-o", "json"]
    if resource.namespace:
        args += ["--namespace", resource.namespace]
    result = json.loads(ownership.k(*args, data=json.dumps(patch).encode()))
    require(Resource.from_object(result) == resource and result["metadata"].get("uid") == metadata["uid"],
            "mutation returned a different resource identity")
    require(result["metadata"].get("resourceVersion") not in {None, "", version}, "mutation returned no new version")
    return result


@contextmanager
def suspended_producer_access(ownership, namespace):
    """Remove only the known producer subject, then restore that exact binding."""
    resource = Resource("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "kube-memlens-node-context-producer")
    document = ownership.verify(resource)
    subject = {"kind": "ServiceAccount", "name": "kube-memlens-node-context", "namespace": namespace}
    require(document.get("subjects") == [subject] and document.get("roleRef") == {
        "apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": resource.name},
        "producer binding differs from the qualification contract")
    revoked = _patch(ownership, resource, document, [{"op": "replace", "path": "/subjects", "value": []}])
    require(not revoked.get("subjects"), "producer binding was not suspended")
    try:
        yield
    finally:
        # Reuse the exact post-revocation version. An intervening edit must fail
        # rather than being overwritten by the restoration operation.
        restored = _patch(ownership, resource, revoked, [{"op": "add", "path": "/subjects", "value": [subject]}])
        require(restored.get("subjects") == [subject], "producer binding was not restored")


def restart_workload(ownership, namespace, component):
    require(component in {"agent", "collector"}, "unsupported recovery component")
    resource = Resource("apps/v1", "DaemonSet" if component == "agent" else "Deployment",
                        "kube-memlens-" + component, namespace)
    document = ownership.verify(resource)
    metadata = document["spec"]["template"]["metadata"]
    annotations = dict(metadata.get("annotations", {}))
    annotations["kubememlens.io/qualification-restart"] = str(uuid.uuid4())
    return _patch(ownership, resource, document, [{"op": "add", "path": "/spec/template/metadata/annotations",
                                                "value": annotations}])


@contextmanager
def suspended_network_rule(ownership, resource, direction):
    require(direction in {"ingress", "egress"} and resource.kind == "NetworkPolicy"
            and resource.name == "node-qualification-network-" + direction,
            "only a dedicated qualification policy rule may be suspended")
    document = ownership.verify(resource)
    rules = document["spec"].get(direction)
    require(isinstance(rules, list) and rules, "qualification policy has no positive control")
    path = "/spec/" + direction
    revoked = _patch(ownership, resource, document, [{"op": "replace", "path": path, "value": []}])
    try:
        yield
    finally:
        _patch(ownership, resource, revoked, [{"op": "add", "path": path, "value": rules}])
