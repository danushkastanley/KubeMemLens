#!/usr/bin/env python3

import hashlib
import io
import json
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

from validate_oci_archive import ArchiveError, validate_archive  # noqa: E402


VERSION = "v1.2.3-rc.1"
COMMIT = "a" * 40
BUILD_DATE = "2026-08-27T00:00:00Z"


class OCIBuilder:
    def __init__(self, bad_label=False, platforms=("amd64", "arm64"), attestations=True):
        self.blobs = {}
        self.bad_label = bad_label
        self.platforms = platforms
        self.attestations = attestations

    def descriptor(self, document, media_type):
        content = json.dumps(document, separators=(",", ":"), sort_keys=True).encode()
        digest = hashlib.sha256(content).hexdigest()
        self.blobs[digest] = content
        return {"mediaType": media_type, "digest": f"sha256:{digest}", "size": len(content)}

    def image(self, architecture):
        labels = {
            "org.opencontainers.image.version": "wrong" if self.bad_label else VERSION,
            "org.opencontainers.image.revision": COMMIT,
            "org.opencontainers.image.created": BUILD_DATE,
            "org.opencontainers.image.source": "https://github.com/danushkastanley/KubeMemLens",
            "org.opencontainers.image.licenses": "Apache-2.0",
        }
        config = self.descriptor({
            "architecture": architecture,
            "os": "linux",
            "config": {"User": "65532:65532", "Entrypoint": ["/kubectl-memlens"], "Labels": labels},
        }, "application/vnd.oci.image.config.v1+json")
        manifest = self.descriptor({
            "schemaVersion": 2,
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "config": config,
            "layers": [],
        }, "application/vnd.oci.image.manifest.v1+json")
        manifest["platform"] = {"os": "linux", "architecture": architecture}
        return manifest

    def attestation(self, subject):
        layers = []
        for predicate in ("https://spdx.dev/Document", "https://slsa.dev/provenance/v1"):
            body = {"_type": "https://in-toto.io/Statement/v0.1", "predicateType": predicate,
                    "predicate": {"spdxVersion": "SPDX-2.3"} if predicate.endswith("Document") else {}}
            layer = self.descriptor(body, "application/vnd.in-toto+json")
            layer["annotations"] = {"in-toto.io/predicate-type": predicate}
            layers.append(layer)
        config = self.descriptor({}, "application/vnd.oci.image.config.v1+json")
        manifest = self.descriptor({
            "schemaVersion": 2,
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "config": config,
            "layers": layers,
        }, "application/vnd.oci.image.manifest.v1+json")
        manifest["platform"] = {"os": "unknown", "architecture": "unknown"}
        manifest["annotations"] = {
            "vnd.docker.reference.type": "attestation-manifest",
            "vnd.docker.reference.digest": subject["digest"],
        }
        return manifest

    def write(self, path):
        images = [self.image(architecture) for architecture in self.platforms]
        attestations = [self.attestation(image) for image in images] if self.attestations else []
        inner = self.descriptor({
            "schemaVersion": 2,
            "mediaType": "application/vnd.oci.image.index.v1+json",
            "manifests": images + attestations,
        }, "application/vnd.oci.image.index.v1+json")
        root = {"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json",
                "manifests": [inner]}
        with tarfile.open(path, "w") as archive:
            self.add(archive, "oci-layout", json.dumps({"imageLayoutVersion": "1.0.0"}).encode())
            self.add(archive, "index.json", json.dumps(root).encode())
            for digest, content in self.blobs.items():
                self.add(archive, f"blobs/sha256/{digest}", content)
        return inner["digest"]

    @staticmethod
    def add(archive, name, content):
        member = tarfile.TarInfo(name)
        member.size = len(content)
        archive.addfile(member, io.BytesIO(content))


class OCIArchiveTests(unittest.TestCase):
    def validate(self, builder):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "image.tar"
            expected = builder.write(archive)
            return expected, validate_archive(archive, VERSION, COMMIT, BUILD_DATE)

    def test_exact_multiarch_archive_passes(self):
        expected, observed = self.validate(OCIBuilder())
        self.assertEqual(observed, expected)

    def test_missing_platform_fails(self):
        with self.assertRaisesRegex(ArchiveError, "amd64 and arm64"):
            self.validate(OCIBuilder(platforms=("amd64",)))

    def test_release_identity_mismatch_fails(self):
        with self.assertRaisesRegex(ArchiveError, "labels"):
            self.validate(OCIBuilder(bad_label=True))

    def test_product_only_archive_passes_without_inline_attestations(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "image.tar"
            builder = OCIBuilder(attestations=False)
            expected = builder.write(archive)
            observed = validate_archive(
                archive, VERSION, COMMIT, BUILD_DATE, require_attestations=False
            )
            self.assertEqual(observed, expected)

    def test_inline_attestations_fail_product_only_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "image.tar"
            OCIBuilder().write(archive)
            with self.assertRaisesRegex(ArchiveError, "must not contain inline attestations"):
                validate_archive(
                    archive, VERSION, COMMIT, BUILD_DATE, require_attestations=False
                )


if __name__ == "__main__":
    unittest.main()
