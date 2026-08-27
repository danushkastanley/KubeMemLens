#!/usr/bin/env python3

import copy
import hashlib
import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

import evidence_manifest as manifest  # noqa: E402


PROBE_DIGEST = "sha256:" + "a" * 64


class EvidenceManifestTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.bundle = Path(self.temporary.name)
        for index, name in enumerate(manifest.EVIDENCE_FILES, start=1):
            (self.bundle / name).write_text(json.dumps({"schemaVersion": 1, "value": index}) + "\n",
                                           encoding="utf-8")

    def test_create_and_verify_bind_the_exact_closed_file_set(self):
        created = manifest.create_manifest(self.bundle, PROBE_DIGEST)
        path = self.bundle / manifest.MANIFEST_NAME
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(created["probeImageDigest"], PROBE_DIGEST)
        self.assertEqual([entry["name"] for entry in created["files"]], list(manifest.EVIDENCE_FILES))
        self.assertEqual(created["manifestDigest"], manifest.canonical_digest(created))
        for entry in created["files"]:
            expected = "sha256:" + hashlib.sha256((self.bundle / entry["name"]).read_bytes()).hexdigest()
            self.assertEqual(entry["digest"], expected)
        verified = manifest.verify_manifest(self.bundle)
        self.assertEqual(verified, created)
        encoded = path.read_text(encoding="utf-8")
        for forbidden in ("/tmp/", "nodeUID", "providerID", "rawContent"):
            self.assertNotIn(forbidden, encoded)

    def test_create_refuses_overwrite_and_invalid_probe_digest(self):
        manifest.create_manifest(self.bundle, PROBE_DIGEST)
        with self.assertRaisesRegex(manifest.ManifestError, "overwrite"):
            manifest.create_manifest(self.bundle, PROBE_DIGEST)
        (self.bundle / manifest.MANIFEST_NAME).unlink()
        for value in ("latest", "SHA256:" + "A" * 64, "repo@example/agent@" + PROBE_DIGEST):
            with self.subTest(value=value):
                with self.assertRaisesRegex(manifest.ManifestError, "probe image digest"):
                    manifest.create_manifest(self.bundle, value)

    def test_verify_rejects_swapped_removed_or_changed_files(self):
        manifest.create_manifest(self.bundle, PROBE_DIGEST)
        target = self.bundle / "doctor.json"
        target.write_text("{\"changed\":true}\n", encoding="utf-8")
        with self.assertRaisesRegex(manifest.ManifestError, "digest changed"):
            manifest.verify_manifest(self.bundle)
        target.write_text("{\"schemaVersion\":1}\n", encoding="utf-8")
        (self.bundle / manifest.MANIFEST_NAME).unlink()
        manifest.create_manifest(self.bundle, PROBE_DIGEST)
        target.unlink()
        with self.assertRaisesRegex(manifest.ManifestError, "unavailable"):
            manifest.verify_manifest(self.bundle)

    def test_symlinks_are_rejected_for_bundle_files_and_manifest(self):
        target = self.bundle / "doctor.json"
        real = self.bundle / "doctor-real.json"
        target.replace(real)
        target.symlink_to(real.name)
        with self.assertRaisesRegex(manifest.ManifestError, "direct regular file"):
            manifest.create_manifest(self.bundle, PROBE_DIGEST)
        target.unlink()
        real.replace(target)
        manifest.create_manifest(self.bundle, PROBE_DIGEST)
        manifest_path = self.bundle / manifest.MANIFEST_NAME
        real_manifest = self.bundle / "manifest-real.json"
        manifest_path.replace(real_manifest)
        manifest_path.symlink_to(real_manifest.name)
        with self.assertRaisesRegex(manifest.ManifestError, "direct regular file"):
            manifest.verify_manifest(self.bundle)

    def test_empty_oversized_and_non_regular_files_are_rejected(self):
        target = self.bundle / "status.json"
        target.write_bytes(b"")
        with self.assertRaisesRegex(manifest.ManifestError, "size"):
            manifest.build_manifest(self.bundle, PROBE_DIGEST)
        target.write_bytes(b"x" * (manifest.MAX_EVIDENCE_FILE_BYTES + 1))
        with self.assertRaisesRegex(manifest.ManifestError, "size"):
            manifest.build_manifest(self.bundle, PROBE_DIGEST)
        target.unlink()
        target.mkdir()
        with self.assertRaisesRegex(manifest.ManifestError, "regular file"):
            manifest.build_manifest(self.bundle, PROBE_DIGEST)

    def test_sensitive_json_is_rejected_before_manifest_creation(self):
        (self.bundle / "status.json").write_text(
            json.dumps({"token": "private-credential"}), encoding="utf-8",
        )
        with self.assertRaisesRegex(manifest.ManifestError, "forbidden key"):
            manifest.create_manifest(self.bundle, PROBE_DIGEST)

    def test_manifest_schema_rejects_paths_extra_fields_and_noncanonical_digests(self):
        created = manifest.build_manifest(self.bundle, PROBE_DIGEST)
        cases = []
        changed = copy.deepcopy(created)
        changed["files"][0]["name"] = "../doctor.json"
        changed["manifestDigest"] = manifest.canonical_digest(changed)
        cases.append(changed)
        changed = copy.deepcopy(created)
        changed["files"][0]["digest"] = "sha256:" + "A" * 64
        changed["manifestDigest"] = manifest.canonical_digest(changed)
        cases.append(changed)
        changed = copy.deepcopy(created)
        changed["rawContent"] = "secret"
        changed["manifestDigest"] = manifest.canonical_digest(changed)
        cases.append(changed)
        changed = copy.deepcopy(created)
        changed["manifestDigest"] = "sha256:" + "0" * 64
        cases.append(changed)
        for changed in cases:
            with self.subTest(changed=changed):
                with self.assertRaises(manifest.ManifestError):
                    manifest.validate_manifest(changed)

    def test_cli_create_verify_and_overwrite_exit_codes(self):
        command = [sys.executable, str(ROOT / "evidence_manifest.py")]
        created = subprocess.run(
            [*command, "create", "--bundle", str(self.bundle), "--probe-image-digest", PROBE_DIGEST],
            check=False, capture_output=True, text=True,
        )
        self.assertEqual(created.returncode, 0, created.stderr)
        self.assertEqual(json.loads(created.stdout)["result"], "pass")
        verified = subprocess.run(
            [*command, "verify", "--bundle", str(self.bundle)],
            check=False, capture_output=True, text=True,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        refused = subprocess.run(
            [*command, "create", "--bundle", str(self.bundle), "--probe-image-digest", PROBE_DIGEST],
            check=False, capture_output=True, text=True,
        )
        self.assertEqual(refused.returncode, 2)
        self.assertIn("overwrite", refused.stderr)


if __name__ == "__main__":
    unittest.main()
