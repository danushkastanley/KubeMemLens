#!/usr/bin/env python3

import copy
import contextlib
import io
import json
import stat
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from pathlib import Path


ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[1]
sys.path.insert(0, str(ROOT))

import observe_unsupported as observer  # noqa: E402
import kubectl_dry_run  # noqa: E402
import unsupported_live as live  # noqa: E402


ARTEFACT_BINDING = {"sourceCommit": "4" * 40, "chartDigest": "sha256:" + "2" * 64}


def fixture(name):
    return json.loads((ROOT / "fixtures" / name).read_text(encoding="utf-8"))


class LiveRunner:
    def __init__(self, profile_id):
        self.profile_id = profile_id
        self.source = fixture(f"{profile_id}-observation.json")
        self.calls = []
        self.inputs = []
        self.dry_requests = []

    def dry_run(self, base, namespace, manifest):
        self.dry_requests.append((base, namespace, manifest))
        if "volumes" not in manifest["spec"]:
            return 201, {"apiVersion": "v1", "kind": "Pod"}
        return 403, self.source["admission"]["targetStatus"] | {
            "apiVersion": "v1", "kind": "Status", "status": "Failure",
        }

    def result(self, command, value, returncode=0, stderr=""):
        output = value if isinstance(value, str) else json.dumps(value)
        return subprocess.CompletedProcess(command, returncode, output, stderr)

    def __call__(self, command, **kwargs):
        self.calls.append(command)
        self.inputs.append(kwargs.get("input"))
        text = " ".join(command)
        if "config view --minify" in text:
            provider = "gke" if self.profile_id == "gke-autopilot" else \
                "eks" if self.profile_id == "eks-fargate" else "aks"
            return self.result(command, fixture(f"{provider}-context.json"))
        if "gcloud container clusters describe" in text:
            value = copy.deepcopy(self.source["provider"])
            value["endpoint"] = "203.0.113.10"
            return self.result(command, value)
        if "auth can-i create pods" in text:
            return self.result(command, "yes\n")
        if "aws eks describe-cluster" in text:
            value = copy.deepcopy(self.source["provider"]["cluster"])
            value["endpoint"] = "https://eks.private.example.test"
            return self.result(command, {"cluster": value})
        if "aws eks describe-fargate-profile" in text:
            return self.result(command, {"fargateProfile": self.source["provider"]["fargateProfile"]})
        if "az aks nodepool show" in text:
            return self.result(command, {
                "provisioningState": "Succeeded", "osType": "Windows", "osSku": "Windows2022",
                "nodeImageVersion": "AKSWindows-2022-containerd-20348.4052.250716",
            })
        if "az aks show" in text:
            value = copy.deepcopy(self.source.get("provider", fixture("aks-cluster.json")))
            value["fqdn"] = "aks.private.example.test"
            value["currentKubernetesVersion"] = value.get("currentKubernetesVersion", "1.36.1")
            value.setdefault("networkProfile", {})["networkPlugin"] = "azure"
            value["networkProfile"]["networkPolicy"] = "calico"
            value["networkProfile"]["networkDataplane"] = None
            return self.result(command, value)
        if "version -o json" in text:
            return self.result(command, self.source["version"])
        if "get pods --all-namespaces" in text:
            node_name = "fargate-subject-node"
            return self.result(command, {"apiVersion": "v1", "kind": "PodList", "items": [{
                "metadata": {"labels": {
                    "eks.amazonaws.com/fargate-profile": "private-fargate-profile",
                }},
                "spec": {"nodeName": node_name},
                "status": {"phase": "Running", "conditions": [
                    {"type": "Ready", "status": "True"},
                ]},
            }]})
        if "get nodes" in text:
            nodes = copy.deepcopy(self.source["nodes"])
            if self.profile_id == "eks-fargate":
                nodes["items"][0]["metadata"]["name"] = "fargate-subject-node"
            if self.profile_id == "windows-deep-mode":
                nodes["items"][0].setdefault("spec", {})["providerID"] = "azure:///private-instance"
                nodes["items"][0]["metadata"]["labels"][
                    "kubernetes.azure.com/agentpool"
                ] = "windowspool"
            if self.profile_id == "cgroup-v1":
                nodes["items"][0].setdefault("spec", {}).setdefault("providerID", "")
                nodes["items"][0]["status"]["addresses"] = [
                    {"type": "InternalIP", "address": "192.0.2.10"},
                ]
            return self.result(command, nodes)
        if "get daemonsets -n kube-system" in text:
            return self.result(command, fixture("self-containerd-inventory.json")["daemonsets"])
        if command[0] == "helm":
            rendered = """nodeSelector:\n  kubernetes.io/os: linux\nvolumes:\n  - hostPath:\n      path: /sys/fs/cgroup\n      type: Directory\n"""
            return self.result(command, rendered)
        if command[0] == "ssh":
            return self.result(command, "cgroup-v1\n")
        return self.result(command, "", 1, "unhandled command")


