"""Install the bound chart and capture exact ownership for every persistent object."""

import json

from common import ContractError, require
from owned_resources import Resource
from prepare_provider import REPOSITORY
from process import execute

RELEASE = "kube-memlens-node-qualification"


def hook(document):
    return document["metadata"].get("annotations", {}).get("helm.sh/hook", "")


class Installer:
    def __init__(self, bundle, ownership, inventory_binary, run=execute):
        self.bundle, self.ownership, self.run = bundle, ownership, run
        c = bundle.configuration
        self.namespace = Resource("v1", "Namespace", c["namespace"])
        self.helm = ["helm", "--kubeconfig", c["kubeconfigPath"], "--kube-context", c["context"]]
        self.values = REPOSITORY / "hack/provider-values" / (c["inventoryProfile"] + ".yaml")
        self.desired = {}
        for phase in ("baseline", "enabled"):
            raw = self.run(["helm", "template", RELEASE, c["chartArchive"], "--namespace", c["namespace"],
                            "--kube-version", c["kubernetesVersion"], "--dry-run=client", "--values", str(self.values),
                            "--values", str(bundle.directory / (phase + "-values.json"))], maximum=2 * 1024 * 1024, timeout=60)
            documents = json.loads(self.run([inventory_binary], data=raw.encode(), maximum=2 * 1024 * 1024))
            require(isinstance(documents, list) and 0 < len(documents) <= 128, "chart inventory count is invalid")
            desired = {}
            for document in documents:
                resource = Resource.from_object(document)
                require(resource.kind != "Namespace", "the coordinator owns namespace creation")
                require(resource.namespace in {"", c["namespace"], "kube-system"}, "chart targets an unexpected namespace")
                require(resource.namespace != "kube-system" or resource.kind == "RoleBinding"
                        and resource.name == "kube-memlens-extension-authentication-reader", "chart targets an unexpected system resource")
                require(resource not in desired, "chart contains duplicate resource identities")
                if hook(document):
                    require(hook(document) in {"test", "post-install,post-upgrade,post-rollback"}, "chart has an unreviewed lifecycle hook")
                desired[resource] = document
            self.desired[phase] = desired

    def preflight(self):
        resources = set(self.desired["baseline"]) | set(self.desired["enabled"])
        self.ownership.require_absent([self.namespace] + sorted(resources, key=lambda r: r.uri))

    def create_namespace(self):
        self.ownership.create({"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": self.namespace.name}})

    def install(self, phase):
        require(phase in self.desired, "invalid chart phase")
        self.ownership.verify(self.namespace)
        # Hooks are intentionally recreated by Helm. A lingering prior hook must
        # be handled before an upgrade, rather than silently replacing its UID.
        self.ownership.require_absent([r for r, d in self.desired[phase].items() if hook(d)])
        complete = False
        try:
            self.run(self.helm + ["upgrade", "--install", RELEASE, self.bundle.configuration["chartArchive"],
                     "--namespace", self.namespace.name, "--values", str(self.values),
                     "--values", str(self.bundle.directory / (phase + "-values.json")),
                     "--wait", "--timeout", "180s"], timeout=195, maximum=2 * 1024 * 1024)
            complete = True
        finally:
            self.capture(phase, complete)

    def capture(self, phase, complete):
        self.ownership.verify(self.namespace)
        failed = False
        for resource, expected in self.desired[phase].items():
            try:
                self.capture_one(resource, expected, complete)
            except (ContractError, KeyError, ValueError, TypeError):
                self.ownership.conflict(resource)
                failed = True
        require(not failed, "chart ownership capture failed; private receipts retained")

    def capture_one(self, resource, expected, complete):
        actual = self.ownership.get(resource)
        if actual is None:
            require(not complete or hook(expected), "a chart resource was not installed")
            return
        if resource in self.ownership.owned:
            self.ownership.verify(resource)
            return
        annotations = actual["metadata"].get("annotations", {})
        if hook(expected):
            require(annotations.get("helm.sh/hook") == hook(expected), "unexpected object occupies a chart hook name")
            # Transient RBAC hooks may lack Helm's persistent-object labels.
            # Match their exact grant and binding before retaining the UID.
            for field in ("rules", "subjects", "roleRef", "aggregationRule"):
                require(actual.get(field) == expected.get(field), "hook authorisation differs from the bound chart")
        else:
            require(annotations.get("meta.helm.sh/release-name") == RELEASE
                    and annotations.get("meta.helm.sh/release-namespace") == self.namespace.name,
                    "chart resource is not owned by the expected release")
        self.ownership.remember(resource, actual["metadata"]["uid"])
