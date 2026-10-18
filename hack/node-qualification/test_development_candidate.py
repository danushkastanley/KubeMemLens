"""Development authority tests use real archives and mock only external signing/Git."""

import copy
import io
import json
from pathlib import Path
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from common import ContractError
from development_candidate import MANIFEST, SIGNATURE, manifest, verify
from test_oci_binary import BinaryImage, sha

COMMIT = "a" * 40
DATE = "2026-01-01T00:00:00Z"
TREE = "sha256:" + "b" * 64


class DevelopmentCandidateTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.bundle = self.root / "bundle"
        self.bundle.mkdir()
        self.tool = self.root / "cosign"
        self.tool.write_text("#!/bin/sh\nexit 99\n")
        self.tool.chmod(0o700)
        self.key = self.root / "verification.pub"
        self.key.write_text("-----BEGIN PUBLIC KEY-----\nfixture\n-----END PUBLIC KEY-----\n")
        self.image = self.bundle / "image.tar"
        with patch.multiple("test_oci_binary", VERSION="dev", COMMIT=COMMIT, BUILD_DATE=DATE):
            image_digest = BinaryImage().write(self.image)
        chart = self.bundle / "chart.tgz"
        chart.write_bytes(b"chart-fixture")
        self.host = "darwin_arm64"
        self.binary = self.root / "cli"
        self.binary.write_bytes(b"#!/bin/sh\ntouch " + str(self.root / "must-not-execute").encode() + b"\n")
        self.producer = self.root / "producer"
        self.producer.write_bytes(b"amd64:memlens-node-context")
        archives = {}
        for system in ("linux_amd64", "linux_arm64", self.host):
            content = self.binary.read_bytes() if system == self.host else (system.split("_")[1] + ":kubectl-memlens").encode()
            archive = self.bundle / (system + ".tar.gz")
            self.archive(archive, content)
            archives[system] = self.entry(archive)
        self.value = {"schemaVersion": 1, "authority": "local-development", "sourceCommit": COMMIT,
                      "sourceTreeDigest": TREE, "version": "dev", "buildDate": DATE, "imageDigest": image_digest,
                      "chart": self.entry(chart), "imageArchive": self.entry(self.image), "cliArchives": archives}
        (self.bundle / SIGNATURE).write_text("signature-fixture")
        self.config = {"sourceCommit": COMMIT, "imageDigest": image_digest, "chartArchive": str(chart),
                       "chartDigest": sha(chart.read_bytes()), "cliBinary": str(self.binary),
                       "cliDigest": sha(self.binary.read_bytes()), "producerBinary": str(self.producer),
                       "producerDigest": sha(self.producer.read_bytes())}
        self.args = SimpleNamespace(development_bundle=str(self.bundle), development_manifest_digest="",
            development_key=str(self.key), development_key_digest=sha(self.key.read_bytes()),
            cosign=str(self.tool), cosign_digest=sha(self.tool.read_bytes()), architecture="amd64")
        self.signer = Mock(return_value="Verified OK")
        self.save()

    @staticmethod
    def archive(path, content, name="kubectl-memlens", kind=tarfile.REGTYPE):
        with tarfile.open(path, "w:gz") as archive:
            member = tarfile.TarInfo(name)
            member.type = kind
            member.size = len(content) if kind == tarfile.REGTYPE else 0
            archive.addfile(member, io.BytesIO(content))

    @staticmethod
    def entry(path):
        return {"name": path.name, "size": path.stat().st_size, "sha256": sha(path.read_bytes())}

    def save(self):
        path = self.bundle / MANIFEST
        path.write_text(json.dumps(self.value))
        self.args.development_manifest_digest = sha(path.read_bytes())

    def check(self):
        with patch("development_candidate.production_source", return_value=TREE), \
             patch("development_candidate.local_command", return_value=b"1767225600\n"):
            return verify(SimpleNamespace(configuration=self.config), self.args, self.host, run=self.signer)

    def test_verifies_both_platforms_and_host_without_executing_candidate(self):
        proof = self.check()
        self.assertEqual(proof["authority"], "local-development")
        self.assertIs(proof["releaseQualificationGranted"], False)
        self.assertEqual(proof["candidate"]["cliDigest"], self.config["cliDigest"])
        self.assertEqual(proof["image"]["architecture"], "amd64")
        self.assertFalse((self.root / "must-not-execute").exists())
        argv, options = self.signer.call_args
        self.assertIn("--insecure-ignore-tlog=true", argv[0])
        self.assertEqual(options["timeout"], 35)
        self.assertNotIn("AWS_ACCESS_KEY_ID", options["environment"])

    def test_signature_rejection_precedes_source_and_payload_inspection(self):
        self.signer.side_effect = ContractError("bad signature")
        with patch("development_candidate.copy_payload") as copy_payload, self.assertRaisesRegex(ContractError, "bad signature"):
            self.check()
        copy_payload.assert_not_called()

    def test_manifest_key_and_verifier_must_match_independent_pins(self):
        for path in (self.bundle / MANIFEST, self.key, self.tool):
            with self.subTest(path=path.name):
                original = path.read_bytes()
                path.write_bytes(original + b"changed")
                with self.assertRaises(ContractError):
                    self.check()
                self.signer.assert_not_called()
                path.write_bytes(original)

    def test_payload_changes_and_undeclared_files_are_rejected(self):
        for name in ("chart.tgz", "image.tar", "linux_arm64.tar.gz", "darwin_arm64.tar.gz"):
            path = self.bundle / name
            original = path.read_bytes()
            path.write_bytes(original + b"changed")
            with self.subTest(name=name), self.assertRaises(ContractError):
                self.check()
            path.write_bytes(original)
        (self.bundle / "unreviewed").write_text("extra")
        self.signer.reset_mock()
        with self.assertRaisesRegex(ContractError, "undeclared"):
            self.check()
        self.signer.assert_not_called()

    def test_payload_symlink_and_missing_host_cannot_be_used(self):
        path = self.bundle / "chart.tgz"
        copy_path = self.root / "chart.tgz"
        path.rename(copy_path)
        path.symlink_to(copy_path)
        with self.assertRaises(ContractError):
            self.check()
        self.value["cliArchives"].pop(self.host)
        self.save()
        with self.assertRaisesRegex(ContractError, "host CLI is missing"):
            self.check()

    def test_source_date_image_and_producer_must_match_proposal(self):
        for field, value in (("sourceCommit", "f" * 40), ("sourceTreeDigest", "sha256:" + "f" * 64),
                             ("buildDate", "2026-01-02T00:00:00Z"), ("imageDigest", "sha256:" + "e" * 64)):
            before = self.value[field]
            self.value[field] = value
            self.save()
            with self.subTest(field=field), self.assertRaises(ContractError):
                self.check()
            self.value[field] = before
        self.save()
        self.producer.write_bytes(b"arm64:memlens-node-context")
        self.config["producerDigest"] = sha(self.producer.read_bytes())
        with self.assertRaisesRegex(ContractError, "producer differs"):
            self.check()

    def test_archive_escape_links_and_cross_platform_mismatch_rejected_even_if_signed(self):
        path = self.bundle / "linux_arm64.tar.gz"
        for name, kind, content in (("../kubectl-memlens", tarfile.REGTYPE, b"x"),
                                    ("kubectl-memlens", tarfile.SYMTYPE, b""),
                                    ("kubectl-memlens", tarfile.REGTYPE, b"amd64:kubectl-memlens")):
            self.archive(path, content, name, kind)
            self.value["cliArchives"]["linux_arm64"] = self.entry(path)
            self.save()
            with self.subTest(name=name, kind=kind), self.assertRaises(ContractError):
                self.check()

    def test_manifest_schema_rejects_release_authority_unsafe_names_and_missing_platform(self):
        mutations = [lambda v: v.update(authority="release"), lambda v: v.update(version="v1.0.0-rc.1"),
                     lambda v: v["chart"].update(name="../chart"), lambda v: v["chart"].update(size=True),
                     lambda v: v["imageArchive"].update(name=v["chart"]["name"]),
                     lambda v: v["cliArchives"].pop("linux_arm64")]
        for change in mutations:
            value = copy.deepcopy(self.value)
            change(value)
            with self.assertRaises(ContractError):
                manifest(json.dumps(value), COMMIT)
        raw = json.dumps(self.value)
        with self.assertRaisesRegex(ContractError, "duplicate"):
            manifest(raw.replace('"schemaVersion": 1', '"schemaVersion": 1, "schemaVersion": 1'), COMMIT)

    def test_signed_single_platform_image_is_not_sufficient(self):
        builder = BinaryImage()
        builder.platforms = ("amd64",)
        with patch.multiple("test_oci_binary", VERSION="dev", COMMIT=COMMIT, BUILD_DATE=DATE):
            image_digest = builder.write(self.image)
        self.value.update(imageDigest=image_digest, imageArchive=self.entry(self.image))
        self.config["imageDigest"] = image_digest
        self.save()
        with self.assertRaises(ValueError):
            self.check()

    def test_duplicate_cli_archive_members_are_rejected_even_when_signed(self):
        path = self.bundle / "linux_arm64.tar.gz"
        with tarfile.open(path, "w:gz") as archive:
            for _ in range(2):
                member = tarfile.TarInfo("kubectl-memlens")
                member.size = 1
                archive.addfile(member, io.BytesIO(b"x"))
        self.value["cliArchives"]["linux_arm64"] = self.entry(path)
        self.save()
        with self.assertRaises(ContractError):
            self.check()


if __name__ == "__main__":
    unittest.main()
