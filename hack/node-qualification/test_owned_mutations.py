import copy
import json
import tempfile
import unittest
from pathlib import Path

from common import ContractError
from owned_mutations import restart_workload, suspended_network_rule, suspended_producer_access
from owned_resources import OwnedResources, Resource
from test_owned_resources import Kubernetes


class VersionedKubernetes(Kubernetes):
    def __init__(self):
        super().__init__()
        self.before_patch = lambda: None
        self.patch_count = 0

    def __call__(self, *args, data=None):
        if args[0] != "patch":
            raw = super().__call__(*args, data=data)
            if args[0] == "create":
                document = json.loads(raw)
                document["metadata"]["resourceVersion"] = "1"
                self.objects[Resource.from_object(document)] = document
                return json.dumps(document)
            return raw
        self.before_patch()
        resource = next(r for r in self.objects if r.kind == args[1] and r.name == args[2])
        candidate = copy.deepcopy(self.objects[resource])
        for operation in json.loads(data):
            keys = operation["path"].strip("/").split("/")
            parent = candidate
            for key in keys[:-1]:
                parent = parent[key]
            if operation["op"] == "test":
                if parent.get(keys[-1]) != operation["value"]:
                    raise ContractError("precondition failed")
            else:
                parent[keys[-1]] = operation["value"]
        candidate["metadata"]["resourceVersion"] = str(int(candidate["metadata"]["resourceVersion"]) + 1)
        self.objects[resource] = candidate
        self.patch_count += 1
        return json.dumps(candidate)


class MutationTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.k = VersionedKubernetes()
        self.owner = OwnedResources(self.k, Path(temporary.name) / "ownership")
        self.binding = {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
                        "metadata": {"name": "kube-memlens-node-context-producer"},
                        "roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole",
                                    "name": "kube-memlens-node-context-producer"},
                        "subjects": [{"kind": "ServiceAccount", "name": "kube-memlens-node-context", "namespace": "fixture"}]}
        self.resource = Resource.from_object(self.binding)
        self.owner.create(self.binding)

    def test_access_is_restored_even_after_observation_error(self):
        original = self.owner.verify(self.resource)
        with self.assertRaisesRegex(ValueError, "observation"):
            with suspended_producer_access(self.owner, "fixture"):
                self.assertEqual(self.owner.verify(self.resource)["subjects"], [])
                raise ValueError("observation failed")
        restored = self.owner.verify(self.resource)
        self.assertEqual(restored["subjects"], original["subjects"])
        self.assertEqual(restored["metadata"]["uid"], original["metadata"]["uid"])
        self.assertEqual(self.k.patch_count, 2)

    def test_intervening_edit_is_preserved_during_restoration(self):
        with self.assertRaises(ContractError):
            with suspended_producer_access(self.owner, "fixture"):
                self.k.objects[self.resource]["metadata"]["resourceVersion"] = "900"
                self.k.objects[self.resource]["subjects"] = [{"kind": "Group", "name": "operator-owned"}]
        self.assertEqual(self.k.objects[self.resource]["subjects"], [{"kind": "Group", "name": "operator-owned"}])
        self.assertEqual(self.k.patch_count, 1)

    def test_replacement_between_read_and_mutation_is_untouched(self):
        def replace():
            self.k.objects[self.resource]["metadata"]["uid"] = "replacement"
        self.k.before_patch = replace
        with self.assertRaises(ContractError):
            with suspended_producer_access(self.owner, "fixture"):
                self.fail("must not enter disruption")
        self.assertEqual(self.k.objects[self.resource]["subjects"], self.binding["subjects"])
        self.assertEqual(self.k.patch_count, 0)

    def test_extra_subject_or_different_namespace_is_not_disrupted(self):
        with self.assertRaises(ContractError):
            with suspended_producer_access(self.owner, "different"):
                self.fail("must not enter disruption")
        self.k.objects[self.resource]["subjects"].append({"kind": "Group", "name": "extra"})
        with self.assertRaises(ContractError):
            with suspended_producer_access(self.owner, "fixture"):
                self.fail("must not enter disruption")
        self.assertEqual(self.k.patch_count, 0)

    def test_restart_preserves_existing_template_annotations_and_uid(self):
        manifest = {"apiVersion": "apps/v1", "kind": "DaemonSet", "metadata": {"name": "kube-memlens-agent", "namespace": "fixture"},
                    "spec": {"template": {"metadata": {"annotations": {"existing": "kept"}}}}}
        created = self.owner.create(manifest)
        restarted = restart_workload(self.owner, "fixture", "agent")
        self.assertEqual(restarted["metadata"]["uid"], created["metadata"]["uid"])
        self.assertEqual(restarted["spec"]["template"]["metadata"]["annotations"]["existing"], "kept")
        self.assertIn("kubememlens.io/qualification-restart", restarted["spec"]["template"]["metadata"]["annotations"])

    def test_unowned_workload_and_unsupported_component_are_rejected(self):
        for component in ("collector", "node-context"):
            with self.assertRaises(ContractError):
                restart_workload(self.owner, "fixture", component)
        self.assertEqual(self.k.patch_count, 0)

    def test_network_positive_control_is_restored_without_changing_other_direction(self):
        policy = {"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
                  "metadata": {"name": "node-qualification-network-egress", "namespace": "fixture"},
                  "spec": {"ingress": [], "egress": [{"to": [{"podSelector": {"matchLabels": {"probe": "target"}}}]}]}}
        self.owner.create(policy)
        resource = Resource.from_object(policy)
        with suspended_network_rule(self.owner, resource, "egress"):
            self.assertEqual(self.owner.verify(resource)["spec"], {"ingress": [], "egress": []})
        self.assertEqual(self.owner.verify(resource)["spec"], policy["spec"])
        with self.assertRaises(ContractError):
            with suspended_network_rule(self.owner, resource, "ingress"):
                self.fail("wrong policy direction must not be modified")


if __name__ == "__main__":
    unittest.main()
