#!/usr/bin/env python3

import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parent
INVENTORY = ROOT.parent / "provider-inventory"
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(INVENTORY))

import build_unsupported_pending as builder  # noqa: E402
from build_unsupported_pending import build_pending  # noqa: E402
from observe_unsupported import build_receipt  # noqa: E402
from profile_contract import EvaluationInputError, load_json, validate_pending_evidence  # noqa: E402


class UnsupportedPendingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profile = load_json(ROOT / "gke-autopilot.json")
        source = json.loads((INVENTORY / "fixtures" / "gke-autopilot-observation.json").read_text())
        binding = {
            "sourceCommit": "4" * 40,
            "chartDigest": "sha256:" + "2" * 64,
            "qualificationToolCommit": "5" * 40,
        }
        cls.receipt = build_receipt(
            cls.profile, source, "2026-08-26T11:59:00Z", binding,
        )
        cls.artefacts = {
            "imageDigest": "sha256:" + "1" * 64,
            "chartDigest": "sha256:" + "2" * 64,
            "valuesDigest": "sha256:" + "3" * 64,
            "providerReceiptDigest": cls.receipt["receiptDigest"],
            "evidenceManifestDigest": None,
            "probeImageDigest": None,
            "sourceCommit": "4" * 40,
            "chartVersion": "1.0.0-rc.1",
        }

    def test_receipt_builds_closed_unsupported_pending_evidence(self):
        pending = build_pending(
            self.profile, self.receipt, self.artefacts, "2026-08-26T12:00:00Z",
        )
        validate_pending_evidence(pending)
        self.assertEqual(pending["outcome"], "unsupported_confirmed")
        self.assertEqual(pending["reasonCode"], "hostpath_not_permitted")
        self.assertEqual(pending["checks"]["prerequisites"], "unsupported")
        self.assertTrue(all(
            value == "not_run" for name, value in pending["checks"].items()
            if name != "prerequisites"
        ))
        self.assertIsNone(pending["artefacts"]["evidenceManifestDigest"])
        self.assertEqual(self.receipt["qualificationToolCommit"], "5" * 40)
        self.assertEqual(pending["artefacts"]["sourceCommit"], "4" * 40)

    def test_stale_or_future_observation_is_rejected(self):
        for completed in ("2026-08-26T13:00:01Z", "2026-08-26T11:58:59Z"):
            with self.subTest(completed=completed), self.assertRaisesRegex(
                    EvaluationInputError, "one hour"):
                build_pending(self.profile, self.receipt, self.artefacts, completed)

    def test_receipt_digest_and_reason_cannot_be_relabelled(self):
        changed = copy.deepcopy(self.receipt)
        changed["unsupportedObservation"]["reasonCode"] = "daemonset_not_supported"
        with self.assertRaises(EvaluationInputError):
            build_pending(self.profile, changed, self.artefacts, "2026-08-26T12:00:00Z")

    def test_receipt_cannot_be_rebound_to_another_chart_or_commit(self):
        for field, value in (("chartDigest", "sha256:" + "9" * 64), ("sourceCommit", "9" * 40)):
            with self.subTest(field=field):
                changed = copy.deepcopy(self.artefacts)
                changed[field] = value
                with self.assertRaisesRegex(EvaluationInputError, "candidate source and chart"):
                    build_pending(self.profile, self.receipt, changed, "2026-08-26T12:00:00Z")

    def test_input_verification_separates_tool_and_candidate_commits(self):
        profile = load_json(ROOT / "aks-virtual-nodes.json")
        source = json.loads((INVENTORY / "fixtures" / "aks-virtual-nodes-observation.json").read_text())
        receipt = build_receipt(
            profile, source, "2026-08-26T11:59:00Z",
            {
                "sourceCommit": "4" * 40,
                "chartDigest": "sha256:" + "2" * 64,
                "qualificationToolCommit": "5" * 40,
            },
        )
        with tempfile.TemporaryDirectory() as directory:
            bundle = Path(directory)
            receipt_path = bundle / "provider-inventory.json"
            archive = bundle / "candidate.tgz"
            receipt_path.write_text(json.dumps(receipt), encoding="utf-8")
            archive.write_bytes(b"candidate")
            with patch.object(builder, "run", side_effect=["", "", "5" * 40 + "\n", "version: 0.0.1\n"]), \
                    patch.object(builder, "tracked_at_commit") as tracked, \
                    patch.object(builder, "tree_at_commit") as tree, \
                    patch.object(builder, "digest_file", side_effect=[
                        "sha256:" + "2" * 64, "sha256:" + "3" * 64,
                    ]), patch.object(builder, "verify_archive"), \
                    patch.object(builder, "verify_chart_metadata"):
                _, _, artefacts = builder.verify_inputs(
                    ROOT / "aks-virtual-nodes.json", receipt_path, archive,
                    "sha256:" + "2" * 64,
                    ROOT.parent / "provider-values" / "aks-virtual-nodes.yaml",
                    "sha256:" + "3" * 64, "4" * 40, "sha256:" + "1" * 64,
                )
        self.assertEqual(artefacts["sourceCommit"], "4" * 40)
        self.assertEqual(receipt["qualificationToolCommit"], "5" * 40)
        self.assertEqual(tracked.call_args_list[0].args[1], "5" * 40)
        self.assertEqual(tracked.call_args_list[1].args[1], "4" * 40)
        self.assertEqual(tree.call_args.args[1], "4" * 40)


if __name__ == "__main__":
    unittest.main()
