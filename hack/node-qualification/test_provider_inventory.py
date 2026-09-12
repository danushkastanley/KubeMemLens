import copy
import json
import unittest
from types import SimpleNamespace

from common import ContractError, load, privacy
from prepare_provider import REPOSITORY
from provider_inventory import bind_nodes, collect_inventory
from provider_plan import POOL_LABELS
from test_provider_plan import ProviderFixture


def stored(name):
    return json.loads((REPOSITORY / "hack/provider-inventory/fixtures" / name).read_text())


class CurrentInventoryTest(ProviderFixture, unittest.TestCase):
    def setup_case(self, profile_name, inventory_name):
        p = load(REPOSITORY / "hack/node-qualification/profiles" / (profile_name + ".json"))
        c = self.config(p["provider"], inventory_name)
        c["poolName"] = None if p["provider"] == "self-managed" else "private-pool-name"
        c["kubernetesVersion"] = "v1.36.1"
        if p["provider"] == "self-managed":
            fixture = stored("self-containerd-inventory.json")
            nodes = fixture["nodes"]
            responses = {"version -o json": fixture["version"], "get daemonsets": fixture["daemonsets"],
                         "config view": {"clusters": [{"cluster": {"server": "https://self.example.test"}}]}}
        else:
            prefix = profile_name.split("-")[0]
            nodes = stored(prefix + "-nodes.json")
            responses = {"config view": stored(prefix + "-context.json")}
            if prefix == "gke":
                responses.update({"clusters describe": stored("gke-cluster.json"), "node-pools describe": stored("gke-node-pool.json")})
                c["kubernetesVersion"] = "v" + responses["clusters describe"]["currentMasterVersion"]
            elif prefix == "eks":
                responses.update({"describe-cluster": stored("eks-cluster.json"), "describe-nodegroup": stored("eks-node-group.json"), "describe-addon": stored("eks-vpc-cni.json")})
            else:
                pool = stored("aks-node-pool.json"); pool["count"] = 2
                responses.update({"aks show": stored("aks-cluster.json"), "nodepool show": pool})
        nodes["items"] = [copy.deepcopy(nodes["items"][0]) for _ in range(2)]
        for slot, node in enumerate(nodes["items"]):
            node["metadata"].update(name="private-" + str(slot), uid="uid-" + str(slot))
            if p["provider"] in POOL_LABELS:
                node["metadata"]["labels"][POOL_LABELS[p["provider"]]] = c["poolName"]
                node.setdefault("spec", {})["providerID"] = {"gke-standard": "gce://", "eks-managed-nodes": "aws://", "aks-node-pools": "azure://"}[p["provider"]] + "private-" + str(slot)
            node.setdefault("spec", {}).pop("taints", None)
            node["status"]["nodeInfo"].update(kubeletVersion=c["kubernetesVersion"], kernelVersion="6.12.10", architecture="amd64")
            node["status"]["conditions"] = [{"type": "Ready", "status": "True"}]
            node["status"]["addresses"] = [{"type": "InternalIP", "address": "10.0.1." + str(slot + 2)}]
        responses["get nodes"] = nodes
        calls = []
        def run(args, **kwargs):
            calls.append((args, kwargs))
            for marker, response in responses.items():
                if marker in " ".join(args):
                    return json.dumps(response)
            raise AssertionError("unexpected inventory command")
        bundle = SimpleNamespace(profile=p, configuration=c, plan={"qualificationToolCommit": self.commit})
        return bundle, run, calls, nodes

    def test_all_four_profiles_use_fresh_inventory_without_granting_support(self):
        for profile, inventory in (("gke-standard", "gke-cos-containerd-amd64"),
                                   ("eks-managed-linux", "eks-al2023-containerd-amd64"),
                                   ("aks-linux", "aks-ubuntu-containerd-amd64"),
                                   ("self-managed-linux", "self-managed-containerd")):
            with self.subTest(profile=profile):
                bundle, run, calls, _ = self.setup_case(profile, inventory)
                receipt, binding = collect_inventory(bundle, run)
                privacy(receipt)
                self.assertNotIn("qualified", receipt)
                self.assertEqual(len(binding["nodes"]), 2)
                self.assertNotIn("private-", json.dumps(receipt))
                for args, kwargs in calls:
                    if args[0] == "kubectl":
                        self.assertEqual(args[1:3], ["--kubeconfig", bundle.configuration["kubeconfigPath"]])
                    self.assertEqual(kwargs["environment"]["QUALIFY_CONTEXT"], bundle.configuration["context"])
        legacy = load(REPOSITORY / "hack/provider-profiles/aks-ubuntu-containerd-amd64.json")
        self.assertEqual(legacy["expectedOutcome"], "unsupported")

    def test_wrong_pool_route_count_or_node_readiness_fails_binding(self):
        bundle, run, _, nodes = self.setup_case("gke-standard", "gke-cos-containerd-amd64")
        receipt, _ = collect_inventory(bundle, run)
        inventory = load(REPOSITORY / "hack/provider-profiles/gke-cos-containerd-amd64.json")
        for scenario in ("count", "pool", "route", "not ready", "taint", "runtime", "duplicate uid", "duplicate instance"):
            with self.subTest(scenario=scenario):
                changed = copy.deepcopy(nodes)
                node = changed["items"][1]
                if scenario == "count":
                    changed["items"].pop()
                elif scenario == "pool":
                    node["metadata"]["labels"]["cloud.google.com/gke-nodepool"] = "other"
                elif scenario == "route":
                    node["status"]["addresses"][0]["address"] = "10.0.9.9"
                elif scenario == "not ready":
                    node["status"]["conditions"][0]["status"] = "False"
                elif scenario == "taint":
                    node["spec"]["taints"] = [{"key": "dedicated", "effect": "NoSchedule"}]
                elif scenario == "runtime":
                    node["status"]["nodeInfo"]["kernelVersion"] = "different"
                elif scenario == "duplicate uid":
                    node["metadata"]["uid"] = changed["items"][0]["metadata"]["uid"]
                else:
                    node["spec"]["providerID"] = changed["items"][0]["spec"]["providerID"]
                with self.assertRaises(ContractError):
                    bind_nodes(bundle.profile, bundle.configuration, inventory, receipt, changed)

    def test_insecure_context_fails_before_provider_commands(self):
        bundle, _, _, _ = self.setup_case("gke-standard", "gke-cos-containerd-amd64")
        calls = []
        def run(args, **kwargs):
            calls.append(args)
            return json.dumps({"clusters": [{"cluster": {"server": "https://fixture", "insecure-skip-tls-verify": True}}]})
        with self.assertRaisesRegex(ContractError, "verified Kubernetes API TLS"):
            collect_inventory(bundle, run)
        self.assertEqual(len(calls), 1)
        self.assertIn("config", calls[0])


if __name__ == "__main__":
    unittest.main()
