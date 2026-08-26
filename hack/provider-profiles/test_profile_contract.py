#!/usr/bin/env python3

import copy
import json
import subprocess
import sys
import tempfile
import unittest
from datetime import date
from pathlib import Path


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

from profile_contract import (  # noqa: E402
    CHECK_IDS,
    EvaluationInputError,
    canonical_digest,
    evaluate,
    load_json,
    validate_evidence,
    validate_profile,
)


PROFILE_IDS = {
    "gke-cos-containerd-amd64",
    "gke-ubuntu-containerd-amd64",
    "eks-al2023-containerd-amd64",
    "aks-ubuntu-containerd-amd64",
    "self-managed-containerd",
    "self-managed-crio-amd64",
    "gke-autopilot",
    "eks-fargate",
    "aks-virtual-nodes",
    "windows-deep-mode",
    "cgroup-v1",
}
SAMPLES = {
    "gke-cos-containerd-amd64": ("gke-standard", "Container-Optimized OS cos-121", "containerd://2.0.5", "amd64", "v2", "COS_CONTAINERD", "GKE Dataplane V2"),
    "gke-ubuntu-containerd-amd64": ("gke-standard", "Ubuntu 22.04.5 LTS", "containerd://2.0.5", "amd64", "v2", "UBUNTU_CONTAINERD", "GKE Dataplane V2"),
    "eks-al2023-containerd-amd64": ("eks-managed-nodes", "Amazon Linux 2023.7.20250804", "containerd://2.0.5", "amd64", "v2", "AL2023_x86_64_STANDARD@1.36.1-20260820", "Amazon VPC CNI v1.21.0-eksbuild.1 network-policy=enabled"),
    "aks-ubuntu-containerd-amd64": ("aks-node-pools", "Ubuntu 24.04 LTS", "containerd://2.0.5", "amd64", "v2", "AKSUbuntu-2404gen2containerd-2026.08.12", "Azure CNI Cilium"),
    "self-managed-containerd": ("self-managed", "Debian GNU/Linux 12 (bookworm)", "containerd://2.0.5", "amd64", "v2", "Debian GNU/Linux 12 (bookworm)", "Cilium v1.18.1"),
    "self-managed-crio-amd64": ("self-managed", "Fedora Linux 42 (Server Edition)", "cri-o://1.33.2", "amd64", "v2", "Fedora Linux 42 (Server Edition)", "Calico v3.30.2"),
    "gke-autopilot": ("gke-autopilot", "Container-Optimized OS cos-121", "containerd://2.0.5", "amd64", "unreported", "AUTOPILOT", "GKE Dataplane V2"),
    "eks-fargate": ("eks-fargate", "Amazon Linux 2023.7", "containerd://2.0.5", "amd64", "unreported", "AWS Fargate managed runtime", "AWS Fargate pod networking"),
    "aks-virtual-nodes": ("aks-virtual-nodes", "Ubuntu 22.04.5 LTS", "virtual-kubelet://1.11.0", "amd64", "unreported", "AKS virtual-node ACI", "Azure CNI virtual nodes"),
    "windows-deep-mode": ("self-managed", "Windows Server 2025 Datacenter", "containerd://2.0.5", "amd64", "none", "Windows Server 2025 Datacenter", "Cilium v1.18.1"),
    "cgroup-v1": ("self-managed", "Debian GNU/Linux 11", "containerd://1.7.28", "amd64", "v1", "Debian GNU/Linux 11", "Antrea v2.3.0"),
}


def read_json(path):
    with path.open(encoding="utf-8") as source:
        return json.load(source)


class ProviderProfileContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profiles = {
            path.stem: read_json(path)
            for path in ROOT.glob("*.json")
        }
        cls.supported = read_json(ROOT / "fixtures" / "gke-cos-pass.json")
        cls.unsupported = read_json(ROOT / "fixtures" / "gke-autopilot-unsupported.json")

    def evidence_for(self, profile):
        source = self.supported if profile["expectedOutcome"] == "pass" else self.unsupported
        evidence = copy.deepcopy(source)
        evidence["profile"] = {"id": profile["id"], "digest": profile["profileDigest"]}
        provider, os_image, runtime, architecture, cgroup, node_image, cni_name = SAMPLES[profile["id"]]
        evidence["environment"].update({
            "provider": provider,
            "osImage": os_image,
            "runtime": runtime,
            "architecture": architecture,
            "cgroupVersion": cgroup,
            "nodeImage": node_image,
            "cniName": cni_name,
            "kubeletVersion": "v1.36.1",
        })
        if profile["expectedOutcome"] == "unsupported":
            evidence["reasonCode"] = profile["expectedUnsupportedReason"]
        if profile["id"] == "windows-deep-mode":
            evidence["environment"].update({"linuxNodeCount": 0, "windowsNodeCount": 3})
        return evidence

    def test_all_declared_profiles_validate_and_have_canonical_digests(self):
        self.assertEqual(set(self.profiles), PROFILE_IDS)
        for profile in self.profiles.values():
            with self.subTest(profile=profile["id"]):
                validate_profile(profile)
                self.assertEqual(profile["profileDigest"], canonical_digest(profile))

    def test_every_profile_accepts_its_sanitised_inventory(self):
        for profile in self.profiles.values():
            with self.subTest(profile=profile["id"]):
                report = evaluate(profile, self.evidence_for(profile), date(2026, 8, 26))
                self.assertEqual(report["result"], "pass", report["failures"])

    def test_environment_mismatches_and_missing_cni_enforcement_fail(self):
        profile = self.profiles["gke-cos-containerd-amd64"]
        for field, value in (
            ("provider", "eks-managed-nodes"),
            ("osImage", "Ubuntu 22.04.5 LTS"),
            ("runtime", "cri-o://1.33.2"),
            ("architecture", "arm64"),
            ("cgroupVersion", "v1"),
            ("nodeImage", "UBUNTU_CONTAINERD"),
            ("cniName", "Azure NPM"),
            ("cniEnforced", False),
        ):
            with self.subTest(field=field):
                evidence = self.evidence_for(profile)
                evidence["environment"][field] = value
                report = evaluate(profile, evidence, date(2026, 8, 26))
                self.assertEqual(report["result"], "fail")
                self.assertTrue(any(field in failure for failure in report["failures"]))

    def test_mixed_pool_inventory_and_unsupported_cni_are_explicit(self):
        supported = self.profiles["gke-cos-containerd-amd64"]
        mixed = self.evidence_for(supported)
        mixed["environment"]["windowsNodeCount"] = 2
        mixed["checks"]["mixedOSScheduling"] = "pass"
        self.assertEqual(evaluate(supported, mixed, date(2026, 8, 26))["result"], "pass")
        unsupported = self.profiles["aks-virtual-nodes"]
        evidence = self.evidence_for(unsupported)
        evidence["environment"]["cniEnforced"] = False
        self.assertEqual(evaluate(unsupported, evidence, date(2026, 8, 26))["result"], "pass")

    def test_passed_mixed_os_check_matches_windows_inventory(self):
        profile = self.profiles["gke-cos-containerd-amd64"]
        no_windows = self.evidence_for(profile)
        no_windows["checks"]["mixedOSScheduling"] = "pass"
        with self.assertRaisesRegex(EvaluationInputError, "mixed-OS"):
            validate_evidence(no_windows)
        mixed = self.evidence_for(profile)
        mixed["environment"]["windowsNodeCount"] = 1
        with self.assertRaisesRegex(EvaluationInputError, "mixed-OS"):
            validate_evidence(mixed)
        mixed["checks"]["mixedOSScheduling"] = "pass"
        validate_evidence(mixed)

    def test_unknown_profile_and_evidence_fields_are_rejected(self):
        profile = copy.deepcopy(self.profiles["gke-cos-containerd-amd64"])
        profile["notes"] = "not part of the contract"
        with self.assertRaisesRegex(EvaluationInputError, "profile fields"):
            validate_profile(profile)
        evidence = copy.deepcopy(self.supported)
        evidence["notes"] = []
        with self.assertRaisesRegex(EvaluationInputError, "evidence fields"):
            validate_evidence(evidence)

    def test_schema_versions_require_integer_one(self):
        profile = copy.deepcopy(self.profiles["gke-cos-containerd-amd64"])
        profile["schemaVersion"] = True
        profile["profileDigest"] = canonical_digest(profile)
        with self.assertRaisesRegex(EvaluationInputError, "schemaVersion"):
            validate_profile(profile)
        evidence = copy.deepcopy(self.supported)
        evidence["schemaVersion"] = 1.0
        with self.assertRaisesRegex(EvaluationInputError, "schemaVersion"):
            validate_evidence(evidence)

    def test_expired_or_incorrect_review_window_is_rejected(self):
        profile = self.profiles["gke-cos-containerd-amd64"]
        with self.assertRaisesRegex(EvaluationInputError, "expired"):
            evaluate(profile, self.evidence_for(profile), date(2026, 11, 25))
        evidence = self.evidence_for(profile)
        evidence["reviewDueAt"] = "2026-11-23"
        with self.assertRaisesRegex(EvaluationInputError, "requalification period"):
            evaluate(profile, evidence, date(2026, 8, 26))
        with self.assertRaisesRegex(EvaluationInputError, "future"):
            evaluate(profile, self.evidence_for(profile), date(2026, 8, 25))

    def test_identifier_and_raw_error_keys_are_rejected_at_any_depth(self):
        for key in ("context", "nodeName", "providerID", "subscriptionId", "rawError", "logs"):
            with self.subTest(key=key):
                evidence = copy.deepcopy(self.supported)
                evidence["environment"][key] = "sensitive"
                with self.assertRaisesRegex(EvaluationInputError, "forbidden key"):
                    validate_evidence(evidence)

    def test_identifier_bearing_and_sensitive_values_are_rejected(self):
        sensitive_values = (
            "projects/private-project/zones/a/images/node",
            "arn:aws:eks:eu-west-1:123456789012:cluster/private",
            "/subscriptions/00000000/resourceGroups/private/providers/Microsoft.ContainerService",
            "private-cluster-cni",
            "10.23.45.67",
            "operator@example.com",
            "password=hunter2",
            "AKIAABCDEFGHIJKLMNOP",
            "123456789012",
            "123e4567-e89b-42d3-a456-426614174000",
            "https://private.example.invalid/inventory",
        )
        for value in sensitive_values:
            with self.subTest(value=value):
                evidence = copy.deepcopy(self.supported)
                evidence["environment"]["cniName"] = value
                with self.assertRaisesRegex(EvaluationInputError, "evidence contains"):
                    validate_evidence(evidence)
        validate_evidence(copy.deepcopy(self.supported))

    def test_exact_profile_and_artifact_digests_are_required(self):
        profile = copy.deepcopy(self.profiles["gke-cos-containerd-amd64"])
        profile.pop("profileDigest")
        with self.assertRaisesRegex(EvaluationInputError, "profile fields"):
            validate_profile(profile)
        for field in (
            "imageDigest", "chartDigest", "valuesDigest", "evidenceManifestDigest", "probeImageDigest",
        ):
            with self.subTest(field=field):
                evidence = copy.deepcopy(self.supported)
                evidence["artefacts"][field] = "latest"
                with self.assertRaisesRegex(EvaluationInputError, "exact lowercase sha256"):
                    validate_evidence(evidence)
        evidence = copy.deepcopy(self.supported)
        evidence["profile"]["digest"] = ""
        with self.assertRaisesRegex(EvaluationInputError, "profile digest"):
            validate_evidence(evidence)

    def test_release_identity_and_inventory_fields_are_strict(self):
        for field, value, message in (
            ("sourceCommit", "abc123", "source commit"),
            ("chartVersion", "latest", "SemVer"),
            ("chartVersion", "1.0.0-01", "SemVer"),
        ):
            with self.subTest(field=field):
                evidence = copy.deepcopy(self.supported)
                evidence["artefacts"][field] = value
                with self.assertRaisesRegex(EvaluationInputError, message):
                    validate_evidence(evidence)
        for field, value in (("linuxNodeCount", -1), ("windowsNodeCount", True)):
            with self.subTest(field=field):
                evidence = copy.deepcopy(self.supported)
                evidence["environment"][field] = value
                with self.assertRaisesRegex(EvaluationInputError, "non-negative integer"):
                    validate_evidence(evidence)
        evidence = copy.deepcopy(self.supported)
        evidence["environment"].pop("kernelVersion")
        with self.assertRaisesRegex(EvaluationInputError, "environment fields"):
            validate_evidence(evidence)
        evidence = copy.deepcopy(self.supported)
        evidence["environment"].pop("kubeletVersion")
        with self.assertRaisesRegex(EvaluationInputError, "environment fields"):
            validate_evidence(evidence)
        evidence = copy.deepcopy(self.supported)
        evidence["environment"]["linuxNodeCount"] = 0
        with self.assertRaisesRegex(EvaluationInputError, "at least one Node"):
            validate_evidence(evidence)

    def test_unclassified_failure_is_rejected_and_classified_failure_fails(self):
        profile = self.profiles["gke-cos-containerd-amd64"]
        evidence = self.evidence_for(profile)
        evidence["outcome"] = "failed"
        evidence["reasonCode"] = "provider_error"
        evidence["checks"]["tui"] = "fail"
        with self.assertRaisesRegex(EvaluationInputError, "classified failure"):
            validate_evidence(evidence)
        evidence["reasonCode"] = "tui_failed"
        report = evaluate(profile, evidence, date(2026, 8, 26))
        self.assertEqual(report["result"], "fail")
        self.assertIn("qualification.tui_failed", report["failures"][0])

    def test_failed_reason_must_match_the_failed_check(self):
        evidence = copy.deepcopy(self.supported)
        evidence["outcome"] = "failed"
        evidence["reasonCode"] = "network_policy_failed"
        with self.assertRaisesRegex(EvaluationInputError, "does not match"):
            validate_evidence(evidence)

    def test_unsupported_profiles_reject_success_and_wrong_reason(self):
        profile = self.profiles["gke-autopilot"]
        evidence = self.evidence_for(profile)
        evidence["outcome"] = "passed"
        evidence["reasonCode"] = None
        evidence["checks"] = dict.fromkeys(CHECK_IDS, "pass")
        evidence["checks"]["mixedOSScheduling"] = "not_run"
        with self.assertRaisesRegex(EvaluationInputError, "cannot report a successful"):
            evaluate(profile, evidence, date(2026, 8, 26))
        evidence = self.evidence_for(profile)
        evidence["reasonCode"] = "daemonset_not_supported"
        with self.assertRaisesRegex(EvaluationInputError, "does not match"):
            evaluate(profile, evidence, date(2026, 8, 26))

    def test_supported_profile_rejects_unsupported_confirmation(self):
        profile = self.profiles["gke-cos-containerd-amd64"]
        evidence = copy.deepcopy(self.unsupported)
        evidence["profile"] = {"id": profile["id"], "digest": profile["profileDigest"]}
        evidence["environment"] = self.evidence_for(profile)["environment"]
        with self.assertRaisesRegex(EvaluationInputError, "supported profile"):
            evaluate(profile, evidence, date(2026, 8, 26))

    def test_cli_exit_codes_distinguish_failure_from_invalid_input(self):
        profile_path = ROOT / "gke-cos-containerd-amd64.json"
        fixture_path = ROOT / "fixtures" / "gke-cos-pass.json"
        passed = subprocess.run(
            [sys.executable, str(ROOT / "validate.py"), "--profile", str(profile_path),
             "--evidence", str(fixture_path), "--as-of", "2026-08-26"],
            check=False, capture_output=True, text=True,
        )
        self.assertEqual(passed.returncode, 0, passed.stderr)
        self.assertEqual(json.loads(passed.stdout)["result"], "pass")

        failed_evidence = copy.deepcopy(self.supported)
        failed_evidence["environment"]["cniEnforced"] = False
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "evidence.json"
            path.write_text(json.dumps(failed_evidence), encoding="utf-8")
            failed = subprocess.run(
                [sys.executable, str(ROOT / "validate.py"), "--profile", str(profile_path),
                 "--evidence", str(path), "--as-of", "2026-08-26"],
                check=False, capture_output=True, text=True,
            )
            failed_evidence["reasonCode"] = "raw_provider_error"
            failed_evidence["outcome"] = "failed"
            path.write_text(json.dumps(failed_evidence), encoding="utf-8")
            invalid = subprocess.run(
                [sys.executable, str(ROOT / "validate.py"), "--profile", str(profile_path),
                 "--evidence", str(path), "--as-of", "2026-08-26"],
                check=False, capture_output=True, text=True,
            )
        self.assertEqual(failed.returncode, 1, failed.stderr)
        self.assertEqual(invalid.returncode, 2)
        self.assertIn("classified failure", invalid.stderr)

    def test_load_json_wraps_invalid_json(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "invalid.json"
            path.write_text("{", encoding="utf-8")
            with self.assertRaisesRegex(EvaluationInputError, "read"):
                load_json(path)


if __name__ == "__main__":
    unittest.main()
