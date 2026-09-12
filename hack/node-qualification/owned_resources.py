"""Track and delete only the exact Kubernetes objects created by a run."""

import json
import re
import time
from dataclasses import dataclass

from common import ContractError, require, write_new

COLLECTIONS = {
    ("v1", "Namespace"): ("/api/v1/namespaces", False),
    ("v1", "ConfigMap"): ("/api/v1", True),
    ("v1", "ServiceAccount"): ("/api/v1", True),
    ("v1", "Service"): ("/api/v1", True),
    ("v1", "Secret"): ("/api/v1", True),
    ("v1", "Pod"): ("/api/v1", True),
    ("apps/v1", "Deployment"): ("/apis/apps/v1", True),
    ("apps/v1", "DaemonSet"): ("/apis/apps/v1", True),
    ("batch/v1", "Job"): ("/apis/batch/v1", True),
    ("networking.k8s.io/v1", "NetworkPolicy"): ("/apis/networking.k8s.io/v1", True),
    ("rbac.authorization.k8s.io/v1", "ClusterRole"): ("/apis/rbac.authorization.k8s.io/v1/clusterroles", False),
    ("rbac.authorization.k8s.io/v1", "ClusterRoleBinding"): ("/apis/rbac.authorization.k8s.io/v1/clusterrolebindings", False),
    ("rbac.authorization.k8s.io/v1", "RoleBinding"): ("/apis/rbac.authorization.k8s.io/v1", True),
    ("rbac.authorization.k8s.io/v1", "Role"): ("/apis/rbac.authorization.k8s.io/v1", True),
    ("apiregistration.k8s.io/v1", "APIService"): ("/apis/apiregistration.k8s.io/v1/apiservices", False),
}
PLURALS = {"ConfigMap": "configmaps", "ServiceAccount": "serviceaccounts", "Service": "services",
           "Secret": "secrets", "Pod": "pods", "Deployment": "deployments", "DaemonSet": "daemonsets",
           "NetworkPolicy": "networkpolicies", "RoleBinding": "rolebindings", "Role": "roles", "Job": "jobs"}


@dataclass(frozen=True)
class Resource:
    api_version: str
    kind: str
    name: str
    namespace: str = ""

    def __post_init__(self):
        require((self.api_version, self.kind) in COLLECTIONS, "resource kind is outside the qualification contract")
        for value in (self.name, self.namespace):
            require(value == "" or re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,251}[a-z0-9]|[a-z0-9]", value), "invalid resource name")
        require(self.name, "resource name is required")
        require(bool(self.namespace) == COLLECTIONS[(self.api_version, self.kind)][1], "resource scope mismatch")

    @property
    def uri(self):
        path, namespaced = COLLECTIONS[(self.api_version, self.kind)]
        if namespaced:
            path += "/namespaces/" + self.namespace + "/" + PLURALS[self.kind]
        return path + "/" + self.name

    @classmethod
    def from_object(cls, value):
        return cls(value["apiVersion"], value["kind"], value["metadata"]["name"], value["metadata"].get("namespace", ""))


class OwnedResources:
    def __init__(self, k, journal):
        self.k, self.journal, self.owned, self.conflicts = k, journal, {}, set()
        require(not journal.exists(), "ownership journal already exists")
        journal.mkdir(mode=0o700)

    def get(self, resource):
        args = ["get", resource.kind, resource.name, "--ignore-not-found", "-o", "json"]
        if resource.namespace:
            args += ["--namespace", resource.namespace]
        raw = self.k(*args)
        if not raw.strip():
            return None
        document = json.loads(raw)
        require(Resource.from_object(document) == resource, "resource read returned a different identity")
        return document

    def require_absent(self, resources):
        for resource in resources:
            require(self.get(resource) is None, "a qualification resource already exists")

    def remember(self, resource, uid):
        require(isinstance(uid, str) and 0 < len(uid) <= 128, "created resource has no valid UID")
        require(resource not in self.owned, "resource ownership was already recorded")
        # Persist each ownership receipt before any later operation. It is private
        # and contains no object payload, so Secrets never enter the journal.
        index = len(self.owned)
        self.owned[resource] = uid
        write_new(self.journal / (str(index) + ".json"),
                  {"apiVersion": resource.api_version, "kind": resource.kind, "name": resource.name,
                   "namespace": resource.namespace, "uid": uid})

    def create(self, manifest):
        resource = Resource.from_object(manifest)
        require(resource not in self.owned, "resource was already created by this run")
        document = json.loads(self.k("create", "-f", "-", "-o", "json", data=json.dumps(manifest).encode()))
        require(Resource.from_object(document) == resource, "create returned a different resource")
        self.remember(resource, document["metadata"]["uid"])
        return document

    def verify(self, resource):
        require(resource in self.owned, "resource is not owned by this run")
        document = self.get(resource)
        require(document is not None and document["metadata"].get("uid") == self.owned[resource], "owned resource was replaced or removed")
        return document

    def conflict(self, resource):
        if resource in self.conflicts:
            return
        index = len(self.conflicts)
        self.conflicts.add(resource)
        write_new(self.journal / ("conflict-" + str(index) + ".json"),
                  {"apiVersion": resource.api_version, "kind": resource.kind, "name": resource.name, "namespace": resource.namespace})

    def delete(self, resource):
        require(resource in self.owned, "refusing to delete an unowned resource")
        document = self.get(resource)
        if document is None:
            return
        require(document["metadata"].get("uid") == self.owned[resource], "refusing to delete a replacement resource")
        data = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": self.owned[resource]},
                "propagationPolicy": "Background"}
        self.k("delete", "--raw", resource.uri, "-f", "-", data=json.dumps(data).encode())

    def cleanup(self, timeout=120):
        errors = list(self.conflicts)
        resources = list(reversed(self.owned))
        # A replaced child must not be removed indirectly by deleting its parent
        # namespace. Retain namespaces whenever an ownership check failed.
        for resource in sorted(resources, key=lambda r: r.kind == "Namespace"):
            if resource.kind == "Namespace" and errors:
                continue
            try:
                self.delete(resource)
            except (ContractError, ValueError, KeyError, TypeError):
                errors.append(resource)
        if errors:
            raise ContractError("cleanup found a replaced or unreadable resource; private ownership receipts retained")
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            remaining = [r for r in self.owned if self.get(r) is not None]
            if not remaining:
                return
            time.sleep(1)
        raise ContractError("owned resource cleanup did not complete within its deadline")
