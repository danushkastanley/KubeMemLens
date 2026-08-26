#!/usr/bin/env python3

import copy
import json
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

import observe_unsupported as observer  # noqa: E402


CASES = {
    "gke-autopilot": ("gke-autopilot-observation.json", "hostpath_not_permitted", "cgroup_hostpath_denied"),
    "eks-fargate": ("eks-fargate-observation.json", "daemonset_not_supported", "daemonset_capability_absent"),
    "aks-virtual-nodes": (
        "aks-virtual-nodes-observation.json", "virtual_nodes_not_supported", "virtual_node_daemonset_absent",
    ),
    "windows-deep-mode": (
        "windows-deep-mode-observation.json", "windows_nodes_not_supported", "windows_cgroup_agent_incompatible",
    ),
    "cgroup-v1": ("cgroup-v1-observation.json", "cgroup_v1_not_supported", "cgroup_v1_observed"),
}
ARTEFACT_BINDING = {"sourceCommit": "4" * 40, "chartDigest": "sha256:" + "2" * 64}


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


class UnsupportedObservationTests(unittest.TestCase):
    def profile(self, profile_id):
        return observer.load_canonical_profile(observer.PROFILES / f"{profile_id}.json")

    def source(self, profile_id):
        filename = CASES[profile_id][0]
        return read_json(ROOT / "fixtures" / filename)

    def receipt(self, profile_id):
        return self.build(self.profile(profile_id), self.source(profile_id), "2026-08-26T11:30:00Z")

    def build(self, profile, source, observed_at=None):
        return observer.build_receipt(profile, source, observed_at, ARTEFACT_BINDING)

    def redigest(self, receipt):
        receipt["receiptDigest"] = observer.canonical_digest(receipt, "receiptDigest")
        return receipt

    def test_all_five_profiles_create_strict_sanitised_receipts(self):
        for profile_id, (_, reason, state) in CASES.items():
            with self.subTest(profile=profile_id):
                profile = self.profile(profile_id)
                receipt = self.receipt(profile_id)
                observer.validate_unsupported_receipt(profile, receipt)
                self.assertEqual(receipt["schemaVersion"], 2)
                self.assertEqual(receipt["unsupportedObservation"]["reasonCode"], reason)
                self.assertEqual(receipt["unsupportedObservation"]["state"], state)
                self.assertTrue(all(receipt["providerChecks"].values()))
                self.assertTrue(all(receipt["unsupportedObservation"]["checks"].values()))
                encoded = json.dumps(receipt, sort_keys=True)
                for private in ("private-", "providerID", "targetStatus", "hostObservations"):
                    self.assertNotIn(private, encoded)

    def test_unobservable_managed_fields_are_recorded_honestly(self):
        gke = self.receipt("gke-autopilot")
        eks = self.receipt("eks-fargate")
        aks = self.receipt("aks-virtual-nodes")
        self.assertEqual(gke["environment"]["cgroupVersion"], "unreported")
        self.assertEqual(eks["environment"]["nodeImage"], "AWS Fargate managed runtime")
        self.assertEqual(eks["environment"]["cgroupVersion"], "unreported")
        self.assertTrue(aks["environment"]["runtime"].startswith("virtual-kubelet://"))
        self.assertEqual(aks["environment"]["cgroupVersion"], "unreported")

    def test_gke_requires_specific_hostpath_admission_evidence(self):
        source = self.source("gke-autopilot")
        for field, value in (
            ("canCreatePods", False),
            ("baselineAccepted", False),
        ):
            with self.subTest(field=field):
                changed = copy.deepcopy(source)
                changed["admission"][field] = value
                with self.assertRaisesRegex(observer.ReceiptError, "hostPath denial"):
                    self.build(self.profile("gke-autopilot"), changed)
        changed = copy.deepcopy(source)
        changed["admission"]["targetStatus"]["message"] = "generic RBAC forbidden"
        with self.assertRaisesRegex(observer.ReceiptError, "hostPath denial"):
            self.build(self.profile("gke-autopilot"), changed)

    def test_provider_modes_and_live_subjects_are_required(self):
        changed = self.source("eks-fargate")
        changed["provider"]["fargateProfile"]["status"] = "DELETING"
        with self.assertRaisesRegex(observer.ReceiptError, "active Fargate"):
            self.build(self.profile("eks-fargate"), changed)
        changed = self.source("aks-virtual-nodes")
        changed["provider"]["addonProfiles"]["aciConnectorLinux"]["enabled"] = False
        with self.assertRaisesRegex(observer.ReceiptError, "enabled Azure"):
            self.build(self.profile("aks-virtual-nodes"), changed)
        changed = self.source("eks-fargate")
        changed["nodes"]["items"][0]["metadata"]["labels"].pop("eks.amazonaws.com/compute-type")
        with self.assertRaisesRegex(observer.ReceiptError, "no matching Ready"):
            self.build(self.profile("eks-fargate"), changed)
        changed = self.source("eks-fargate")
        changed["provider"]["fargateProfile"]["selectors"] = []
        with self.assertRaisesRegex(observer.ReceiptError, "active Fargate"):
            self.build(self.profile("eks-fargate"), changed)
        changed = self.source("aks-virtual-nodes")
        changed["nodes"]["items"][0]["spec"]["taints"] = []
        with self.assertRaisesRegex(observer.ReceiptError, "no matching Ready"):
            self.build(self.profile("aks-virtual-nodes"), changed)

    def test_windows_requires_matching_provider_and_linux_only_chart(self):
        source = self.source("windows-deep-mode")
        source["chart"]["daemonSet"]["spec"]["template"]["spec"]["nodeSelector"] = {}
        with self.assertRaisesRegex(observer.ReceiptError, "Linux-only"):
            self.build(self.profile("windows-deep-mode"), source)
        source = self.source("windows-deep-mode")
        source["providerProof"]["provider"] = "gke-standard"
        with self.assertRaisesRegex(observer.ReceiptError, "no matching Ready"):
            self.build(self.profile("windows-deep-mode"), source)

    def test_cgroup_v1_requires_every_linux_node_host_observation(self):
        source = self.source("cgroup-v1")
        source["hostObservations"] = []
        with self.assertRaisesRegex(observer.ReceiptError, "every Linux Node"):
            self.build(self.profile("cgroup-v1"), source)
        source = self.source("cgroup-v1")
        source["hostObservations"][0]["controllersPresent"] = True
        with self.assertRaisesRegex(observer.ReceiptError, "every Linux Node"):
            self.build(self.profile("cgroup-v1"), source)
        source = self.source("cgroup-v1")
        source["nodes"]["items"][0]["metadata"].pop("uid")
        source["hostObservations"][0]["nodeUID"] = None
        with self.assertRaisesRegex(observer.ReceiptError, "every Linux Node"):
            self.build(self.profile("cgroup-v1"), source)

    def test_reason_method_state_spec_and_checks_are_profile_bound(self):
        profile = self.profile("gke-autopilot")
        receipt = self.receipt("gke-autopilot")
        mutations = (
            ("reasonCode", "daemonset_not_supported"),
            ("method", "provider_capability"),
            ("state", "daemonset_capability_absent"),
        )
        for field, value in mutations:
            with self.subTest(field=field):
                changed = copy.deepcopy(receipt)
                changed["unsupportedObservation"][field] = value
                self.redigest(changed)
                with self.assertRaisesRegex(observer.ReceiptError, "does not match"):
                    observer.validate_unsupported_receipt(profile, changed)
        changed = copy.deepcopy(receipt)
        changed["proof"]["observationSpecDigest"] = "sha256:" + "9" * 64
        self.redigest(changed)
        with self.assertRaisesRegex(observer.ReceiptError, "observation spec"):
            observer.validate_unsupported_receipt(profile, changed)
        changed = copy.deepcopy(receipt)
        changed["unsupportedObservation"]["checks"]["sourceBound"] = False
        self.redigest(changed)
        with self.assertRaisesRegex(observer.ReceiptError, "checks must all be true"):
            observer.validate_unsupported_receipt(profile, changed)

    def test_privacy_and_subject_count_fail_closed(self):
        profile = self.profile("gke-autopilot")
        receipt = self.receipt("gke-autopilot")
        changed = copy.deepcopy(receipt)
        changed["environment"]["nodeImage"] = "https://private.example/node"
        self.redigest(changed)
        with self.assertRaisesRegex(observer.ReceiptError, "resource path or URL"):
            observer.validate_unsupported_receipt(profile, changed)
        changed = copy.deepcopy(receipt)
        changed["unsupportedObservation"]["subjectCount"] = 0
        self.redigest(changed)
        with self.assertRaisesRegex(observer.ReceiptError, "positive"):
            observer.validate_unsupported_receipt(profile, changed)

    def test_source_parser_requires_exact_profile_shape(self):
        changed = self.source("gke-autopilot")
        changed["profileID"] = "eks-fargate"
        with self.assertRaisesRegex(observer.ReceiptError, "selected profile"):
            self.build(self.profile("gke-autopilot"), changed)
        changed = self.source("gke-autopilot")
        changed["callerProof"] = True
        with self.assertRaisesRegex(observer.ReceiptError, "selected profile"):
            self.build(self.profile("gke-autopilot"), changed)


if __name__ == "__main__":
    unittest.main()
