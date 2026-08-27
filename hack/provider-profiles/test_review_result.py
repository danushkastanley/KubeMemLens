#!/usr/bin/env python3

import copy
import hashlib
import json
import stat
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

from profile_contract import EvaluationInputError, load_json, validate_evidence, validate_pending_evidence  # noqa: E402
from bundle_test_support import write_supported_evidence_files  # noqa: E402
from evidence_manifest import create_manifest  # noqa: E402
from review_result import ACKNOWLEDGEMENT, finalise_result, kubernetes_versions_match  # noqa: E402
import observe_unsupported as observer  # noqa: E402
import convert_recorded_unsupported as converter  # noqa: E402


UNSUPPORTED_CASES = {
    "aks-ubuntu-containerd-amd64": None,
    "gke-autopilot": "gke-autopilot-observation.json",
    "eks-fargate": "eks-fargate-observation.json",
    "aks-virtual-nodes": "aks-virtual-nodes-observation.json",
    "windows-deep-mode": "windows-deep-mode-observation.json",
    "cgroup-v1": "cgroup-v1-observation.json",
}


class ProviderReviewResultTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profile_path = ROOT / "gke-cos-containerd-amd64.json"
        cls.profile = load_json(cls.profile_path)
        final = load_json(ROOT / "fixtures" / "gke-cos-pass.json")
        cls.pending = {key: value for key, value in final.items() if key not in {"reviewedAt", "reviewDueAt"}}
        cls.receipt = {
            "schemaVersion": 1,
            "profile": cls.pending["profile"],
            "observedAt": "2026-08-26T11:59:00Z",
            "provider": cls.pending["environment"]["provider"],
            "nodeImage": cls.pending["environment"]["nodeImage"],
            "cniName": cls.pending["environment"]["cniName"],
            "controlPlaneVersion": "1.36.1-gke.100",
            "proofSource": "gcloud:control-plane+node-pool",
            "providerChecks": {
                "profileCanonical": True,
                "providerMode": True,
                "nodeImage": True,
                "cni": True,
                "controlPlaneVersion": True,
                "contextBinding": True,
                "nodePoolBinding": True,
            },
        }
        encoded = json.dumps(cls.receipt, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()
        cls.receipt["receiptDigest"] = "sha256:" + hashlib.sha256(encoded).hexdigest()
        cls.pending["artefacts"]["providerReceiptDigest"] = cls.receipt["receiptDigest"]
    def write_supported_bundle(self, root, pending, receipt):
        return write_supported_evidence_files(root, pending, receipt)

    def review(self, profile, pending, receipt, reviewed_at):
        if profile["expectedOutcome"] != "pass":
            return finalise_result(profile, pending, receipt, reviewed_at)
        with tempfile.TemporaryDirectory() as directory:
            working = copy.deepcopy(pending)
            root = Path(directory)
            self.write_supported_bundle(root, working, receipt)
            (root / "provider-qualification.pending.json").write_text(
                json.dumps(working), encoding="utf-8",
            )
            return finalise_result(profile, working, receipt, reviewed_at, bundle=root)

    def unsupported_case(self, profile_id):
        profile = load_json(ROOT / f"{profile_id}.json")
        if profile_id == "aks-ubuntu-containerd-amd64":
            fixture_root = INVENTORY_ROOT / "fixtures"
            record_path = fixture_root / "aks-requestheader-incompatibility.json"
            failed_path = fixture_root / "aks-requestheader-failed-pending.json"
            receipt_path = fixture_root / "aks-requestheader-provider-inventory.json"
            summary_path = fixture_root / "aks-requestheader-failed-summary.json"
            record = load_json(record_path)
            source = b"synthetic recorded candidate source"
            record["releaseCandidate"]["extensionServerSourceSha256"] = converter.digest_bytes(source)
            failed = load_json(failed_path)
            receipt = converter.convert_record(
                profile, record, failed, load_json(receipt_path), load_json(summary_path),
                failed_digest=converter.digest_file(failed_path),
                summary_digest=converter.digest_file(summary_path),
                qualification_tool_commit="5" * 40,
                source_content=source,
            )
            unsupported = load_json(ROOT / "fixtures" / "gke-autopilot-unsupported.json")
            pending = {key: copy.deepcopy(value) for key, value in unsupported.items()
                       if key not in {"reviewedAt", "reviewDueAt"}}
            pending["completedAt"] = receipt["observedAt"]
            pending["profile"] = copy.deepcopy(receipt["profile"])
            pending["environment"] = copy.deepcopy(receipt["environment"])
            pending["reasonCode"] = profile["expectedUnsupportedReason"]
            pending["artefacts"].update({
                "imageDigest": record["releaseCandidate"]["imageDigest"],
                "chartDigest": record["releaseCandidate"]["chartDigest"],
                "valuesDigest": failed["artefacts"]["valuesDigest"],
                "providerReceiptDigest": receipt["receiptDigest"],
                "sourceCommit": record["releaseCandidate"]["sourceCommit"],
                "chartVersion": failed["artefacts"]["chartVersion"],
            })
            return profile, pending, receipt
        source = load_json(INVENTORY_ROOT / "fixtures" / UNSUPPORTED_CASES[profile_id])
        receipt = observer.build_receipt(
            profile, source, "2026-08-26T11:30:00Z",
            {"sourceCommit": "3" * 40, "chartDigest": "sha256:" + "2" * 64},
        )
        final = load_json(ROOT / "fixtures" / "gke-autopilot-unsupported.json")
        pending = {key: copy.deepcopy(value) for key, value in final.items()
                   if key not in {"reviewedAt", "reviewDueAt"}}
        pending["profile"] = copy.deepcopy(receipt["profile"])
        pending["environment"] = copy.deepcopy(receipt["environment"])
        pending["reasonCode"] = profile["expectedUnsupportedReason"]
        pending["artefacts"]["providerReceiptDigest"] = receipt["receiptDigest"]
        return profile, pending, receipt

    def test_pending_schema_has_no_review_fields(self):
        validate_pending_evidence(copy.deepcopy(self.pending))
        pending = copy.deepcopy(self.pending)
        pending["reviewedAt"] = "2026-08-26"
        with self.assertRaisesRegex(EvaluationInputError, "pending evidence fields"):
            validate_pending_evidence(pending)

    def test_eks_provider_minor_is_bound_to_the_live_patch_version(self):
        self.assertTrue(kubernetes_versions_match("eks-managed-nodes", "v1.36.2-eks-build", "1.36"))
        self.assertTrue(kubernetes_versions_match("eks-fargate", "v1.36.2-eks-build", "1.36"))
        self.assertFalse(kubernetes_versions_match("eks-managed-nodes", "v1.35.9-eks-build", "1.36"))
        self.assertFalse(kubernetes_versions_match("gke-standard", "v1.36.2-gke.1", "1.36"))

    def test_pending_cli_validates_without_assigning_a_review_date(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "pending.json"
            path.write_text(json.dumps(self.pending), encoding="utf-8")
            result = subprocess.run(
                [sys.executable, str(ROOT / "validate.py"), "--profile", str(self.profile_path),
                 "--evidence", str(path), "--pending"],
                check=False, capture_output=True, text=True,
            )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("reviewedAt", self.pending)

    def test_pending_cli_binds_profile_environment_and_outcome(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "pending.json"
            command = [
                sys.executable,
                str(ROOT / "validate.py"),
                "--profile",
                str(self.profile_path),
                "--evidence",
                str(path),
                "--pending",
            ]
            wrong_identity = copy.deepcopy(self.pending)
            wrong_identity["profile"]["id"] = "gke-ubuntu-containerd-amd64"
            path.write_text(json.dumps(wrong_identity), encoding="utf-8")
            identity_result = subprocess.run(command, check=False, capture_output=True, text=True)

            wrong_environment = copy.deepcopy(self.pending)
            wrong_environment["environment"]["nodeImage"] = "UBUNTU_CONTAINERD"
            path.write_text(json.dumps(wrong_environment), encoding="utf-8")
            environment_result = subprocess.run(command, check=False, capture_output=True, text=True)

            failed_outcome = copy.deepcopy(self.pending)
            failed_outcome["outcome"] = "failed"
            failed_outcome["reasonCode"] = "tui_failed"
            failed_outcome["checks"]["tui"] = "fail"
            path.write_text(json.dumps(failed_outcome), encoding="utf-8")
            outcome_result = subprocess.run(command, check=False, capture_output=True, text=True)
        self.assertEqual(identity_result.returncode, 2)
        self.assertIn("profile identity", identity_result.stderr)
        self.assertEqual(environment_result.returncode, 1, environment_result.stderr)
        self.assertIn("environment.nodeImage", environment_result.stdout)
        self.assertEqual(outcome_result.returncode, 1, outcome_result.stderr)
        self.assertIn("qualification.outcome", outcome_result.stdout)

    def test_pending_schema_requires_kubelet_values_and_receipt_digests(self):
        pending = copy.deepcopy(self.pending)
        pending["environment"].pop("kubeletVersion")
        with self.assertRaisesRegex(EvaluationInputError, "environment fields"):
            validate_pending_evidence(pending)
        for field in (
            "valuesDigest", "providerReceiptDigest", "evidenceManifestDigest", "probeImageDigest",
        ):
            with self.subTest(field=field):
                pending = copy.deepcopy(self.pending)
                pending["artefacts"].pop(field)
                with self.assertRaisesRegex(EvaluationInputError, "artefacts fields"):
                    validate_pending_evidence(pending)

    def test_review_adds_current_window_and_validates_final_evidence(self):
        final = self.review(self.profile, self.pending, self.receipt, date(2026, 8, 26))
        self.assertEqual(final["reviewedAt"], "2026-08-26")
        self.assertEqual(final["reviewDueAt"], "2026-11-24")
        validate_evidence(final)
        self.assertNotIn("reviewedAt", self.pending)

    def test_review_rejects_a_mismatched_evidence_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            pending = copy.deepcopy(self.pending)
            root = Path(directory)
            self.write_supported_bundle(root, pending, self.receipt)
            pending["artefacts"]["evidenceManifestDigest"] = "sha256:" + "9" * 64
            (root / "provider-qualification.pending.json").write_text(
                json.dumps(pending), encoding="utf-8",
            )
            with self.assertRaisesRegex(EvaluationInputError, "manifest digest"):
                finalise_result(self.profile, pending, self.receipt, date(2026, 8, 26), bundle=root)

    def test_review_cannot_refresh_old_evidence(self):
        with self.assertRaisesRegex(EvaluationInputError, "within seven days"):
            self.review(self.profile, self.pending, self.receipt, date(2026, 9, 3))

    def test_review_rejects_manifested_nonpassing_lifecycle_facts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pending = copy.deepcopy(self.pending)
            self.write_supported_bundle(root, pending, self.receipt)
            lifecycle_path = root / "lifecycle.json"
            lifecycle = json.loads(lifecycle_path.read_text(encoding="utf-8"))
            lifecycle["checks"]["tui"]["cleanExit"] = False
            lifecycle_path.write_text(json.dumps(lifecycle), encoding="utf-8")
            (root / "evidence-manifest.json").unlink()
            manifest = create_manifest(root, pending["artefacts"]["probeImageDigest"])
            pending["artefacts"]["evidenceManifestDigest"] = manifest["manifestDigest"]
            (root / "provider-qualification.pending.json").write_text(
                json.dumps(pending), encoding="utf-8",
            )
            with self.assertRaisesRegex(EvaluationInputError, "lifecycle strict facts"):
                finalise_result(self.profile, pending, self.receipt, date(2026, 8, 26), bundle=root)

    def test_review_finalises_all_six_unsupported_observations(self):
        for profile_id in UNSUPPORTED_CASES:
            with self.subTest(profile=profile_id):
                profile, pending, receipt = self.unsupported_case(profile_id)
                reviewed_at = date.fromisoformat(pending["completedAt"][:10])
                final = self.review(profile, pending, receipt, reviewed_at)
                self.assertEqual(final["outcome"], "unsupported_confirmed")
                self.assertEqual(final["reasonCode"], profile["expectedUnsupportedReason"])
                validate_evidence(final)

    def test_review_requires_outcome_specific_receipt_versions(self):
        profile, pending, _ = self.unsupported_case("gke-autopilot")
        v1 = copy.deepcopy(self.receipt)
        v1["profile"] = copy.deepcopy(pending["profile"])
        v1["receiptDigest"] = "sha256:" + hashlib.sha256(json.dumps(
            {key: value for key, value in v1.items() if key != "receiptDigest"},
            ensure_ascii=False, separators=(",", ":"), sort_keys=True,
        ).encode()).hexdigest()
        pending["artefacts"]["providerReceiptDigest"] = v1["receiptDigest"]
        with self.assertRaisesRegex(EvaluationInputError, "provider receipt is invalid"):
            self.review(profile, pending, v1, date(2026, 8, 26))

        _, _, v2 = self.unsupported_case("gke-autopilot")
        supported = copy.deepcopy(self.pending)
        supported["artefacts"]["providerReceiptDigest"] = v2["receiptDigest"]
        with self.assertRaisesRegex(EvaluationInputError, "provider receipt is invalid"):
            self.review(self.profile, supported, v2, date(2026, 8, 26))

    def test_unsupported_review_binds_the_full_environment(self):
        profile, pending, receipt = self.unsupported_case("gke-autopilot")
        for field, value in pending["environment"].items():
            with self.subTest(field=field):
                changed = copy.deepcopy(pending)
                if type(value) is bool:
                    changed["environment"][field] = not value
                elif type(value) is int:
                    changed["environment"][field] = value + 1
                else:
                    changed["environment"][field] = "mismatched"
                with self.assertRaisesRegex(EvaluationInputError, "unsupported live observation"):
                    self.review(profile, changed, receipt, date(2026, 8, 26))

    def test_unsupported_review_binds_reason_method_state_and_spec(self):
        profile, pending, receipt = self.unsupported_case("gke-autopilot")
        changed_pending = copy.deepcopy(pending)
        changed_pending["reasonCode"] = "daemonset_not_supported"
        with self.assertRaisesRegex(EvaluationInputError, "pending reason"):
            self.review(profile, changed_pending, receipt, date(2026, 8, 26))
        for field, value in (
            ("method", "provider_capability"),
            ("state", "daemonset_capability_absent"),
        ):
            with self.subTest(field=field):
                changed = copy.deepcopy(receipt)
                changed["unsupportedObservation"][field] = value
                changed["receiptDigest"] = observer.canonical_digest(changed, "receiptDigest")
                changed_pending = copy.deepcopy(pending)
                changed_pending["artefacts"]["providerReceiptDigest"] = changed["receiptDigest"]
                with self.assertRaisesRegex(EvaluationInputError, "provider receipt is invalid"):
                    self.review(profile, changed_pending, changed, date(2026, 8, 26))
        changed = copy.deepcopy(receipt)
        changed["proof"]["observationSpecDigest"] = "sha256:" + "9" * 64
        changed["receiptDigest"] = observer.canonical_digest(changed, "receiptDigest")
        pending["artefacts"]["providerReceiptDigest"] = changed["receiptDigest"]
        with self.assertRaisesRegex(EvaluationInputError, "provider receipt is invalid"):
            self.review(profile, pending, changed, date(2026, 8, 26))

    def test_review_rejects_stale_or_future_unsupported_observations(self):
        profile, pending, receipt = self.unsupported_case("gke-autopilot")
        for observed_at, message in (
            ("2026-08-26T10:59:59Z", "more than one hour"),
            ("2026-08-26T12:00:01Z", "cannot postdate"),
        ):
            with self.subTest(observed_at=observed_at):
                changed = copy.deepcopy(receipt)
                changed["observedAt"] = observed_at
                changed["receiptDigest"] = observer.canonical_digest(changed, "receiptDigest")
                changed_pending = copy.deepcopy(pending)
                changed_pending["artefacts"]["providerReceiptDigest"] = changed["receiptDigest"]
                with self.assertRaisesRegex(EvaluationInputError, message):
                    self.review(profile, changed_pending, changed, date(2026, 8, 26))

    def test_review_rejects_privacy_invalid_unsupported_receipt(self):
        profile, pending, receipt = self.unsupported_case("gke-autopilot")
        receipt["environment"]["nodeImage"] = "https://private.example/node"
        receipt["receiptDigest"] = observer.canonical_digest(receipt, "receiptDigest")
        pending["artefacts"]["providerReceiptDigest"] = receipt["receiptDigest"]
        with self.assertRaisesRegex(EvaluationInputError, "provider receipt is invalid"):
            self.review(profile, pending, receipt, date(2026, 8, 26))

    def test_cli_finalises_unsupported_v2_receipt(self):
        profile, pending, receipt = self.unsupported_case("cgroup-v1")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            profile_path = ROOT / "cgroup-v1.json"
            pending_path = root / "provider-qualification.pending.json"
            receipt_path = root / "provider-inventory.json"
            output_path = root / "provider-qualification.json"
            pending_path.write_text(json.dumps(pending), encoding="utf-8")
            receipt_path.write_text(json.dumps(receipt), encoding="utf-8")
            result = subprocess.run([
                sys.executable, str(ROOT / "review_result.py"), "--profile", str(profile_path),
                "--input", str(pending_path), "--output", str(output_path),
                "--provider-receipt", str(receipt_path), "--acknowledge", ACKNOWLEDGEMENT,
            ], check=False, capture_output=True, text=True)
            reviewed = json.loads(output_path.read_text(encoding="utf-8")) if output_path.exists() else {}
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(reviewed.get("outcome"), "unsupported_confirmed")
        self.assertEqual(reviewed.get("reasonCode"), "cgroup_v1_not_supported")

    def test_review_requires_exact_profile_identity(self):
        pending = copy.deepcopy(self.pending)
        pending["profile"]["id"] = "gke-ubuntu-containerd-amd64"
        with self.assertRaisesRegex(EvaluationInputError, "profile identity"):
            self.review(self.profile, pending, self.receipt, date(2026, 8, 26))

    def test_review_binds_environment_and_digest_to_provider_receipt(self):
        for field in ("provider", "nodeImage", "cniName", "kubernetesVersion"):
            with self.subTest(field=field):
                pending = copy.deepcopy(self.pending)
                pending["environment"][field] = "mismatched"
                with self.assertRaisesRegex(EvaluationInputError, "provider-owned inventory"):
                    self.review(self.profile, pending, self.receipt, date(2026, 8, 26))
        pending = copy.deepcopy(self.pending)
        pending["artefacts"]["providerReceiptDigest"] = "sha256:" + "9" * 64
        with self.assertRaisesRegex(EvaluationInputError, "receipt digest"):
            self.review(self.profile, pending, self.receipt, date(2026, 8, 26))

    def test_cli_requires_acknowledgement_and_refuses_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pending_path = root / "provider-qualification.pending.json"
            receipt_path = root / "provider-inventory.json"
            output_path = root / "provider-qualification.json"
            pending = copy.deepcopy(self.pending)
            self.write_supported_bundle(root, pending, self.receipt)
            pending_path.write_text(json.dumps(pending), encoding="utf-8")
            command = [
                sys.executable,
                str(ROOT / "review_result.py"),
                "--profile",
                str(self.profile_path),
                "--input",
                str(pending_path),
                "--output",
                str(output_path),
                "--provider-receipt",
                str(receipt_path),
                "--evidence-manifest",
                str(root / "evidence-manifest.json"),
                "--acknowledge",
                "not-reviewed",
            ]
            unacknowledged = subprocess.run(command, check=False, capture_output=True, text=True)
            self.assertFalse(output_path.exists())
            command[-1] = ACKNOWLEDGEMENT
            reviewed = subprocess.run(command, check=False, capture_output=True, text=True)
            overwritten = subprocess.run(command, check=False, capture_output=True, text=True)
            mode = stat.S_IMODE(output_path.stat().st_mode)
        self.assertNotEqual(unacknowledged.returncode, 0)
        self.assertIn("--acknowledge", unacknowledged.stderr)
        self.assertEqual(reviewed.returncode, 0, reviewed.stderr)
        self.assertNotEqual(overwritten.returncode, 0)
        self.assertIn("refusing to overwrite", overwritten.stderr)
        self.assertEqual(mode, 0o600)


if __name__ == "__main__":
    unittest.main()
