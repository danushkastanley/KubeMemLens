import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from common import ContractError, digest, load
from prepare_provider import REPOSITORY, prepare, review_preview
from prepare_provider import local_command
from provider_plan import file_digest, host_routes, validate_config
from provider_source import bind_files
from release.package_chart import package_chart


class ProviderPreparationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="node-provider-preparation-")
        cls.root = Path(cls.temporary.name)
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                        "-keyout", str(cls.root / "key.pem"), "-out", str(cls.root / "ca.pem"),
                        "-subj", "/CN=qualification-test", "-days", "1",
                        "-addext", "basicConstraints=critical,CA:TRUE"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for name in ("cli", "producer"):
            (cls.root / name).write_bytes(b"unexecuted test artefact " + name.encode())
        marker = cls.root / "credential-executed"
        kubeconfig = {"apiVersion": "v1", "kind": "Config", "current-context": "fixture",
                      "clusters": [{"name": "fixture", "cluster": {"server": "https://127.0.0.1:1"}}],
                      "contexts": [{"name": "fixture", "context": {"cluster": "fixture", "user": "fixture"}}],
                      "users": [{"name": "fixture", "user": {"exec": {"apiVersion": "client.authentication.k8s.io/v1",
                                "interactiveMode": "Never", "command": "/bin/sh",
                                "args": ["-c", 'touch "$1"; exit 1', "sh", str(marker)]}}}]}
        (cls.root / "kubeconfig").write_text(json.dumps(kubeconfig))
        package_chart(REPOSITORY / "charts/kube-memlens", cls.root / "chart.tgz", 0)
        cls.commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPOSITORY, text=True).strip()

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def config(self, provider="gke-standard", inventory="gke-cos-containerd-amd64"):
        root = self.root
        return {"schemaVersion": 1, "inventoryProfile": inventory,
                "namespace": "kube-memlens-qualification-fixture", "context": "fixture",
                "kubeconfigPath": str(root / "kubeconfig"), "kubernetesVersion": "v1.37.0",
                "imageRepository": "ghcr.io/example/kube-memlens", "imageDigest": "sha256:" + "a" * 64,
                "sourceCommit": self.commit, "chartArchive": str(root / "chart.tgz"),
                "chartDigest": file_digest(root / "chart.tgz", 20 * 1024 * 1024),
                "cliBinary": str(root / "cli"), "cliDigest": file_digest(root / "cli", 1024),
                "producerBinary": str(root / "producer"), "producerDigest": file_digest(root / "producer", 1024),
                "kubeletCAFile": str(root / "ca.pem"), "kubeletAudience": "https://fixture.invalid",
                "apiServerCIDRs": ["10.0.0.1/32"], "nodeCIDRs": ["10.0.1.2/32", "10.0.1.3/32"],
                "poolName": None if provider == "self-managed" else "fixture-pool"}

    def test_all_provider_bundles_render_privately_without_executing_credentials(self):
        cases = [("gke-standard", "gke-cos-containerd-amd64"),
                 ("eks-managed-linux", "eks-al2023-containerd-amd64"),
                 ("aks-linux", "aks-ubuntu-containerd-amd64"),
                 ("self-managed-linux", "self-managed-containerd")]
        for name, inventory in cases:
            with self.subTest(profile=name):
                p = load(REPOSITORY / "hack/node-qualification/profiles" / (name + ".json"))
                config = self.config(p["provider"], inventory)
                output = self.root / ("proposal-" + name)
                with patch.dict(os.environ, {"KUBECONFIG": config["kubeconfigPath"]}):
                    result = prepare(p, config, output)
                self.assertEqual(result["state"], "prepared-not-approved")
                self.assertFalse(result["qualified"])
                self.assertFalse(result["providerRunStarted"])
                self.assertEqual(result["budgets"], p["budgets"])
                for name, expected in result["files"].items():
                    self.assertEqual("sha256:" + hashlib.sha256((output / name).read_bytes()).hexdigest(), expected)
                self.assertFalse((self.root / "credential-executed").exists())
                self.assertEqual(output.stat().st_mode & 0o777, 0o700)
                for path in output.iterdir():
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                baseline = (output / "baseline.preview.yaml").read_text()
                enabled = (output / "enabled.preview.yaml").read_text()
                self.assertNotIn("name: kube-memlens-node-context-producer", baseline)
                self.assertIn("name: kube-memlens-node-context-producer", enabled)
                self.assertNotIn("nodes/proxy", enabled)
                self.assertIn("insecureSkipTLSVerify: false", enabled)
                self.assertEqual(enabled.count("<generated by Helm at installation>"), 4)
                self.assertIn("REVIEW ONLY", enabled)
                self.assertIn(config["imageDigest"], enabled)
                self.assertIn("10.0.1.2/32", enabled)
                workload = load(output / "workload.json")["spec"]
                pod = workload["template"]["spec"]
                self.assertEqual(workload["replicas"] * len(pod["containers"]), 32)
                self.assertNotIn("nodeName", pod)
                self.assertFalse(pod["automountServiceAccountToken"])
                before = (output / "plan.private.json").read_bytes()
                with self.assertRaises(ContractError):
                    prepare(p, config, output)
                self.assertEqual((output / "plan.private.json").read_bytes(), before)

    def test_malformed_or_overbroad_inputs_fail_before_rendering(self):
        p = load(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json")
        cases = {"namespace": "default", "context": "fixture\nprivate", "schemaVersion": True,
                 "inventoryProfile": "eks-al2023-containerd-amd64", "sourceCommit": "main",
                 "imageRepository": "https://registry.invalid/image", "poolName": None,
                 "nodeCIDRs": ["10.0.1.0/24"], "apiServerCIDRs": ["0.0.0.0/0"],
                 "cliDigest": "sha256:" + "b" * 64, "kubernetesVersion": "v1.35.5"}
        for field, value in cases.items():
            with self.subTest(field=field):
                c = self.config(); c[field] = value
                with self.assertRaises(ValueError):
                    validate_config(p, c)
        c = self.config(); c["approved"] = True
        with self.assertRaises(ContractError):
            validate_config(p, c)

    def test_trust_does_not_copy_private_keys_or_accept_invalid_certificates(self):
        p = load(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json")
        for name, content in (("private-key", (self.root / "ca.pem").read_bytes() + (self.root / "key.pem").read_bytes()),
                              ("invalid-ca", b"-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")):
            path = self.root / name; path.write_bytes(content)
            c = self.config(); c["kubeletCAFile"] = str(path)
            with self.assertRaises(ValueError):
                validate_config(p, c)

    def test_routes_and_preview_redaction_fail_closed(self):
        self.assertEqual(host_routes(["2001:db8::1/128"], 2), ["2001:db8::1/128"])
        for routes in (["127.0.0.1/32"], ["::/128"], ["10.0.0.1/32"] * 2,
                       ["169.254.169.254/32"], ["2001:db8::1%eth0/128"]):
            with self.assertRaises(ValueError):
                host_routes(routes, 2)
        with self.assertRaises(ContractError):
            review_preview(b"unexpected TLS template\n")

    def test_source_binding_rejects_extra_changed_and_symlinked_files(self):
        with tempfile.TemporaryDirectory() as folder:
            repository = Path(folder)
            chart = repository / "charts/kube-memlens"
            chart.mkdir(parents=True)
            source = chart / "Chart.yaml"
            source.write_bytes(b"test")
            listing = b"100644 blob " + b"a" * 40 + b" 4\tcharts/kube-memlens/Chart.yaml\0"
            command = lambda args, _: listing if args[1] == "ls-tree" else b"test"
            bind_files(repository, "a" * 40, "charts/kube-memlens", command)
            source.write_bytes(b"edit")
            with self.assertRaises(ContractError):
                bind_files(repository, "a" * 40, "charts/kube-memlens", command)
            source.write_bytes(b"test")
            extra = chart / "untracked.yaml"; extra.write_bytes(b"extra")
            with self.assertRaises(ContractError):
                bind_files(repository, "a" * 40, "charts/kube-memlens", command)
            extra.unlink()
            source.unlink(); source.symlink_to(repository / "outside")
            with self.assertRaises(ContractError):
                bind_files(repository, "a" * 40, "charts/kube-memlens", command)

    def test_failed_render_never_produces_a_prepared_manifest(self):
        p = load(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json")
        output = self.root / "failed-render"
        def fail_render(args, repository):
            if args[:2] == ["helm", "template"]:
                raise ContractError("render failed")
            return local_command(args, repository)
        with patch("prepare_provider.local_command", side_effect=fail_render):
            with self.assertRaises(ContractError):
                prepare(p, self.config(), output)
        self.assertTrue(output.is_dir())
        self.assertFalse((output / "plan.private.json").exists())

    def test_private_registry_port_and_context_spaces_are_preserved(self):
        p = load(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json")
        c = self.config()
        c.update(imageRepository="registry.example:5000/team/image", context="fixture cluster")
        self.assertEqual(validate_config(p, c)["context"], "fixture cluster")
        c["imageRepository"] = "registry.example:99999/team/image"
        with self.assertRaises(ContractError):
            validate_config(p, c)

    def test_namespace_length_and_canonical_profile_are_enforced(self):
        p = load(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json")
        c = self.config()
        c["namespace"] = "kube-memlens-qualification-" + "a" * 36
        self.assertEqual(len(validate_config(p, c)["namespace"]), 63)
        c["namespace"] += "a"
        with self.assertRaises(ContractError):
            validate_config(p, c)
        p["budgets"]["producerMeanCPUMilli"] += 1
        p["profileDigest"] = digest(p, "profileDigest")
        with self.assertRaisesRegex(ContractError, "canonical checked-in profile"):
            prepare(p, self.config(), self.root / "changed-profile")

    def test_cli_requires_private_configuration_and_returns_a_proposal_only(self):
        config = self.root / "configuration.json"
        config.write_text(json.dumps(self.config()))
        output = self.root / "cli-proposal"
        command = [sys.executable, str(REPOSITORY / "hack/node-qualification/prepare_provider.py"),
                   "--profile", str(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json"),
                   "--configuration", str(config), "--output", str(output)]
        config.chmod(0o644)
        rejected = subprocess.run(command, capture_output=True, text=True, timeout=60)
        self.assertEqual(rejected.returncode, 2)
        self.assertIn("group/world readable", rejected.stderr)
        self.assertFalse(output.exists())
        config.chmod(0o600)
        accepted = subprocess.run(command, capture_output=True, text=True, timeout=60)
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        self.assertFalse(load(output / "plan.private.json")["providerRunStarted"])
        self.assertFalse((self.root / "credential-executed").exists())


if __name__ == "__main__":
    unittest.main()
