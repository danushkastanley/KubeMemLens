import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import qualify


class RestrictedQualificationTests(unittest.TestCase):
    def document(self):
        return {"observations": {"pods": [{"workingSet": {"bytes": None}}], "nodes": [],
                "completeness": "partial", "sources": [{"source": "kubernetes-metrics",
                "scope": "pod", "availability": "absent", "freshness": "unavailable",
                "completeness": "partial", "reason": "source-absent"}]}}

    def test_profiles_preserve_partial_and_missing_sources(self):
        for profile in qualify.PROFILE_IDS:
            result = qualify.evidence(profile, "sha256:" + "a" * 64, "v1.37.0", self.document(),
                                      {"podList": "allowed", "podMetricsList": "denied"})
            self.assertEqual(result["outcome"], "limited")
            self.assertFalse(result["providerSupportQualified"])
            self.assertEqual(result["sources"][0]["availability"], "absent")
            self.assertEqual(result["visibility"]["capturedPodsWithWorkingSet"], 0)
            self.assertFalse(result["cleanup"]["remoteChanges"])

    def test_measured_zero_is_observed(self):
        document = self.document()
        document["observations"]["pods"][0]["workingSet"]["bytes"] = 0
        result = qualify.evidence("local-kind", "sha256:" + "a" * 64, "v1.37.0", document, {})
        self.assertEqual(result["outcome"], "observed")

    def test_raw_identity_is_never_copied(self):
        document = self.document()
        document["observations"]["pods"][0]["uid"] = "private-workload"
        result = qualify.evidence("local-kind", "sha256:" + "a" * 64, "v1.37.0", document, {})
        self.assertNotIn("private-workload", json.dumps(result))

    def test_rejects_sensitive_source_text(self):
        for value in ("Bearer private-credential", "https://private.example", "10.0.0.1"):
            document = copy.deepcopy(self.document())
            document["observations"]["sources"][0]["reason"] = value
            with self.assertRaises(ValueError):
                qualify.evidence("local-kind", "sha256:" + "a" * 64, "v1.37.0", document, {})

    def test_command_output_is_bounded(self):
        self.assertEqual(qualify.run([sys.executable, "-c", "print('ok')"], 8), b"ok\n")
        with self.assertRaises(ValueError):
            qualify.run([sys.executable, "-c", "print('x' * 100)"], 8)
        with self.assertRaises(ValueError):
            qualify.run([sys.executable, "-c", "raise SystemExit(1)"], 8)

    def test_scoped_review_does_not_require_crd_discovery(self):
        temporary_paths = []
        reviews = []

        def command(args, *unused, **kwargs):
            if "version" in args:
                return b'{"serverVersion":{"gitVersion":"v1.37.0"}}'
            if "create" in args:
                self.assertIn("--raw", args)
                self.assertIn("/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", args)
                review = Path(args[args.index("-f") + 1])
                temporary_paths.append(review)
                reviews.append(json.loads(review.read_text())["spec"]["resourceAttributes"])
                return b'{"status":{"allowed":false}}'
            if "capture" in args:
                path = Path(args[args.index("-o") + 1])
                temporary_paths.append(path)
                document = self.document()
                document.update({"schemaVersion": 3, "redacted": True})
                document["observations"]["mode"] = "restricted"
                document["observations"]["pods"][0].update({"namespace": "fixture", "name": "pod"})
                document["observations"]["nodes"] = None
                path.write_text(json.dumps(document))
                path.chmod(0o600)
            return b"deterministic offline output"

        with tempfile.TemporaryDirectory() as directory:
            binary, output = Path(directory) / "binary", Path(directory) / "result.json"
            binary.write_bytes(b"fixture")
            args = SimpleNamespace(cli=str(binary), output=str(output), profile="local-kind",
                                   kubeconfig="private-config", context="private-context", namespace="fixture")
            with patch.object(qualify, "run", side_effect=command):
                qualify.qualify(args)
            result = json.loads(output.read_text())
            self.assertTrue(all(value == "denied" for value in result["permissions"].values()))
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertNotIn("private-config", output.read_text())
        self.assertEqual(len(reviews), 4)
        self.assertTrue(all(review.get("namespace") == "fixture" for review in reviews if review["resource"] == "pods"))
        self.assertTrue(all(not path.exists() for path in temporary_paths))


if __name__ == "__main__":
    unittest.main()
