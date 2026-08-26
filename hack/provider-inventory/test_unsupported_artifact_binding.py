#!/usr/bin/env python3

import hashlib
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


import unsupported_artifact_binding as binding


class UnsupportedArtifactBindingTests(unittest.TestCase):
    def test_clean_exact_candidate_is_bound(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "candidate.tgz"
            archive.write_bytes(b"exact-chart")
            digest = "sha256:" + hashlib.sha256(b"exact-chart").hexdigest()
            with patch.object(binding, "run", side_effect=["4" * 40 + "\n", ""]), \
                    patch.object(binding, "verify_archive") as verify_archive, \
                    patch.object(binding, "verify_chart_metadata") as verify_metadata:
                result = binding.verify_binding("4" * 40, archive, digest)
            self.assertEqual(result, {"sourceCommit": "4" * 40, "chartDigest": digest})
            verify_archive.assert_called_once()
            verify_metadata.assert_called_once()

    def test_dirty_checkout_or_wrong_digest_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "candidate.tgz"
            archive.write_bytes(b"exact-chart")
            digest = "sha256:" + hashlib.sha256(b"exact-chart").hexdigest()
            with patch.object(binding, "run", side_effect=["4" * 40 + "\n", " M chart"]):
                with self.assertRaisesRegex(ValueError, "repository must be clean"):
                    binding.verify_binding("4" * 40, archive, digest)
            with patch.object(binding, "run", side_effect=["4" * 40 + "\n", ""]):
                with self.assertRaisesRegex(ValueError, "chart archive digest"):
                    binding.verify_binding("4" * 40, archive, "sha256:" + "9" * 64)


if __name__ == "__main__":
    unittest.main()
