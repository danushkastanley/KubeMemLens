import copy
import unittest
from datetime import timedelta

from common import ContractError, digest, instant, load
from review import review
from test_contract import ROOT, fixture, seal


def attestation(e, p):
    a = {"schemaVersion": 1, "recordDigest": e["recordDigest"], "profileDigest": p["profileDigest"], "independentReview": True, "decision": "approve", "reviewedAt": "2026-09-11T13:00:00Z"}
    a["attestationDigest"] = digest(a, "attestationDigest")
    return a


class IndependentReviewTest(unittest.TestCase):
    def test_bound_local_review_and_expiry(self):
        p, e = fixture(); a = attestation(e, p)
        now = instant(a["reviewedAt"])
        result = review(p, e, a, now=now)
        self.assertTrue(result["qualified"])
        self.assertEqual(result["profile"], e["profile"])
        self.assertEqual(result["provenance"], "unknown")
        self.assertEqual(result["expiresAt"], "2026-10-11T12:20:00Z")
        with self.assertRaises(ContractError):
            review(p, e, a, now=now + timedelta(days=31))

    def test_dirty_failed_unbound_and_non_independent_evidence_cannot_be_reviewed(self):
        for scenario in ("dirty", "failed", "different evidence", "non-independent", "future", "late"):
            with self.subTest(scenario=scenario):
                p, e = fixture(); a = attestation(e, p)
                now = instant(a["reviewedAt"])
                if scenario == "dirty":
                    e["artefacts"]["sourceDirty"] = True; seal(e); a = attestation(e, p)
                elif scenario == "failed":
                    e["orchestration"] = "failed"; seal(e); a = attestation(e, p)
                elif scenario == "different evidence":
                    a["recordDigest"] = "sha256:" + "b" * 64
                elif scenario == "non-independent":
                    a["independentReview"] = False
                elif scenario == "future":
                    a["reviewedAt"] = "2026-09-12T13:00:00Z"
                else:
                    a["reviewedAt"] = "2026-09-21T13:00:00Z"; now = instant(a["reviewedAt"])
                a["attestationDigest"] = digest(a, "attestationDigest")
                with self.assertRaises(ContractError):
                    review(p, e, a, now=now)

    def test_provider_receipt_binds_the_actual_run_and_tool(self):
        p = load(ROOT / "profiles/gke-standard.json"); p, e = fixture(p)
        e["environment"].update(nodeImage="COS_CONTAINERD", cni="GKE Dataplane V2", architecture="amd64", osImage="COS 125")
        r = {"schemaVersion": 2, "profile": {"id": "gke-cos-containerd-amd64", "digest": "sha256:" + "c" * 64}, "observedAt": "2026-09-11T12:10:00Z", "provider": "gke-standard", "nodeImage": "COS_CONTAINERD", "cniName": "GKE Dataplane V2", "controlPlaneVersion": "1.37.0", "proofSource": "gcloud:control-plane+node-pool", "providerChecks": {k: True for k in ("profileCanonical", "providerMode", "nodeImage", "cni", "controlPlaneVersion", "contextBinding", "nodePoolBinding")}, "qualificationToolCommit": e["artefacts"]["toolCommit"]}
        r["profile"]["digest"] = load(ROOT.parent / "provider-profiles/gke-cos-containerd-amd64.json")["profileDigest"]
        r["receiptDigest"] = digest(r, "receiptDigest"); e["environment"]["providerReceiptDigest"] = r["receiptDigest"]; seal(e)
        a = attestation(e, p); now = instant(a["reviewedAt"])
        self.assertTrue(review(p, e, a, r, now=now)["qualified"])
        for field, value in (("qualificationToolCommit", "b" * 40), ("observedAt", "2026-08-11T12:10:00Z"), ("controlPlaneVersion", "1.36.0")):
            changed = copy.deepcopy(r); changed[field] = value; changed["receiptDigest"] = digest(changed, "receiptDigest")
            sample = copy.deepcopy(e); sample["environment"]["providerReceiptDigest"] = changed["receiptDigest"]; seal(sample)
            with self.assertRaises(ContractError):
                review(p, sample, attestation(sample, p), changed, now=now)


if __name__ == "__main__":
    unittest.main()
