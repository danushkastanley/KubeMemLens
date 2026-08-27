#!/usr/bin/env python3

import copy
import hashlib
import json
import subprocess
import sys
import tempfile
import unittest
from datetime import date
from pathlib import Path


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
INVENTORY_ROOT = ROOT.parent / "provider-inventory"
sys.path.insert(0, str(INVENTORY_ROOT))

from evaluate_matrix import (  # noqa: E402
    MatrixInputError,
    PROFILE_IDS,
    RELEASE_FIELDS,
    SUPPORTED_PROFILE_IDS,
    UNSUPPORTED_PROFILE_IDS,
    evaluate_matrix,
    load_canonical_profiles,
    load_reviewed_bundles,
)
from evidence_manifest import MANIFEST_NAME  # noqa: E402
from bundle_test_support import write_supported_evidence_files  # noqa: E402
from profile_contract import EvaluationInputError  # noqa: E402
from review_result import finalise_result  # noqa: E402
import observe_unsupported as observer  # noqa: E402


SAMPLES = {
    "gke-cos-containerd-amd64": ("gke-standard", "Container-Optimized OS cos-121", "containerd://2.0.5", "amd64", "v2", "COS_CONTAINERD", "GKE Dataplane V2"),
    "gke-ubuntu-containerd-amd64": ("gke-standard", "Ubuntu 22.04.5 LTS", "containerd://2.0.5", "amd64", "v2", "UBUNTU_CONTAINERD", "GKE Dataplane V2"),
    "eks-al2023-containerd-amd64": ("eks-managed-nodes", "Amazon Linux 2023.7.20250804", "containerd://2.0.5", "amd64", "v2", "AL2023_x86_64_STANDARD@1.36.1-20260820", "Amazon VPC CNI v1.21.0-eksbuild.1 network-policy=enabled"),
    "aks-ubuntu-containerd-amd64": ("aks-node-pools", "Ubuntu 24.04 LTS", "containerd://2.0.5", "amd64", "v2", "AKSUbuntu-2404gen2containerd-2026.08.12", "Azure CNI Cilium"),
    "self-managed-containerd": ("self-managed", "Debian GNU/Linux 12 (bookworm)", "containerd://2.0.5", "amd64", "v2", "Debian GNU/Linux 12 (bookworm)", "Cilium v1.18.1"),
    "self-managed-crio-amd64": ("self-managed", "Fedora Linux 42 (Server Edition)", "cri-o://1.36.1", "amd64", "v2", "Fedora Linux 42 (Server Edition)", "Calico v3.30.3"),
    "gke-autopilot": ("gke-autopilot", "Container-Optimized OS cos-121", "containerd://2.0.5", "amd64", "unreported", "AUTOPILOT", "GKE Dataplane V2"),
    "eks-fargate": ("eks-fargate", "Amazon Linux 2023.7", "containerd://2.0.5", "amd64", "unreported", "AWS Fargate managed runtime", "AWS Fargate pod networking"),
    "aks-virtual-nodes": ("aks-virtual-nodes", "Ubuntu 22.04.5 LTS", "virtual-kubelet://1.11.0", "amd64", "unreported", "AKS virtual-node ACI", "Azure CNI virtual nodes"),
    "windows-deep-mode": ("aks-node-pools", "Windows Server 2022 Datacenter", "containerd://2.0.5", "amd64", "none", "AKSWindows-2022-containerd-20348.4052.250716", "Azure CNI Calico"),
    "cgroup-v1": ("self-managed", "Debian GNU/Linux 11", "containerd://1.7.28", "amd64", "v1", "Debian GNU/Linux 11", "Antrea v2.3.0"),
}
UNSUPPORTED_SOURCES = {
    "gke-autopilot": "gke-autopilot-observation.json",
    "eks-fargate": "eks-fargate-observation.json",
    "aks-virtual-nodes": "aks-virtual-nodes-observation.json",
    "windows-deep-mode": "windows-deep-mode-observation.json",
    "cgroup-v1": "cgroup-v1-observation.json",
}


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


class ProviderMatrixTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profiles = load_canonical_profiles()
        cls.passed = read_json(ROOT / "fixtures" / "gke-cos-pass.json")
        cls.unsupported = read_json(ROOT / "fixtures" / "gke-autopilot-unsupported.json")

    def evidence_for(self, profile_id):
        profile = self.profiles[profile_id]
        source = self.passed if profile_id in SUPPORTED_PROFILE_IDS else self.unsupported
        evidence = copy.deepcopy(source)
        evidence["profile"] = {"id": profile_id, "digest": profile["profileDigest"]}
        provider, os_image, runtime, architecture, cgroup, node_image, cni_name = SAMPLES[profile_id]
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
        if profile_id in UNSUPPORTED_PROFILE_IDS:
            evidence["reasonCode"] = profile["expectedUnsupportedReason"]
        if profile_id == "windows-deep-mode":
            evidence["environment"].update({"linuxNodeCount": 0, "windowsNodeCount": 2})
        return evidence

    def complete_matrix(self):
        records = [self.evidence_for(profile_id) for profile_id in sorted(PROFILE_IDS)]
        mixed = next(record for record in records if record["profile"]["id"] == "gke-cos-containerd-amd64")
        mixed["environment"]["windowsNodeCount"] = 1
        mixed["checks"]["mixedOSScheduling"] = "pass"
        return records

    def supported_receipt(self, profile, pending):
        providers = {
            "gke-standard": "gcloud:control-plane+node-pool",
            "eks-managed-nodes": "aws:control-plane+nodegroup+vpc-cni-addon",
            "aks-node-pools": "az:control-plane+node-pool",
            "self-managed": "kubectl:version+nodes+daemonsets",
        }
        environment = pending["environment"]
        receipt = {
            "schemaVersion": 2,
            "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
            "observedAt": "2026-08-26T11:59:00Z",
            "qualificationToolCommit": "5" * 40,
            "provider": environment["provider"],
            "nodeImage": environment["nodeImage"],
            "cniName": environment["cniName"],
            "controlPlaneVersion": environment["kubernetesVersion"].removeprefix("v"),
            "proofSource": providers[environment["provider"]],
            "providerChecks": {
                "profileCanonical": True, "providerMode": True, "nodeImage": True,
                "cni": True, "controlPlaneVersion": True, "contextBinding": True,
                "nodePoolBinding": True,
            },
        }
        encoded = json.dumps(receipt, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()
        receipt["receiptDigest"] = "sha256:" + hashlib.sha256(encoded).hexdigest()
        return receipt

    def write_supported_bundle(self, bundle, record):
        profile = self.profiles[record["profile"]["id"]]
        pending = {key: copy.deepcopy(value) for key, value in record.items()
                   if key not in {"reviewedAt", "reviewDueAt"}}
        receipt = self.supported_receipt(profile, pending)
        pending["artefacts"]["providerReceiptDigest"] = receipt["receiptDigest"]
        bundle.mkdir()
        write_supported_evidence_files(bundle, pending, receipt)
        (bundle / "provider-qualification.pending.json").write_text(
            json.dumps(pending), encoding="utf-8",
        )
        final = finalise_result(profile, pending, receipt, date(2026, 8, 26), bundle=bundle)
        path = bundle / "provider-qualification.json"
        path.write_text(json.dumps(final), encoding="utf-8")
        return path

    def write_unsupported_bundle(self, bundle, record):
        profile = self.profiles[record["profile"]["id"]]
        source = read_json(INVENTORY_ROOT / "fixtures" / UNSUPPORTED_SOURCES[profile["id"]])
        receipt = observer.build_receipt(
            profile, source, "2026-08-26T11:30:00Z",
            {
                "sourceCommit": "3" * 40,
                "chartDigest": "sha256:" + "2" * 64,
                "qualificationToolCommit": "5" * 40,
            },
        )
        pending = {key: copy.deepcopy(value) for key, value in record.items()
                   if key not in {"reviewedAt", "reviewDueAt"}}
        pending["environment"] = copy.deepcopy(receipt["environment"])
        pending["reasonCode"] = profile["expectedUnsupportedReason"]
        pending["artefacts"]["providerReceiptDigest"] = receipt["receiptDigest"]
        pending["artefacts"]["evidenceManifestDigest"] = None
        pending["artefacts"]["probeImageDigest"] = None
        final = finalise_result(profile, pending, receipt, date(2026, 8, 26))
        bundle.mkdir()
        (bundle / "provider-inventory.json").write_text(json.dumps(receipt), encoding="utf-8")
        (bundle / "provider-qualification.pending.json").write_text(
            json.dumps(pending), encoding="utf-8",
        )
        path = bundle / "provider-qualification.json"
        path.write_text(json.dumps(final), encoding="utf-8")
        return path

    def complete_bundle_paths(self, root):
        paths = []
        for record in self.complete_matrix():
            profile_id = record["profile"]["id"]
            bundle = root / profile_id
            if profile_id in SUPPORTED_PROFILE_IDS:
                paths.append(self.write_supported_bundle(bundle, record))
            else:
                paths.append(self.write_unsupported_bundle(bundle, record))
        return paths

    def test_complete_reviewed_matrix_passes(self):
        report = evaluate_matrix(self.complete_matrix(), date(2026, 8, 26), self.profiles)
        self.assertEqual(report["result"], "pass", report["failures"])
        self.assertEqual(len(report["rows"]), 11)
        self.assertEqual(set(report["release"]), set(RELEASE_FIELDS))
        self.assertNotIn("qualificationToolCommit", report["release"])

    def test_canonical_matrix_has_exact_supported_and_unsupported_rows(self):
        self.assertEqual(len(SUPPORTED_PROFILE_IDS), 6)
        self.assertEqual(len(UNSUPPORTED_PROFILE_IDS), 5)
        self.assertEqual(set(self.profiles), PROFILE_IDS)

    def test_missing_duplicate_unknown_and_pending_rows_are_rejected(self):
        records = self.complete_matrix()
        with self.assertRaisesRegex(MatrixInputError, "missing evidence"):
            evaluate_matrix(records[:-1], date(2026, 8, 26), self.profiles)
        with self.assertRaisesRegex(MatrixInputError, "duplicate evidence"):
            evaluate_matrix(records + [copy.deepcopy(records[0])], date(2026, 8, 26), self.profiles)
        unknown = copy.deepcopy(records)
        unknown[0]["profile"]["id"] = "unknown-provider"
        with self.assertRaisesRegex(MatrixInputError, "unknown profile"):
            evaluate_matrix(unknown, date(2026, 8, 26), self.profiles)
        pending = copy.deepcopy(records)
        pending[0].pop("reviewedAt")
        pending[0].pop("reviewDueAt")
        with self.assertRaisesRegex(MatrixInputError, "reviewed final evidence"):
            evaluate_matrix(pending, date(2026, 8, 26), self.profiles)

    def test_release_identity_mismatch_fails_each_field(self):
        for field in RELEASE_FIELDS:
            with self.subTest(field=field):
                records = self.complete_matrix()
                record = records[0]
                record["artefacts"][field] = "0.0.2" if field == "chartVersion" else \
                    ("4" * 40 if field == "sourceCommit" else "sha256:" + "9" * 64)
                report = evaluate_matrix(records, date(2026, 8, 26), self.profiles)
                self.assertEqual(report["result"], "fail")
                self.assertTrue(any(field in failure for failure in report["failures"]))

    def test_failed_row_and_missing_mixed_os_record_fail_matrix(self):
        records = self.complete_matrix()
        record = next(item for item in records if item["profile"]["id"] == "gke-ubuntu-containerd-amd64")
        record["outcome"] = "failed"
        record["reasonCode"] = "tui_failed"
        record["checks"]["tui"] = "fail"
        report = evaluate_matrix(records, date(2026, 8, 26), self.profiles)
        self.assertEqual(report["result"], "fail")
        self.assertTrue(any("did not pass" in failure for failure in report["failures"]))

        records = self.complete_matrix()
        mixed = next(item for item in records if item["environment"]["windowsNodeCount"] > 0
                     and item["profile"]["id"] in SUPPORTED_PROFILE_IDS)
        mixed["environment"]["windowsNodeCount"] = 0
        mixed["checks"]["mixedOSScheduling"] = "not_run"
        report = evaluate_matrix(records, date(2026, 8, 26), self.profiles)
        self.assertEqual(report["result"], "fail")
        self.assertIn("mixed Linux/Windows", report["failures"][-1])

        records = self.complete_matrix()
        supported = next(
            item for item in records if item["profile"]["id"] in SUPPORTED_PROFILE_IDS
        )
        supported["artefacts"]["probeImageDigest"] = "sha256:" + "9" * 64
        report = evaluate_matrix(records, date(2026, 8, 26), self.profiles)
        self.assertEqual(report["result"], "fail")
        self.assertTrue(any("probe image digest" in failure for failure in report["failures"]))

    def test_cli_exit_codes_distinguish_failed_matrix_from_invalid_input(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = self.complete_bundle_paths(root)
            command = [sys.executable, str(ROOT / "evaluate_matrix.py"), "--as-of", "2026-08-26",
                       *(str(path) for path in paths)]
            passed = subprocess.run(command, check=False, capture_output=True, text=True)
            incomplete = subprocess.run(command[:-1], check=False, capture_output=True, text=True)
            mixed_path = next(path for path in paths if path.parent.name == "gke-cos-containerd-amd64")
            pending_path = mixed_path.parent / "provider-qualification.pending.json"
            pending = read_json(pending_path)
            pending["environment"]["windowsNodeCount"] = 0
            pending["checks"]["mixedOSScheduling"] = "not_run"
            receipt = read_json(mixed_path.parent / "provider-inventory.json")
            (mixed_path.parent / MANIFEST_NAME).unlink()
            write_supported_evidence_files(mixed_path.parent, pending, receipt)
            pending_path.write_text(json.dumps(pending), encoding="utf-8")
            final = finalise_result(
                self.profiles["gke-cos-containerd-amd64"], pending, receipt,
                date(2026, 8, 26), bundle=mixed_path.parent,
            )
            mixed_path.write_text(json.dumps(final), encoding="utf-8")
            failed = subprocess.run(command, check=False, capture_output=True, text=True)
            detached = root / "detached" / "provider-qualification.json"
            detached.parent.mkdir()
            detached.write_text(json.dumps(final), encoding="utf-8")
            detached_result = subprocess.run(
                [sys.executable, str(ROOT / "evaluate_matrix.py"), str(detached)],
                check=False, capture_output=True, text=True,
            )
        self.assertEqual(passed.returncode, 0, passed.stderr)
        self.assertEqual(json.loads(passed.stdout)["result"], "pass")
        self.assertEqual(incomplete.returncode, 2)
        self.assertEqual(failed.returncode, 1, failed.stderr)
        self.assertEqual(detached_result.returncode, 2)

    def test_bundle_loader_enforces_outcome_specific_manifest_rules(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = self.complete_bundle_paths(root)
            supported = next(path for path in paths if path.parent.name in SUPPORTED_PROFILE_IDS)
            (supported.parent / MANIFEST_NAME).unlink()
            with self.assertRaisesRegex(EvaluationInputError, "manifest"):
                load_reviewed_bundles([supported], self.profiles)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = self.complete_bundle_paths(root)
            unsupported = next(path for path in paths if path.parent.name in UNSUPPORTED_PROFILE_IDS)
            (unsupported.parent / MANIFEST_NAME).write_text("{}", encoding="utf-8")
            with self.assertRaisesRegex(MatrixInputError, "cannot contain"):
                load_reviewed_bundles([unsupported], self.profiles)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = self.complete_bundle_paths(root)
            supported = next(path for path in paths if path.parent.name in SUPPORTED_PROFILE_IDS)
            (supported.parent / "private-provider-response.json").write_text(
                '{"credential":"private"}', encoding="utf-8",
            )
            with self.assertRaisesRegex(EvaluationInputError, "unexpected file"):
                load_reviewed_bundles([supported], self.profiles)


if __name__ == "__main__":
    unittest.main()
