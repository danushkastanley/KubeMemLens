import hashlib
import json
import unittest

from common import ContractError
from local_image_graph import INDEX, MANIFEST, inspect


class LocalImageGraphTest(unittest.TestCase):
    def setUp(self):
        self.blobs = {}
        self.config = self.add({"os": "linux", "architecture": "arm64"})
        self.manifest = self.add({"mediaType": MANIFEST, "config": {"digest": self.config}})
        self.index = self.add({"mediaType": INDEX, "manifests": [{"digest": self.manifest,
                             "platform": {"os": "linux", "architecture": "arm64"}}]})

    def add(self, value):
        data = json.dumps(value, separators=(",", ":"))
        digest = "sha256:" + hashlib.sha256(data.encode()).hexdigest()
        self.blobs[digest] = data
        return digest

    def run_command(self, argv, **kwargs):
        self.assertEqual(argv[:8], ["docker", "exec", "owned-node", "ctr", "-n", "k8s.io", "content", "get"])
        return self.blobs[argv[-1]]

    def test_index_and_platform_manifest_are_resolved_to_the_same_config(self):
        for root in (self.index, self.manifest):
            result = inspect("owned-node", root, "arm64", self.run_command)
            self.assertEqual(result["imageDigest"], root)
            self.assertEqual(result["platformManifestDigest"], self.manifest)
            self.assertEqual(result["imageConfigDigest"], self.config)

    def test_changed_content_or_missing_architecture_is_rejected(self):
        with self.assertRaises(ContractError):
            inspect("owned-node", self.index, "amd64", self.run_command)
        self.blobs[self.config] = '{"os":"linux","architecture":"amd64"}'
        with self.assertRaisesRegex(ContractError, "descriptor bytes"):
            inspect("owned-node", self.index, "arm64", self.run_command)


if __name__ == "__main__":
    unittest.main()
