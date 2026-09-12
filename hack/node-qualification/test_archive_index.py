import hashlib
import json
import unittest

from archive_index import INDEX, wrapper_digest
from common import ContractError


class ArchiveIndexTest(unittest.TestCase):
    def setUp(self):
        self.image = "sha256:" + "a" * 64
        self.wrapper = {"schemaVersion": 2, "mediaType": INDEX,
                        "manifests": [{"mediaType": INDEX, "digest": self.image, "size": 123}]}

    def test_import_wrapper_is_bound_to_its_only_image_child(self):
        raw = json.dumps(self.wrapper).encode()
        self.assertEqual(wrapper_digest(raw, self.image), "sha256:" + hashlib.sha256(raw).hexdigest())

    def test_unrelated_or_additional_child_cannot_be_accepted(self):
        with self.assertRaises(ContractError):
            wrapper_digest(json.dumps(self.wrapper).encode(), "sha256:" + "b" * 64)
        self.wrapper["manifests"].append(dict(self.wrapper["manifests"][0]))
        with self.assertRaises(ContractError):
            wrapper_digest(json.dumps(self.wrapper).encode(), self.image)


if __name__ == "__main__":
    unittest.main()
