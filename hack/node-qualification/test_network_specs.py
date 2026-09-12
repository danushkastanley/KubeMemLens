import unittest

from common import ContractError
from network_specs import APP, LABEL, PORT, manifests
from owned_resources import Resource

IMAGE = "registry.example/probe@sha256:" + "a" * 64


class NetworkSpecsTest(unittest.TestCase):
    def setUp(self):
        self.documents = manifests("fixture", IMAGE, ["node-a", "node-b"])

    def test_probe_workloads_have_no_credentials_or_host_access(self):
        pods = [d["spec"]["template"]["spec"] if d["kind"] == "Job" else d["spec"]
                for d in self.documents if d["kind"] in {"Job", "Pod"}]
        self.assertEqual(len(pods), 6)
        for pod in pods:
            self.assertFalse(pod["automountServiceAccountToken"])
            self.assertFalse(pod["hostNetwork"])
            self.assertFalse(pod["hostPID"])
            self.assertEqual(pod["securityContext"]["runAsUser"], 65532)
            self.assertFalse(any("hostPath" in v or "projected" in v or "secret" in v for v in pod.get("volumes", [])))
            for container in pod["containers"]:
                self.assertEqual(container["image"], IMAGE)
                self.assertTrue(container["securityContext"]["readOnlyRootFilesystem"])
                self.assertEqual(container["securityContext"]["capabilities"], {"drop": ["ALL"]})

    def test_ingress_targets_have_a_controller_and_match_the_real_policy(self):
        jobs = [d for d in self.documents if d["kind"] == "Job"]
        ingress = [j for j in jobs if j["spec"]["template"]["metadata"]["labels"][LABEL] == "ingress"]
        self.assertEqual(len(ingress), 2)
        for job in ingress:
            labels = job["spec"]["template"]["metadata"]["labels"]
            self.assertTrue(APP.items() <= labels.items())
            self.assertEqual(job["spec"]["backoffLimit"], 0)
            self.assertEqual(job["spec"]["activeDeadlineSeconds"], 600)

    def test_temporary_allows_are_limited_to_same_namespace_probe_traffic(self):
        policies = [d for d in self.documents if d["kind"] == "NetworkPolicy"]
        self.assertEqual(len(policies), 3)
        for policy in policies:
            self.assertEqual(policy["spec"]["policyTypes"], ["Ingress", "Egress"])
            for direction, peer_key in (("ingress", "from"), ("egress", "to")):
                for rule in policy["spec"][direction]:
                    self.assertEqual(rule["ports"], [{"protocol": "TCP", "port": PORT}])
                    self.assertTrue(all(set(peer) == {"podSelector"} for peer in rule[peer_key]))
        resources = [Resource.from_object(d) for d in self.documents]
        self.assertEqual(len(set(resources)), len(resources))

    def test_missing_or_repeated_nodes_are_rejected(self):
        for nodes in ([], ["node-a"], ["node-a", "node-a"], ["node-a", "../escape"]):
            with self.assertRaises(ContractError):
                manifests("fixture", IMAGE, nodes)


if __name__ == "__main__":
    unittest.main()
