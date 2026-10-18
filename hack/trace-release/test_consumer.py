import copy
import hashlib
from pathlib import Path
import sys
import tempfile
import unittest

from consumer import payload_receipt, payload_request
from manifest import Manifest, ManifestError, canonical
from test_manifest import fixture
from tools import Tool


class ConsumerTests(unittest.TestCase):
    def setUp(self):
        self.manifest = Manifest.read(canonical(fixture()))

    def test_request_is_derived_from_each_manifest_payload(self):
        request = payload_request(self.manifest)
        self.assertEqual(request['imageDigest'], self.manifest.image_digest)
        self.assertEqual(request['sourceCommit'], self.manifest.source_commit)
        for row, item in zip(request['payloads'], self.manifest.artefacts):
            self.assertEqual(row, {'role': item.role, 'name': item.payload.name,
                                   'size': item.payload.size, 'sha256': item.payload.sha256})

    def test_inspector_cannot_invent_authentication_or_substitute_identity(self):
        manifest = self.manifest
        receipt = {'schemaVersion': 1, 'releaseVersion': manifest.version,
                   'sourceCommit': manifest.source_commit, 'imageDigest': manifest.image_digest,
                   'chartImage': 'ghcr.io/danushkastanley/kube-memlens-trace@'+manifest.image_digest,
                   'payloadAgreement': True, 'authenticated': False, 'runtimeExecuted': False,
                   'engineDigest': 'sha256:'+'b'*64, 'programmeIndexDigest': 'sha256:'+'c'*64,
                   'inventories': [{'role': item.role, 'platform': sbom.platform,
                                    'files': [{'name': 'file', 'sha256': 'd'*64}]}
                                   for item in manifest.artefacts for sbom in item.sboms]}
        self.assertEqual(payload_receipt(canonical(receipt), manifest), receipt)
        for key, value in [('authenticated', True), ('runtimeExecuted', True), ('payloadAgreement', 1),
                           ('payloadAgreement', False), ('sourceCommit', 'd'*40), ('schemaVersion', True),
                           ('chartImage', 'different'), ('engineDigest', 'latest'), ('extra', True)]:
            changed = copy.deepcopy(receipt)
            changed[key] = value
            with self.assertRaises(ManifestError): payload_receipt(canonical(changed), manifest)
        del receipt['programmeIndexDigest']
        with self.assertRaises(ManifestError): payload_receipt(canonical(receipt), manifest)

    def test_owned_tool_rejects_changed_bytes_and_symlink(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root/'source'
            source.write_bytes(b'unexecuted tool fixture')
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            tool = Tool.copy(source, digest, root, 'owned')
            source.write_bytes(b'changed')
            self.assertEqual(hashlib.sha256(tool.path.read_bytes()).hexdigest(), digest)
            with self.assertRaises(ManifestError): Tool.copy(source, digest, root, 'wrong')
            link = root/'link'
            link.symlink_to(source)
            with self.assertRaises(ManifestError): Tool.copy(link, digest, root, 'linked')

    def test_real_tool_execution_is_credential_free_and_bounded(self):
        # Use the host's existing interpreter as a caller-pinned test tool, never
        # execute code or a binary supplied by a candidate release bundle.
        source = Path(sys.executable).resolve()
        digest = hashlib.sha256(source.read_bytes()).hexdigest()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tool = Tool.copy(source, digest, root, 'python')
            code, output = tool.run(['-c', 'import os; print(sorted(os.environ))'], root)
            self.assertEqual(code, 0)
            self.assertNotIn(b'AWS_', output)
            self.assertNotIn(b'GITHUB_TOKEN', output)
            with self.assertRaises(ManifestError):
                tool.run(['-c', 'print("x"*100000)'], root, maximum=4096)
            with self.assertRaises(ManifestError):
                tool.run(['-c', 'import time; time.sleep(10)'], root, timeout=0.1)
            with self.assertRaises(ManifestError):
                tool.run(['-c', 'raise SystemExit(2)'], root)
            code, output = tool.run(['-c', 'from pathlib import Path; Path("scratch").write_bytes(b"x"*5000); print("ok")'],
                                    root, maximum=4096, scratch_file_limit=8192)
            self.assertEqual((code, output), (0, b'ok\n'))
            with self.assertRaises(ManifestError):
                tool.run(['-c', 'print("x"*5000)'], root, maximum=4096, scratch_file_limit=8192)


if __name__ == '__main__':
    unittest.main()
