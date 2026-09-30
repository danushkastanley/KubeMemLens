import copy
import json
import unittest
from types import SimpleNamespace

from common import ContractError
from network_probes import NetworkChecks, controlled_pod
from owned_resources import Resource


class NetworkIdentityTest(unittest.TestCase):
    def setUp(self):
        self.parent = {"metadata": {"name": "node-qualification-network-control-0",
                                    "namespace": "fixture", "uid": "job-uid"}}
        self.pod = {"metadata": {"name": "generated-pod", "namespace": "fixture", "uid": "pod-uid",
                                 "ownerReferences": [{"apiVersion": "batch/v1", "kind": "Job",
                                                      "name": self.parent["metadata"]["name"],
                                                      "uid": "job-uid", "controller": True,
                                                      "blockOwnerDeletion": True}]},
                    "spec": {"nodeName": "node-a", "hostNetwork": False},
                    "status": {"podIP": "10.0.1.2"}}

    def test_generated_child_is_bound_to_the_actual_job(self):
        self.assertEqual(controlled_pod(self.parent, [self.pod], "node-a"), self.pod)
        self.assertIsNone(controlled_pod(self.parent, [], "node-a"))

    def test_same_label_or_name_cannot_substitute_for_controller_identity(self):
        for field, value in (("uid", "replacement-job"), ("name", "other-job"), ("kind", "ReplicaSet"),
                             ("apiVersion", "apps/v1"), ("controller", False)):
            with self.subTest(field=field):
                pod = copy.deepcopy(self.pod)
                pod["metadata"]["ownerReferences"][0][field] = value
                with self.assertRaisesRegex(ContractError, "different controller"):
                    controlled_pod(self.parent, [pod], "node-a")
        for owners in ([], self.pod["metadata"]["ownerReferences"] * 2):
            pod = copy.deepcopy(self.pod)
            pod["metadata"]["ownerReferences"] = owners
            with self.assertRaises(ContractError):
                controlled_pod(self.parent, [pod], "node-a")

    def test_namespace_node_and_host_network_must_match(self):
        for section, field, value in (("metadata", "namespace", "foreign"), ("spec", "nodeName", "node-b"),
                                       ("spec", "hostNetwork", True)):
            with self.subTest(field=field):
                pod = copy.deepcopy(self.pod)
                pod[section][field] = value
                with self.assertRaises(ContractError):
                    controlled_pod(self.parent, [pod], "node-a")

    def test_multiple_live_children_fail_instead_of_selecting_one(self):
        with self.assertRaisesRegex(ContractError, "multiple live Pods"):
            controlled_pod(self.parent, [self.pod, copy.deepcopy(self.pod)], "node-a")
        terminating = copy.deepcopy(self.pod)
        terminating["metadata"]["deletionTimestamp"] = "2026-10-01T00:00:00Z"
        self.assertIsNone(controlled_pod(self.parent, [terminating], "node-a"))
        self.assertEqual(controlled_pod(self.parent, [terminating, self.pod], "node-a"), self.pod)

    def test_each_probe_rechecks_parent_and_original_child(self):
        checked = []

        def verify(resource):
            checked.append(resource)
            return self.parent

        checks = object.__new__(NetworkChecks)
        checks.namespace, checks.nodes = "fixture", ["node-a", "node-b"]
        checks.owner = SimpleNamespace(verify=verify)
        checks.pods = {("control", 0): copy.deepcopy(self.pod)}
        checks.k = lambda *_: json.dumps(self.pod)
        self.assertEqual(checks.verify_pod("control", 0), self.pod)
        self.assertEqual(checked, [Resource("batch/v1", "Job", self.parent["metadata"]["name"], "fixture")])
        self.pod["metadata"]["uid"] = "replacement-pod"
        with self.assertRaisesRegex(ContractError, "identity or address changed"):
            checks.verify_pod("control", 0)
        self.pod["metadata"]["uid"] = "pod-uid"
        self.parent["metadata"]["uid"] = "replacement-job"
        with self.assertRaisesRegex(ContractError, "different controller"):
            checks.verify_pod("control", 0)


if __name__ == "__main__":
    unittest.main()
