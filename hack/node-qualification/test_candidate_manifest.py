import hashlib
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path

from candidate_manifest import cli_archive_digest, verify
from common import ContractError
from process import execute


def sha(data):
    return hashlib.sha256(data).hexdigest()


class CandidateManifestTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.tag, self.commit = "v1.2.3-rc.4", "a" * 40
        self.image = "ghcr.io/danushkastanley/candidates/1.2.3-rc.4/kube-memlens"
        binary = b"#!/bin/sh\ntouch " + str(self.root / "must-not-execute").encode() + b"\n"
        (self.root / "cli").write_bytes(binary)
        (self.root / "chart.tgz").write_bytes(b"chart-fixture")
        self.archive = self.root / "kube-memlens_1.2.3_linux_amd64.tar.gz"
        with tarfile.open(self.archive, "w:gz") as archive:
            item = tarfile.TarInfo("kubectl-memlens"); item.size = len(binary)
            archive.addfile(item, io.BytesIO(binary))
        names = [f"kube-memlens_1.2.3_{system}_{arch}." + ("zip" if system == "windows" else "tar.gz")
                 for system in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]
        manifest = {"schema_version": 1, "candidate_tag": self.tag, "intended_ga_tag": "v1.2.3", "source_commit": self.commit,
                    "cli_archives": {n: sha(self.archive.read_bytes()) for n in names},
                    "image": {"repository": self.image, "digest": "sha256:" + "b" * 64},
                    "chart": {"repository": "ghcr.io/danushkastanley/candidates/1.2.3-rc.4/charts/kube-memlens",
                              "digest": "sha256:" + "c" * 64, "package": {"name": "kube-memlens-1.2.3.tgz", "sha256": sha(b"chart-fixture")}},
                    "workflow_identity": "https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/" + self.tag}
        (self.root / "candidate-manifest.json").write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
        (self.root / "candidate-manifest.sigstore.json").write_text("signature-fixture")
        self.config = {"sourceCommit": self.commit, "imageRepository": self.image, "imageDigest": manifest["image"]["digest"],
                       "chartArchive": str(self.root / "chart.tgz"), "chartDigest": "sha256:" + sha(b"chart-fixture"),
                       "cliBinary": str(self.root / "cli"), "cliDigest": "sha256:" + sha(binary)}
        self.commands = []

    def runner(self, argv, **kwargs):
        self.commands.append(argv)
        if argv[0].endswith("validate_candidate_manifest.sh"):
            return execute(argv, **kwargs)
        self.assertIn(argv[0], {"cosign", "gh"})
        return "{}"

    def check(self, runner=None):
        return verify(self.root, self.tag, self.config, "linux_amd64", run=runner or self.runner)

    def test_authority_and_bytes_are_bound_without_executing_candidate(self):
        result = self.check()
        self.assertEqual(result["sourceCommit"], self.commit)
        self.assertEqual(result["cliDigest"], self.config["cliDigest"])
        self.assertFalse((self.root / "must-not-execute").exists())
        self.assertEqual([c[0] for c in self.commands[1:]], ["cosign", "cosign", "gh"])
        signature = self.commands[1]
        self.assertEqual(signature[signature.index("--certificate-identity") + 1],
                         "https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/" + self.tag)

    def test_signature_failure_stops_before_image_verification(self):
        def rejected(argv, **kwargs):
            if argv[0] == "cosign":
                raise ContractError("signature rejected")
            return self.runner(argv, **kwargs)
        with self.assertRaisesRegex(ContractError, "signature rejected"):
            self.check(rejected)
        self.assertEqual(len(self.commands), 1)

    def test_wrong_source_or_image_is_rejected_before_network_verification(self):
        for field in ("sourceCommit", "imageDigest"):
            before = self.config[field]
            self.config[field] = ("d" * 40) if field == "sourceCommit" else "sha256:" + "d" * 64
            with self.assertRaises(ContractError):
                self.check()
            self.config[field] = before
        self.assertTrue(all(c[0].endswith("validate_candidate_manifest.sh") for c in self.commands))

    def test_changed_proposed_binary_is_not_accepted(self):
        (self.root / "cli").write_bytes(b"changed")
        with self.assertRaisesRegex(ContractError, "proposed binary"):
            self.check()

    def test_approved_mirror_keeps_the_original_signature_authority(self):
        self.config["imageRepository"] = "registry.example/approved-mirror"
        result = self.check()
        self.assertEqual(result["imageDigest"], self.config["imageDigest"])
        self.assertEqual(self.commands[-2][-1], self.image + "@" + self.config["imageDigest"])
        self.assertEqual(self.commands[-1][3], "oci://" + self.image + "@" + self.config["imageDigest"])

    def test_archive_symlinks_duplicate_members_and_escape_are_rejected(self):
        for name, kind, duplicate in (("kubectl-memlens", tarfile.SYMTYPE, False),
                                     ("../kubectl-memlens", tarfile.REGTYPE, False),
                                     ("kubectl-memlens", tarfile.REGTYPE, True)):
            with tarfile.open(self.archive, "w:gz") as archive:
                item = tarfile.TarInfo(name); item.type = kind; item.size = 1 if kind == tarfile.REGTYPE else 0
                archive.addfile(item, io.BytesIO(b"x"))
                if duplicate:
                    archive.addfile(item, io.BytesIO(b"x"))
            with self.assertRaises(ContractError):
                cli_archive_digest(self.archive)


if __name__ == "__main__":
    unittest.main()
