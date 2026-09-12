import gzip
import hashlib
import io
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from common import ContractError
from oci_binary import BINARIES, inspect_archive
from test_validate_oci_archive import BUILD_DATE, COMMIT, VERSION, OCIBuilder


def sha(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


class BinaryImage(OCIBuilder):
    def __init__(self, missing=False, symlink=False, overwrite=False, bad_diff=False, corrupt=False, variant=False):
        super().__init__(attestations=False)
        self.missing, self.symlink, self.overwrite = missing, symlink, overwrite
        self.bad_diff, self.corrupt, self.variant = bad_diff, corrupt, variant

    def image(self, architecture):
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w") as layer:
            for name in sorted(BINARIES):
                if self.missing and name == "memlens-node-context":
                    continue
                content = (architecture + ":" + name).encode()
                item = tarfile.TarInfo(name); item.mode = 0o755; item.size = len(content)
                if self.symlink and name == "memlens-node-context":
                    item.type = tarfile.SYMTYPE; item.linkname = "/private/credential"; item.size = 0
                layer.addfile(item, io.BytesIO(content))
        raw = stream.getvalue(); compressed = gzip.compress(raw, mtime=0)
        digest = sha(compressed)
        self.blobs[digest[7:]] = compressed + (b"corrupt" if self.corrupt else b"")
        layer = {"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digest, "size": len(compressed)}
        layers = [layer, layer] if self.overwrite else [layer]
        diff_id = "sha256:" + "f" * 64 if self.bad_diff else sha(raw)
        config = self.descriptor({"os": "linux", "architecture": architecture,
            "rootfs": {"type": "layers", "diff_ids": [diff_id] * len(layers)},
            "config": {"User": "65532:65532", "Entrypoint": ["/kubectl-memlens"], "Labels": {
                "org.opencontainers.image.version": VERSION, "org.opencontainers.image.revision": COMMIT,
                "org.opencontainers.image.created": BUILD_DATE, "org.opencontainers.image.licenses": "Apache-2.0",
                "org.opencontainers.image.source": "https://github.com/danushkastanley/KubeMemLens"}}},
            "application/vnd.oci.image.config.v1+json")
        manifest = self.descriptor({"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
                                    "config": config, "layers": layers}, "application/vnd.oci.image.manifest.v1+json")
        manifest["platform"] = {"os": "linux", "architecture": architecture}
        if self.variant and architecture == "arm64":
            manifest["platform"]["variant"] = "v8"
        return manifest


class OCIBinaryTest(unittest.TestCase):
    def check(self, builder, architecture="amd64", expected=None):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "image.tar"
            digest = builder.write(archive)
            return inspect_archive(archive, expected or digest, VERSION, COMMIT, BUILD_DATE, architecture)

    def test_each_platform_binary_is_bound_to_its_layer(self):
        for architecture in ("amd64", "arm64"):
            result = self.check(BinaryImage(), architecture)
            self.assertEqual(result["binaries"]["memlens-node-context"], sha((architecture + ":memlens-node-context").encode()))
            self.assertEqual(set(result["binaries"]), BINARIES)

    def test_image_index_must_match_the_trusted_candidate(self):
        with self.assertRaisesRegex(ContractError, "signed candidate image index"):
            self.check(BinaryImage(), expected="sha256:" + "0" * 64)

    def test_corruption_missing_binary_and_unsupported_filesystem_are_rejected(self):
        for builder in (BinaryImage(corrupt=True), BinaryImage(bad_diff=True), BinaryImage(missing=True),
                        BinaryImage(symlink=True), BinaryImage(overwrite=True)):
            with self.assertRaises(ContractError):
                self.check(builder)

    def test_expanded_layer_bound_is_enforced(self):
        with patch("oci_binary.MAX_LAYER", 512), self.assertRaisesRegex(ContractError, "expanded bound"):
            self.check(BinaryImage())

    def test_arm64_v8_descriptor_uses_the_declared_architecture(self):
        self.assertEqual(self.check(BinaryImage(variant=True), "arm64")["architecture"], "arm64")


if __name__ == "__main__":
    unittest.main()
