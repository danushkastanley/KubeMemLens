import copy
import json
import tempfile
import unittest
from pathlib import Path

from common import ContractError
from owned_resources import OwnedResources, Resource
from provider_install import Installer, RELEASE
from test_owned_resources import Kubernetes


class ChartOwnershipTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.k = Kubernetes(); self.owned = OwnedResources(self.k, Path(self.temp.name) / "journal")
        self.namespace = {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "fixture"}}
        self.role = {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole",
                     "metadata": {"name": "fixture-role"}, "rules": [{"apiGroups": [""], "resources": ["nodes"], "verbs": ["get"]}]}
        self.installer = object.__new__(Installer)
        self.installer.ownership = self.owned
        self.installer.namespace = Resource.from_object(self.namespace)
        self.installer.desired = {"baseline": {Resource.from_object(self.role): self.role}, "enabled": {}}

    def test_existing_cluster_resource_blocks_install_before_namespace_creation(self):
        self.k("create", data=json.dumps(self.role).encode())
        with self.assertRaises(ContractError):
            self.installer.preflight()
        self.assertNotIn(Resource.from_object(self.namespace), self.k.objects)

    def test_capture_requires_release_ownership_and_retains_uid(self):
        self.owned.create(self.namespace)
        actual = copy.deepcopy(self.role)
        actual["metadata"]["annotations"] = {"meta.helm.sh/release-name": RELEASE, "meta.helm.sh/release-namespace": "fixture"}
        created = json.loads(self.k("create", data=json.dumps(actual).encode()))
        self.installer.capture("baseline", True)
        resource = Resource.from_object(actual)
        self.assertEqual(self.owned.owned[resource], created["metadata"]["uid"])
        self.k.objects[resource]["metadata"]["uid"] = "replacement"
        with self.assertRaises(ContractError):
            self.installer.capture("baseline", True)

    def test_foreign_release_and_expanded_hook_permissions_are_not_adopted(self):
        self.owned.create(self.namespace)
        actual = copy.deepcopy(self.role)
        actual["metadata"]["annotations"] = {"meta.helm.sh/release-name": "other", "meta.helm.sh/release-namespace": "fixture"}
        self.k("create", data=json.dumps(actual).encode())
        with self.assertRaises(ContractError):
            self.installer.capture("baseline", False)
        resource = Resource.from_object(actual)
        self.role["metadata"]["annotations"] = {"helm.sh/hook": "post-install,post-upgrade,post-rollback"}
        self.k.objects[resource]["metadata"]["annotations"] = dict(self.role["metadata"]["annotations"])
        self.k.objects[resource]["rules"][0]["resources"] = ["nodes/proxy"]
        with self.assertRaises(ContractError):
            self.installer.capture("baseline", False)
        self.assertNotIn(resource, self.owned.owned)

    def test_capture_continues_after_conflict_and_cleanup_preserves_namespace(self):
        self.owned.create(self.namespace)
        self.k("create", data=json.dumps(self.role).encode())
        owned_role = copy.deepcopy(self.role)
        owned_role["metadata"] = {"name": "later-owned-role", "annotations": {
            "meta.helm.sh/release-name": RELEASE, "meta.helm.sh/release-namespace": "fixture"}}
        resource = Resource.from_object(owned_role)
        self.installer.desired["baseline"][resource] = owned_role
        self.k("create", data=json.dumps(owned_role).encode())
        with self.assertRaises(ContractError):
            self.installer.capture("baseline", True)
        self.assertIn(resource, self.owned.owned)
        with self.assertRaises(ContractError):
            self.owned.cleanup()
        self.assertNotIn(resource, self.k.objects)
        self.assertIn(Resource.from_object(self.role), self.k.objects)
        self.assertIn(Resource.from_object(self.namespace), self.k.objects)


if __name__ == "__main__":
    unittest.main()
