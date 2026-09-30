import copy
import unittest

from manifest import Manifest, ManifestError, canonical
from sbom import SCANNER, WRAPPER, bind_scan, inventories, verify_files
from test_manifest import fixture


class SBOMInventoryTests(unittest.TestCase):
    def setUp(self):
        self.manifest = Manifest.read(canonical(fixture()))
        self.payload = self.manifest.artefacts[2].payload
        self.files = {'launcher': 'a'*64, 'worker': 'b'*64}
        self.scan = {'spdxVersion': 'SPDX-2.3', 'dataLicense': 'CC0-1.0', 'SPDXID': 'SPDXRef-DOCUMENT',
                     'documentNamespace': 'https://example.invalid/spdx/scan',
                     'creationInfo': {'creators': [SCANNER], 'created': '2026-09-30T00:00:00Z'},
                     'packages': [{'SPDXID': 'SPDXRef-image', 'name': 'image',
                                   'checksums': [{'algorithm': 'SHA256', 'checksumValue': 'c'*64}]}],
                     'files': [{'fileName': 'launcher', 'SPDXID': 'SPDXRef-launcher',
                                'checksums': [{'algorithm': 'SHA256', 'checksumValue': 'a'*64}]}],
                     'relationships': [{'spdxElementId': 'SPDXRef-DOCUMENT', 'relatedSpdxElement': 'SPDXRef-image',
                                        'relationshipType': 'DESCRIBES'}]}

    def bind(self):
        return bind_scan(canonical(self.scan), self.payload, 'linux/amd64', self.manifest.version, self.files)

    def test_wrapper_preserves_scanner_digest_and_accounts_for_every_file(self):
        import json
        raw = self.bind()
        value = json.loads(raw)
        self.assertEqual(value['packages'][0], self.scan['packages'][0])
        self.assertEqual(value['files'][0], self.scan['files'][0])
        self.assertEqual(value['documentDescribes'], [WRAPPER])
        self.assertEqual(verify_files(raw, self.files), {'files': 2, 'fileInventoryVerified': True})

    def test_scanner_cannot_supply_a_wrong_or_undeclared_file(self):
        for field, replacement in [('fileName', 'hidden'), ('checksums', [{'algorithm': 'SHA256', 'checksumValue': 'd'*64}])]:
            changed = copy.deepcopy(self.scan)
            changed['files'][0][field] = replacement
            with self.assertRaises(ManifestError):
                bind_scan(canonical(changed), self.payload, 'linux/amd64', self.manifest.version, self.files)

    def test_missing_extra_changed_and_unowned_files_are_rejected(self):
        import json
        value = json.loads(self.bind())
        for mutate in [lambda v: v['files'].pop(),
                       lambda v: v['files'].append(copy.deepcopy(v['files'][0])),
                       lambda v: v['files'][0]['checksums'][0].update(checksumValue='f'*64),
                       lambda v: v.update(relationships=[]),
                       lambda v: v['relationships'].append(copy.deepcopy(v['relationships'][-1]))]:
            changed = copy.deepcopy(value)
            mutate(changed)
            with self.assertRaises(ManifestError): verify_files(canonical(changed), self.files)

    def test_inspector_inventory_is_complete_and_unambiguous(self):
        rows = [{'role': item.role, 'platform': sbom.platform,
                 'files': [{'name': 'file', 'sha256': 'a'*64}]}
                for item in self.manifest.artefacts for sbom in item.sboms]
        self.assertEqual(len(inventories(rows)), 5)
        for mutate in [lambda v: v.pop(), lambda v: v.append(copy.deepcopy(v[0])),
                       lambda v: v[0].update(role='other'),
                       lambda v: v[0]['files'].append(copy.deepcopy(v[0]['files'][0])),
                       lambda v: v[0]['files'][0].update(name='../outside'),
                       lambda v: v[0]['files'][0].update(sha256='tag')]:
            changed = copy.deepcopy(rows)
            mutate(changed)
            with self.assertRaises(ManifestError): inventories(changed)


if __name__ == '__main__':
    unittest.main()
