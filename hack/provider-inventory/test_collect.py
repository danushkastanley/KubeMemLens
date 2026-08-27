#!/usr/bin/env python3

import copy
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

import collect  # noqa: E402


def fixture(name):
    return json.loads((ROOT / "fixtures" / name).read_text(encoding="utf-8"))


class FixtureCommandRunner:
    def __init__(self, responses):
        self.responses = responses
        self.calls = []

    def __call__(self, command, **_kwargs):
        self.calls.append(command)
        command_text = " ".join(command)
        for marker, response in self.responses.items():
            if marker in command_text:
                return subprocess.CompletedProcess(command, 0, json.dumps(response), "")
        return subprocess.CompletedProcess(command, 1, "", "unhandled private selector")


class ProviderInventoryTests(unittest.TestCase):
    def profile(self, name):
        return collect.load_canonical_profile(collect.PROFILES / f"{name}.json")

    def gke_runner(self, cluster=None, pool=None, context=None, nodes=None):
        delegate = FixtureCommandRunner({
            "clusters describe": cluster or fixture("gke-cluster.json"),
            "node-pools describe": pool or fixture("gke-node-pool.json"),
            "config view": context or fixture("gke-context.json"),
            "get nodes": nodes or fixture("gke-nodes.json"),
        })
        return Mock(side_effect=delegate), delegate

    def eks_runner(self, cluster=None, group=None, addon=None, context=None, nodes=None):
        delegate = FixtureCommandRunner({
            "describe-cluster": cluster or fixture("eks-cluster.json"),
            "describe-nodegroup": group or fixture("eks-node-group.json"),
            "describe-addon": addon or fixture("eks-vpc-cni.json"),
            "config view": context or fixture("eks-context.json"),
            "get nodes": nodes or fixture("eks-nodes.json"),
        })
        return Mock(side_effect=delegate), delegate

    def aks_runner(self, cluster=None, pool=None, context=None, nodes=None):
        delegate = FixtureCommandRunner({
            "aks show": cluster or fixture("aks-cluster.json"),
            "nodepool show": pool or fixture("aks-node-pool.json"),
            "config view": context or fixture("aks-context.json"),
            "get nodes": nodes or fixture("aks-nodes.json"),
        })
        return Mock(side_effect=delegate), delegate

    def self_runner(self, inventory):
        delegate = FixtureCommandRunner({
            "version -o json": inventory["version"],
            "get nodes": inventory["nodes"],
            "get daemonsets": inventory["daemonsets"],
        })
        return Mock(side_effect=delegate), delegate

    def test_gke_standard_receipt_uses_sanitised_owned_inventory(self):
        environment = {
            "QUALIFY_CONTEXT": "private-context",
            "QUALIFY_GKE_PROJECT": "private-project-42",
            "QUALIFY_GKE_LOCATION": "europe-west1",
            "QUALIFY_GKE_CLUSTER": "private-cluster-name",
            "QUALIFY_GKE_NODE_POOL": "private-pool-name",
        }
        runner, delegate = self.gke_runner()
        receipt = collect.collect_receipt(
            self.profile("gke-cos-containerd-amd64"), environment, runner,
            observed_at="2026-08-26T12:00:00Z",
        )
        self.assertEqual(receipt["provider"], "gke-standard")
        self.assertEqual(receipt["nodeImage"], "COS_CONTAINERD")
        self.assertEqual(receipt["cniName"], "GKE Dataplane V2")
        self.assertEqual(len(delegate.calls), 4)
        kubectl_calls = [call for call in delegate.calls if call[0] == "kubectl"]
        self.assertEqual(len(kubectl_calls), 2)
        self.assertTrue(all(call[1:3] == ["--context", environment["QUALIFY_CONTEXT"]]
                            for call in kubectl_calls))
        collect.validate_receipt(receipt)
        encoded = json.dumps(receipt, sort_keys=True)
        for forbidden in environment.values():
            self.assertNotIn(forbidden, encoded)
        for forbidden in ("203.0.113.10", "gke-node-1", "private-zone", "private-instance"):
            self.assertNotIn(forbidden, encoded)
        self.assertNotIn("autopilot", encoded)

    def test_gke_ubuntu_profile_requires_ubuntu_image_family(self):
        pool = fixture("gke-node-pool.json")
        pool["config"]["imageType"] = "UBUNTU_CONTAINERD"
        runner, _ = self.gke_runner(pool=pool, nodes=fixture("gke-ubuntu-nodes.json"))
        receipt = collect.collect_receipt(
            self.profile("gke-ubuntu-containerd-amd64"), self.gke_environment(), runner,
            observed_at="2026-08-26T12:00:00Z",
        )
        self.assertEqual(receipt["nodeImage"], "UBUNTU_CONTAINERD")

    def test_eks_receipt_includes_ami_release_and_cni_configuration(self):
        environment = {
            "QUALIFY_CONTEXT": "arn:aws:eks:eu-west-2:123456789012:cluster/private-eks-name",
            "QUALIFY_EKS_REGION": "eu-west-2",
            "QUALIFY_EKS_CLUSTER": "private-eks-name",
            "QUALIFY_EKS_NODEGROUP": "private-group-name",
        }
        runner, delegate = self.eks_runner()
        receipt = collect.collect_receipt(
            self.profile("eks-al2023-containerd-amd64"), environment, runner,
            observed_at="2026-08-26T12:00:00Z",
        )
        self.assertEqual(receipt["provider"], "eks-managed-nodes")
        self.assertEqual(receipt["nodeImage"], "AL2023_x86_64_STANDARD@1.36.1-20260820")
        self.assertEqual(receipt["cniName"], "Amazon VPC CNI v1.20.0-eksbuild.1 network-policy=enabled")
        self.assertEqual(len(delegate.calls), 5)
        encoded = json.dumps(receipt)
        self.assertNotIn("private-eks-name", encoded)
        self.assertNotIn("configurationValues", encoded)

    def test_derived_inventory_must_match_canonical_receipt_patterns(self):
        group = fixture("eks-node-group.json")
        group["nodegroup"]["releaseVersion"] = "mutable-release"
        runner, _ = self.eks_runner(group=group)
        with self.assertRaisesRegex(collect.ReceiptError, "node image"):
            collect.collect_receipt(
                self.profile("eks-al2023-containerd-amd64"), self.eks_environment(), runner,
            )

    def test_reclassified_aks_profile_is_not_collectable_as_a_supported_row(self):
        profile_path = collect.PROFILES / "aks-ubuntu-containerd-amd64.json"
        with self.assertRaisesRegex(collect.ReceiptError, "no provider inventory driver"):
            collect.load_canonical_profile(profile_path)
        profile = json.loads(profile_path.read_text(encoding="utf-8"))
        runner, delegate = self.aks_runner()
        with self.assertRaisesRegex(collect.ReceiptError, "not an active supported"):
            collect.collect_receipt(profile, self.aks_environment(), runner)
        self.assertEqual(delegate.calls, [])
        collect.validate_receipt(fixture("aks-requestheader-provider-inventory.json"))

    def test_reclassified_aks_inventory_driver_remains_strict(self):
        profile = json.loads(
            (collect.PROFILES / "aks-ubuntu-containerd-amd64.json").read_text(encoding="utf-8"),
        )
        runner, delegate = self.aks_runner()
        provider, image, cni, version, proof = collect.collect_aks(
            profile, self.aks_environment(), runner,
        )
        self.assertEqual(provider, "aks-node-pools")
        self.assertTrue(image.startswith("AKSUbuntu-2404"))
        self.assertEqual(cni, "Azure CNI Cilium")
        self.assertEqual(version, "1.36.1")
        self.assertEqual(proof, "az:control-plane+node-pool")
        self.assertEqual(len(delegate.calls), 4)

    def test_reclassified_aks_inventory_requires_fixed_vmss_pool(self):
        profile = json.loads(
            (collect.PROFILES / "aks-ubuntu-containerd-amd64.json").read_text(encoding="utf-8"),
        )
        cases = (
            ("autoscaling", "enableAutoScaling", True, "fixed three-Node pool"),
            ("count", "count", 2, "fixed three-Node pool"),
            ("pool type", "typePropertiesType", "AvailabilitySet", "managed Ubuntu Linux row"),
        )
        for name, field, value, message in cases:
            with self.subTest(name=name):
                pool = fixture("aks-node-pool.json")
                pool[field] = value
                runner, _ = self.aks_runner(pool=pool)
                with self.assertRaisesRegex(collect.ReceiptError, message):
                    collect.collect_aks(profile, self.aks_environment(), runner)

    def test_reclassified_aks_inventory_accepts_exact_legacy_pool_type(self):
        profile = json.loads(
            (collect.PROFILES / "aks-ubuntu-containerd-amd64.json").read_text(encoding="utf-8"),
        )
        pool = fixture("aks-node-pool.json")
        pool.pop("typePropertiesType")
        pool["type"] = "VirtualMachineScaleSets"
        runner, _ = self.aks_runner(pool=pool)
        self.assertEqual(
            collect.collect_aks(profile, self.aks_environment(), runner)[0],
            "aks-node-pools",
        )

    def test_reclassified_aks_inventory_keeps_context_pool_and_cni_binding(self):
        profile = json.loads(
            (collect.PROFILES / "aks-ubuntu-containerd-amd64.json").read_text(encoding="utf-8"),
        )
        cluster = fixture("aks-cluster.json")
        cluster["networkProfile"]["networkPlugin"] = "kubenet"
        runner, _ = self.aks_runner(cluster=cluster)
        with self.assertRaisesRegex(collect.ReceiptError, "Azure CNI"):
            collect.collect_aks(profile, self.aks_environment(), runner)
        context = fixture("aks-context.json")
        context["clusters"][0]["cluster"]["server"] = "https://wrong.example.test"
        runner, _ = self.aks_runner(context=context)
        with self.assertRaisesRegex(collect.ReceiptError, "cluster endpoint"):
            collect.collect_aks(profile, self.aks_environment(), runner)
        nodes = fixture("aks-nodes.json")
        nodes["items"][0]["metadata"]["labels"].pop("kubernetes.azure.com/agentpool")
        runner, _ = self.aks_runner(nodes=nodes)
        with self.assertRaisesRegex(collect.ReceiptError, "no live Nodes"):
            collect.collect_aks(profile, self.aks_environment(), runner)

    def test_self_managed_profiles_use_live_bounded_kubectl_inventory(self):
        cases = (
            ("self-managed-containerd", "self-containerd-inventory.json", "Cilium v1.18.1"),
            ("self-managed-crio-amd64", "self-crio-inventory.json", "Calico v3.30.3"),
        )
        for profile_id, inventory_name, cni in cases:
            with self.subTest(profile=profile_id):
                runner, delegate = self.self_runner(fixture(inventory_name))
                receipt = collect.collect_receipt(
                    self.profile(profile_id), {"QUALIFY_CONTEXT": "private-context"}, runner,
                    observed_at="2026-08-26T12:00:00Z",
                )
                self.assertEqual(receipt["provider"], "self-managed")
                self.assertEqual(receipt["cniName"], cni)
                self.assertNotIn("/", receipt["nodeImage"])
                self.assertEqual(len(delegate.calls), 3)

    def test_self_managed_bounded_kubectl_observation_uses_three_calls(self):
        inventory = fixture("self-containerd-inventory.json")
        runner, delegate = self.self_runner(inventory)
        receipt = collect.collect_receipt(
            self.profile("self-managed-containerd"), {"QUALIFY_CONTEXT": "private-context"}, runner,
            observed_at="2026-08-26T12:00:00Z",
        )
        self.assertEqual(receipt["proofSource"], "kubectl:version+nodes+daemonsets")
        self.assertEqual(len(delegate.calls), 3)
        self.assertNotIn("private-context", json.dumps(receipt))

    def test_wrong_provider_mode_and_image_are_rejected(self):
        cluster = fixture("gke-cluster.json")
        cluster["autopilot"]["enabled"] = True
        runner, _ = self.gke_runner(cluster=cluster)
        with self.assertRaisesRegex(collect.ReceiptError, "Standard"):
            collect.collect_receipt(self.profile("gke-cos-containerd-amd64"), self.gke_environment(), runner)
        pool = fixture("gke-node-pool.json")
        pool["config"]["imageType"] = "UBUNTU_CONTAINERD"
        runner, _ = self.gke_runner(pool=pool)
        with self.assertRaisesRegex(collect.ReceiptError, "image family"):
            collect.collect_receipt(self.profile("gke-cos-containerd-amd64"), self.gke_environment(), runner)

    def test_wrong_cni_is_rejected_for_each_managed_provider(self):
        gke = fixture("gke-cluster.json")
        gke["networkConfig"]["datapathProvider"] = "DATAPATH_PROVIDER_UNSPECIFIED"
        gke["networkPolicy"]["enabled"] = False
        runner, _ = self.gke_runner(cluster=gke)
        with self.assertRaisesRegex(collect.ReceiptError, "supported CNI"):
            collect.collect_receipt(self.profile("gke-cos-containerd-amd64"), self.gke_environment(), runner)

        addon = fixture("eks-vpc-cni.json")
        addon["addon"]["configurationValues"] = '{"enableNetworkPolicy":"false"}'
        runner, _ = self.eks_runner(addon=addon)
        with self.assertRaisesRegex(collect.ReceiptError, "not enabled"):
            collect.collect_receipt(self.profile("eks-al2023-containerd-amd64"), self.eks_environment(), runner)

    def test_managed_profiles_require_the_live_context(self):
        environment = self.gke_environment()
        environment.pop("QUALIFY_CONTEXT")
        runner, _ = self.gke_runner()
        with self.assertRaisesRegex(collect.ReceiptError, "QUALIFY_CONTEXT"):
            collect.collect_receipt(self.profile("gke-cos-containerd-amd64"), environment, runner)

    def test_each_managed_context_must_match_its_provider_endpoint(self):
        cases = (
            ("gke-cos-containerd-amd64", self.gke_environment, self.gke_runner, "gke-context.json"),
            ("eks-al2023-containerd-amd64", self.eks_environment, self.eks_runner, "eks-context.json"),
        )
        for profile_id, environment, runner_factory, context_fixture in cases:
            with self.subTest(profile=profile_id):
                context = fixture(context_fixture)
                context["clusters"][0]["cluster"]["server"] = "https://wrong.example.test"
                runner, _ = runner_factory(context=context)
                with self.assertRaisesRegex(collect.ReceiptError, "cluster endpoint"):
                    collect.collect_receipt(self.profile(profile_id), environment(), runner)

    def test_each_managed_pool_requires_its_canonical_live_label(self):
        cases = (
            ("gke-cos-containerd-amd64", self.gke_environment, self.gke_runner, "gke-nodes.json"),
            ("eks-al2023-containerd-amd64", self.eks_environment, self.eks_runner, "eks-nodes.json"),
        )
        for profile_id, environment, runner_factory, nodes_fixture in cases:
            with self.subTest(profile=profile_id):
                nodes = fixture(nodes_fixture)
                labels = nodes["items"][0]["metadata"]["labels"]
                pool_label = next(key for key in labels if key != "kubernetes.io/os")
                labels[pool_label] = "different-private-pool"
                runner, _ = runner_factory(nodes=nodes)
                with self.assertRaisesRegex(collect.ReceiptError, "no live Nodes"):
                    collect.collect_receipt(self.profile(profile_id), environment(), runner)

    def test_selected_provider_pool_must_have_matching_live_linux_nodes(self):
        for name, changed, message in (
            ("missing", {"items": []}, "no live Nodes"),
            ("runtime", fixture("gke-nodes.json"), "runtime profile"),
            ("architecture", fixture("gke-nodes.json"), "runtime profile"),
            ("image", fixture("gke-nodes.json"), "runtime profile"),
            ("windows", fixture("gke-nodes.json"), "all-Linux"),
        ):
            nodes = copy.deepcopy(changed)
            if name == "runtime":
                nodes["items"][0]["status"]["nodeInfo"]["containerRuntimeVersion"] = "cri-o://1.36.1"
            elif name == "architecture":
                nodes["items"][0]["status"]["nodeInfo"]["architecture"] = "arm64"
            elif name == "image":
                nodes["items"][0]["status"]["nodeInfo"]["osImage"] = "Ubuntu 24.04.2 LTS"
            elif name == "windows":
                nodes["items"][0]["metadata"]["labels"]["kubernetes.io/os"] = "windows"
            with self.subTest(case=name):
                runner, _ = self.gke_runner(nodes=nodes)
                with self.assertRaisesRegex(collect.ReceiptError, message):
                    collect.collect_receipt(
                        self.profile("gke-cos-containerd-amd64"), self.gke_environment(), runner,
                    )

    def test_self_managed_wrong_runtime_and_cni_are_rejected(self):
        inventory = fixture("self-containerd-inventory.json")
        inventory["nodes"]["items"][0]["status"]["nodeInfo"]["containerRuntimeVersion"] = "cri-o://1.36.1"
        runner, _ = self.self_runner(inventory)
        with self.assertRaisesRegex(collect.ReceiptError, "runtime"):
            collect.collect_receipt(
                self.profile("self-managed-containerd"), {"QUALIFY_CONTEXT": "private-context"}, runner,
            )
        inventory = fixture("self-containerd-inventory.json")
        inventory["daemonsets"]["items"][0]["metadata"]["name"] = "kube-flannel-ds"
        runner, _ = self.self_runner(inventory)
        with self.assertRaisesRegex(collect.ReceiptError, "supported CNI"):
            collect.collect_receipt(
                self.profile("self-managed-containerd"), {"QUALIFY_CONTEXT": "private-context"}, runner,
            )

    def test_self_managed_inventory_rejects_managed_provider_identity(self):
        for identity in ("gce://private", "aws://private", "azure://private"):
            inventory = fixture("self-containerd-inventory.json")
            inventory["nodes"]["items"][0].setdefault("spec", {})["providerID"] = identity
            runner, _ = self.self_runner(inventory)
            with self.subTest(identity=identity), self.assertRaisesRegex(
                    collect.ReceiptError, "managed-provider"):
                collect.collect_receipt(
                    self.profile("self-managed-containerd"),
                    {"QUALIFY_CONTEXT": "private-context"}, runner,
                )

    def test_noncanonical_profile_content_is_rejected(self):
        profile = copy.deepcopy(self.profile("gke-cos-containerd-amd64"))
        profile["requalificationDays"] = 91
        profile["profileDigest"] = collect.canonical_digest(profile, "profileDigest")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "profile.json"
            path.write_text(json.dumps(profile), encoding="utf-8")
            with self.assertRaisesRegex(collect.ReceiptError, "checked-in canonical"):
                collect.load_canonical_profile(path)

    def test_receipt_shape_digest_and_forbidden_output_are_strict(self):
        runner, _ = self.gke_runner()
        receipt = collect.collect_receipt(
            self.profile("gke-cos-containerd-amd64"), self.gke_environment(), runner,
            observed_at="2026-08-26T12:00:00Z",
        )
        self.assertTrue(all(receipt["providerChecks"].values()))
        changed = copy.deepcopy(receipt)
        changed["clusterName"] = "private"
        with self.assertRaisesRegex(collect.ReceiptError, "receipt fields"):
            collect.validate_receipt(changed)
        changed = copy.deepcopy(receipt)
        changed["nodeImage"] = "COS_CONTAINERD_CHANGED"
        with self.assertRaisesRegex(collect.ReceiptError, "receiptDigest"):
            collect.validate_receipt(changed)
        changed = copy.deepcopy(receipt)
        changed["observedAt"] = 1
        with self.assertRaisesRegex(collect.ReceiptError, "observedAt"):
            collect.validate_receipt(changed)
        changed = copy.deepcopy(receipt)
        changed["providerChecks"].pop("contextBinding")
        with self.assertRaisesRegex(collect.ReceiptError, "providerChecks"):
            collect.validate_receipt(changed)
        forbidden_keys = {"cluster", "nodegroup", "project", "subscription", "resourceGroup", "rawResponse"}
        self.assertTrue(forbidden_keys.isdisjoint(receipt))

        legacy = copy.deepcopy(receipt)
        legacy["schemaVersion"] = 1
        legacy.pop("qualificationToolCommit")
        legacy["receiptDigest"] = collect.canonical_digest(legacy, "receiptDigest")
        collect.validate_receipt(legacy)

        changed = copy.deepcopy(receipt)
        changed["qualificationToolCommit"] = "not-a-commit"
        changed["receiptDigest"] = collect.canonical_digest(changed, "receiptDigest")
        with self.assertRaisesRegex(collect.ReceiptError, "qualificationToolCommit"):
            collect.validate_receipt(changed)

    def test_production_tool_binding_requires_a_clean_checkout(self):
        runner = Mock(side_effect=[
            subprocess.CompletedProcess(["git"], 0, "5" * 40 + "\n", ""),
            subprocess.CompletedProcess(["git"], 0, " M hack/provider-inventory/collect.py\n", ""),
        ])
        with self.assertRaisesRegex(collect.ReceiptError, "must be clean"):
            collect.current_tool_commit(runner, require_clean=True)

    def test_command_errors_do_not_echo_private_selectors_or_raw_stderr(self):
        runner = Mock(return_value=subprocess.CompletedProcess(
            ["gcloud"], 1, "", "private-project-42 failed with internal response",
        ))
        with self.assertRaises(collect.ReceiptError) as raised:
            collect.collect_receipt(
                self.profile("gke-cos-containerd-amd64"), self.gke_environment(), runner,
            )
        message = str(raised.exception)
        self.assertNotIn("private-project-42", message)
        self.assertNotIn("internal response", message)

    @staticmethod
    def gke_environment():
        return {
            "QUALIFY_CONTEXT": "private-context",
            "QUALIFY_GKE_PROJECT": "private-project-42",
            "QUALIFY_GKE_LOCATION": "europe-west1",
            "QUALIFY_GKE_CLUSTER": "private-cluster-name",
            "QUALIFY_GKE_NODE_POOL": "private-pool-name",
        }

    @staticmethod
    def eks_environment():
        return {
            "QUALIFY_CONTEXT": "arn:aws:eks:eu-west-2:123456789012:cluster/private-eks-name",
            "QUALIFY_EKS_REGION": "eu-west-2",
            "QUALIFY_EKS_CLUSTER": "private-eks-name",
            "QUALIFY_EKS_NODEGROUP": "private-group-name",
        }

    @staticmethod
    def aks_environment():
        return {
            "QUALIFY_CONTEXT": "private-context",
            "QUALIFY_AKS_SUBSCRIPTION": "00000000-1111-2222-3333-444444444444",
            "QUALIFY_AKS_RESOURCE_GROUP": "private-resource-group",
            "QUALIFY_AKS_CLUSTER": "private-aks-name",
            "QUALIFY_AKS_NODE_POOL": "userpool",
        }


if __name__ == "__main__":
    unittest.main()