class UnsupportedLiveTests(unittest.TestCase):
    def profile(self, profile_id):
        return observer.load_canonical_profile(observer.PROFILES / f"{profile_id}.json")

    def environment(self, profile_id, directory):
        common = {"QUALIFY_CONTEXT": "private-context"}
        if profile_id == "gke-autopilot":
            return common | {
                "QUALIFY_GKE_PROJECT": "private-project", "QUALIFY_GKE_LOCATION": "europe-west1",
                "QUALIFY_GKE_CLUSTER": "private-cluster",
                "QUALIFY_OBSERVATION_NAMESPACE": "qualification-observation",
            }
        if profile_id == "eks-fargate":
            return common | {
                "QUALIFY_EKS_REGION": "eu-west-2", "QUALIFY_EKS_CLUSTER": "private-cluster",
                "QUALIFY_EKS_FARGATE_PROFILE": "private-fargate-profile",
            }
        if profile_id in {"aks-virtual-nodes", "windows-deep-mode"}:
            values = common | {
                "QUALIFY_AKS_SUBSCRIPTION": "00000000-1111-2222-3333-444444444444",
                "QUALIFY_AKS_RESOURCE_GROUP": "private-group",
                "QUALIFY_AKS_CLUSTER": "private-cluster",
            }
            if profile_id == "windows-deep-mode":
                values["QUALIFY_AKS_WINDOWS_NODE_POOL"] = "windowspool"
            return values
        key = Path(directory) / "id_qualification"
        known_hosts = Path(directory) / "known_hosts"
        key.write_text("private test key", encoding="utf-8")
        key.chmod(0o600)
        known_hosts.write_text("192.0.2.10 test-key", encoding="utf-8")
        return common | {
            "QUALIFY_SSH_USER": "qualification", "QUALIFY_SSH_ADDRESS_TYPE": "InternalIP",
            "QUALIFY_SSH_KEY": str(key), "QUALIFY_SSH_KNOWN_HOSTS": str(known_hosts),
        }

    def receipt(self, profile_id, directory):
        runner = LiveRunner(profile_id)
        profile = self.profile(profile_id)
        source = live.collect_live_source(
            profile_id, self.environment(profile_id, directory), observer.SPECS[profile_id].get("probe"),
            runner=runner, repo_root=REPOSITORY, dry_run=runner.dry_run,
        )
        return observer.build_receipt(
            profile, source, "2026-08-26T12:00:00Z", ARTEFACT_BINDING,
        ), runner

    def test_all_profiles_collect_live_sources_and_create_private_receipts(self):
        with tempfile.TemporaryDirectory() as directory:
            for profile_id in observer.PARSERS:
                with self.subTest(profile=profile_id):
                    receipt, runner = self.receipt(profile_id, directory)
                    observer.validate_unsupported_receipt(self.profile(profile_id), receipt)
                    encoded = json.dumps(receipt)
                    self.assertNotIn("private-", encoded)
                    self.assertNotIn("192.0.2.10", encoded)
                    self.assertTrue(runner.calls)

    def test_gke_uses_exact_auth_and_two_server_dry_runs(self):
        with tempfile.TemporaryDirectory() as directory:
            _, runner = self.receipt("gke-autopilot", directory)
        self.assertEqual(len(runner.dry_requests), 2)
        self.assertTrue(any("auth" in call and "can-i" in call for call in runner.calls))
        self.assertNotEqual(runner.dry_requests[0][2], runner.dry_requests[1][2])

    def test_managed_profiles_bind_cloud_endpoint_to_context(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = LiveRunner("eks-fargate")
            original = runner.__call__

            def mismatched(command, **kwargs):
                result = original(command, **kwargs)
                if "config view --minify" in " ".join(command):
                    value = fixture("eks-context.json")
                    value["clusters"][0]["cluster"]["server"] = "https://wrong.example.test"
                    return runner.result(command, value)
                return result

            with self.assertRaisesRegex(observer.ReceiptError, "provider cluster endpoint"):
                live.collect_live_source(
                    "eks-fargate", self.environment("eks-fargate", directory), None,
                    runner=mismatched, repo_root=REPOSITORY,
                )

    def test_eks_subjects_are_scoped_to_the_selected_fargate_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            _, runner = self.receipt("eks-fargate", directory)
        pod_calls = [" ".join(call) for call in runner.calls if "get" in call and "pods" in call]
        self.assertEqual(len(pod_calls), 1)
        self.assertIn(
            "eks.amazonaws.com/fargate-profile=private-fargate-profile", pod_calls[0],
        )

    def test_windows_requires_current_aks_windows_pool(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = LiveRunner("windows-deep-mode")
            original = runner.__call__

            def linux_pool(command, **kwargs):
                if "az aks nodepool show" in " ".join(command):
                    return runner.result(command, {"provisioningState": "Succeeded", "osType": "Linux",
                                                  "osSku": "Ubuntu", "nodeImageVersion": "Ubuntu"})
                return original(command, **kwargs)

            with self.assertRaisesRegex(observer.ReceiptError, "current Windows pool"):
                live.collect_live_source(
                    "windows-deep-mode", self.environment("windows-deep-mode", directory), None,
                    runner=linux_pool, repo_root=REPOSITORY,
                )

    def test_windows_rejects_linux_only_cilium_network_policy(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = LiveRunner("windows-deep-mode")
            original = runner.__call__

            def cilium_cluster(command, **kwargs):
                if "az aks show" in " ".join(command):
                    response = original(command, **kwargs)
                    provider = json.loads(response.stdout)
                    provider["networkProfile"]["networkPolicy"] = "cilium"
                    provider["networkProfile"]["networkDataplane"] = "cilium"
                    return runner.result(command, provider)
                return original(command, **kwargs)

            with self.assertRaisesRegex(observer.ReceiptError, "Azure CNI with Calico"):
                live.collect_live_source(
                    "windows-deep-mode", self.environment("windows-deep-mode", directory), None,
                    runner=cilium_cluster, repo_root=REPOSITORY,
                )

    def test_windows_records_configured_cni_without_claiming_enforcement(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt, _ = self.receipt("windows-deep-mode", directory)
        self.assertEqual(receipt["environment"]["cniName"], "Azure CNI Calico")
        self.assertFalse(receipt["environment"]["cniEnforced"])

    def test_cgroup_v1_uses_batchmode_ssh_for_every_linux_node(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt, runner = self.receipt("cgroup-v1", directory)
        ssh = [call for call in runner.calls if call[0] == "ssh"]
        self.assertEqual(len(ssh), receipt["unsupportedObservation"]["subjectCount"])
        self.assertTrue(all("BatchMode=yes" in call and "StrictHostKeyChecking=yes" in call for call in ssh))
        self.assertTrue(all(call[-2:] == ["/bin/sh", "-s"] for call in ssh))
        ssh_inputs = [value for call, value in zip(runner.calls, runner.inputs) if call[0] == "ssh"]
        self.assertTrue(all(value == live.CGROUP_V1_COMMAND + "\n" for value in ssh_inputs))

    def test_cgroup_v1_rejects_managed_nodes_and_permissive_keys(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = self.environment("cgroup-v1", directory)
            Path(environment["QUALIFY_SSH_KEY"]).chmod(0o644)
            with self.assertRaisesRegex(observer.ReceiptError, "group or other"):
                live.collect_live_source(
                    "cgroup-v1", environment, None, runner=LiveRunner("cgroup-v1"),
                    repo_root=REPOSITORY,
                )
        with tempfile.TemporaryDirectory() as directory:
            environment = self.environment("cgroup-v1", directory)
            runner = LiveRunner("cgroup-v1")
            runner.source["nodes"]["items"][0].setdefault("spec", {})[
                "providerID"
            ] = "aws:///private-instance"
            with self.assertRaisesRegex(observer.ReceiptError, "self-managed"):
                live.collect_live_source(
                    "cgroup-v1", environment, None, runner=runner, repo_root=REPOSITORY,
                )

    def test_cli_collects_live_and_has_no_source_argument(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "receipt.json"
            environment = self.environment("gke-autopilot", directory)
            arguments = ["--profile", str(observer.PROFILES / "gke-autopilot.json"),
                         "--source-commit", "4" * 40,
                         "--chart-archive", str(Path(directory) / "candidate.tgz"),
                         "--chart-digest", "sha256:" + "2" * 64,
                         "--output", str(output)]
            runner = LiveRunner("gke-autopilot")
            verifier = lambda *_: ARTEFACT_BINDING
            observer.main(arguments, environment, runner, runner.dry_run, verifier)
            mode = stat.S_IMODE(output.stat().st_mode)
            with self.assertRaises(SystemExit), contextlib.redirect_stderr(io.StringIO()):
                observer.main([*arguments, "--source", "private.json"], environment,
                              runner, runner.dry_run, verifier)
        self.assertEqual(mode, 0o600)

    def test_loopback_proxy_returns_exact_success_and_error_objects(self):
        class Process:
            def __init__(self):
                self.stopped = False
                self.stdout = None

            def poll(self):
                return 0 if self.stopped else None

            def terminate(self):
                self.stopped = True

            def wait(self, timeout):
                return 0

        class Response(io.BytesIO):
            def __init__(self, status, value):
                super().__init__(json.dumps(value).encode())
                self.status = status

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

        process = Process()
        popen = lambda *_args, **_kwargs: process
        success = lambda *_args, **_kwargs: Response(201, {"kind": "Pod"})
        code, value = kubectl_dry_run.post_server_dry_run(
            ["kubectl", "--context", "private"], "observation", {"kind": "Pod"},
            popen=popen, opener=success, port_reader=lambda _process: 18443,
        )
        self.assertEqual((code, value["kind"]), (201, "Pod"))
        def denied(*_args, **_kwargs):
            body = json.dumps({"kind": "Status", "reason": "Forbidden", "code": 403}).encode()
            raise urllib.error.HTTPError(
                "http://127.0.0.1", 403, "Forbidden", {}, io.BytesIO(body),
            )
        code, value = kubectl_dry_run.post_server_dry_run(
            ["kubectl", "--context", "private"], "observation", {"kind": "Pod"},
            popen=popen, opener=denied, port_reader=lambda _process: 18443,
        )
        self.assertEqual((code, value["kind"]), (403, "Status"))


if __name__ == "__main__":
    unittest.main()
