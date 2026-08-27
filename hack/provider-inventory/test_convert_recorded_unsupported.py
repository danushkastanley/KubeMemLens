#!/usr/bin/env python3

import copy
import json
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
PROFILES = ROOT.parent / "provider-profiles"
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(PROFILES))

import convert_recorded_unsupported as converter  # noqa: E402
from collect import ReceiptError  # noqa: E402
from observe_unsupported import validate_unsupported_receipt  # noqa: E402
from profile_contract import EvaluationInputError, load_json  # noqa: E402


FILES = {
    "record": "aks-requestheader-incompatibility.json",
    "failed": "aks-requestheader-failed-pending.json",
    "receipt": "aks-requestheader-provider-inventory.json",
    "summary": "aks-requestheader-failed-summary.json",
}


class RecordedUnsupportedConversionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profile = load_json(PROFILES / "aks-ubuntu-containerd-amd64.json")
        cls.paths = {name: ROOT / "fixtures" / filename for name, filename in FILES.items()}
        cls.values = {name: load_json(path) for name, path in cls.paths.items()}
        cls.source = converter.source_at_commit(
            cls.values["record"]["releaseCandidate"]["sourceCommit"],
        )

    def convert(self, values=None, failed_digest=None, summary_digest=None, source=None):
        values = values or copy.deepcopy(self.values)
        return converter.convert_record(
            self.profile, values["record"], values["failed"], values["receipt"], values["summary"],
            failed_digest=failed_digest or converter.digest_file(self.paths["failed"]),
            summary_digest=summary_digest or converter.digest_file(self.paths["summary"]),
            qualification_tool_commit="5" * 40,
            source_content=source if source is not None else self.source,
        )

    def test_exact_recorded_attempt_converts_to_schema_v3_unsupported_receipt(self):
        receipt = self.convert()
        validate_unsupported_receipt(self.profile, receipt)
        self.assertEqual(receipt["schemaVersion"], 3)
        self.assertEqual(receipt["profile"]["digest"], self.profile["profileDigest"])
        self.assertEqual(receipt["proof"]["source"], "recorded-live-conversion")
        self.assertEqual(
            receipt["unsupportedObservation"]["reasonCode"],
            "requestheader_proxy_identity_unavailable",
        )
        self.assertEqual(receipt["unsupportedObservation"]["subjectCount"], 4)
        self.assertEqual(
            receipt["artefacts"]["sourceCommit"],
            "b878c14ecb4206f82259545017c554a3fb0d704d",
        )

    def test_recorded_fixture_bytes_match_the_retained_live_input_digests(self):
        record = self.values["record"]
        self.assertEqual(
            converter.digest_file(self.paths["failed"]),
            record["candidateObservation"]["failedPendingSha256"],
        )
        self.assertEqual(
            converter.digest_file(self.paths["summary"]),
            record["candidateObservation"]["failedSummarySha256"],
        )
        self.assertEqual(
            converter.digest_bytes(self.source),
            record["releaseCandidate"]["extensionServerSourceSha256"],
        )

    def test_each_recorded_input_is_digest_or_identity_bound(self):
        cases = (
            ("pending digest", {}, "9" * 64, None, None, "input digest"),
            ("summary digest", {}, None, "9" * 64, None, "input digest"),
            ("candidate source", {}, None, None, b"changed", "source hash"),
            ("provider receipt", {"receipt": ("nodeImage", "changed")}, None, None, None, "receiptDigest"),
        )
        for name, mutations, failed_digest, summary_digest, source, message in cases:
            with self.subTest(name=name):
                values = copy.deepcopy(self.values)
                for target, (field, value) in mutations.items():
                    values[target][field] = value
                with self.assertRaisesRegex(ReceiptError, message):
                    self.convert(values, failed_digest, summary_digest, source)

    def test_security_decision_and_live_failure_facts_cannot_be_relabelled(self):
        mutations = (
            ("classification", "securityDecision", "weaken-validation", "approved security decision"),
            ("providerAuthenticationConfiguration", "requestHeaderAllowedNames", ["proxy"], "authentication state"),
            ("candidateObservation", "collectorReady", True, "candidate observation"),
        )
        for section, field, value, message in mutations:
            with self.subTest(field=field):
                values = copy.deepcopy(self.values)
                values["record"][section][field] = value
                with self.assertRaisesRegex(ReceiptError, message):
                    self.convert(values)

    def test_failed_attempt_and_environment_must_match_the_record(self):
        for mutation, message in (("outcome", "failed candidate"), ("environment", "environment differs")):
            with self.subTest(mutation=mutation):
                values = copy.deepcopy(self.values)
                if mutation == "outcome":
                    values["failed"]["outcome"] = "passed"
                    values["failed"]["reasonCode"] = None
                    values["failed"]["checks"] = dict.fromkeys(values["failed"]["checks"], "pass")
                    values["failed"]["checks"]["mixedOSScheduling"] = "pass"
                else:
                    values["record"]["environment"]["nodeImage"] = "AKSUbuntu-2404gen2containerd-202608.99.0"
                with self.assertRaisesRegex((ReceiptError, EvaluationInputError), message):
                    self.convert(values)


if __name__ == "__main__":
    unittest.main()
