import copy
from dataclasses import FrozenInstanceError
import hashlib
import json
import unittest

from manifest import Manifest, ManifestError, ROLES, SBOM_PLATFORMS, canonical


def descriptor(name, data=b'bounded fixture'):
    return {'name': name, 'sha256': hashlib.sha256(data).hexdigest(), 'size': len(data)}


def fixture():
    return {'schemaVersion': 1, 'releaseVersion': '0.1.0-dev.1',
            'sourceCommit': 'a'*40, 'traceContract': 1,
            'image': {'repository': 'ghcr.io/danushkastanley/kube-memlens-trace',
                      'digest': 'sha256:'+'b'*64},
            'artefacts': [
                {'role': role, 'payload': descriptor(role+'.tar'),
                 'sboms': [{'platform': platform,
                            'blob': descriptor(role+'-'+platform.replace('/', '-')+'.spdx.json')}
                           for platform in SBOM_PLATFORMS[role]],
                 'provenance': descriptor(role+'.provenance.json')}
                for role in ROLES]}


class ManifestTests(unittest.TestCase):
    def reject(self, value):
        with self.assertRaises(ManifestError):
            Manifest.read(canonical(value))

    def test_inventory_has_one_owner_and_preserves_signed_bytes(self):
        value = fixture()
        raw = canonical(value)
        result = Manifest.read(raw)
        self.assertEqual(result.original, raw)
        self.assertEqual(len(result.blobs()), 13)
        self.assertEqual(len(result.asset_names()), 16)
        lines = result.subjects().decode().splitlines()
        self.assertEqual(lines, sorted(lines, key=lambda line: line.split('  ')[1]))
        self.assertEqual({line.split('  ')[1] for line in lines},
                         {blob.name for blob in result.blobs()})
        value['artefacts'][0]['payload']['sha256'] = 'c'*64
        self.assertEqual(result.original, raw)
        with self.assertRaises(FrozenInstanceError):
            result.artefacts[0].payload.sha256 = 'c'*64
        for blob in result.blobs():
            blob.verify(b'bounded fixture')
            with self.assertRaises(ManifestError):
                blob.verify(b'changed fixture')

    def test_only_explicit_development_and_beta_identities(self):
        for version in ['0.1.0-dev.1', '0.1.0-beta.1', '1.20.3-beta.42']:
            value = fixture(); value['releaseVersion'] = version
            self.assertEqual(Manifest.read(canonical(value)).version, version)
        for version in ['latest', 'v0.1.0-beta.1', '0.1.0', '0.1.0-beta.0',
                        '01.1.0-beta.1', '0.1.0-beta.1+local', '0.1.0-dev.1\n']:
            value = fixture(); value['releaseVersion'] = version
            self.reject(value)

    def test_unknown_or_missing_top_level_fields_fail(self):
        original = fixture()
        for name in original:
            value = copy.deepcopy(original); del value[name]
            self.reject(value)
        value = fixture(); value['accepted'] = True
        self.reject(value)
        for key in ['schemaVersion', 'traceContract']:
            for number in [True, 0, 2, None, '1']:
                value = fixture(); value[key] = number
                self.reject(value)

    def test_duplicate_ambiguous_or_noncanonical_json_fails(self):
        raw = canonical(fixture())
        malformed = [b'', b'null', b'[]', b' '*65537, raw+b'{}',
                     raw.replace(b'"schemaVersion":1', b'"schemaVersion":1,"schemaVersion":1'),
                     raw.replace(b'"traceContract":1', b'"traceContract":1.0'),
                     raw.replace(b'"traceContract":1', b'"traceContract":NaN'),
                     json.dumps(fixture(), indent=2).encode(), raw+b'\n',
                     b'['*10000+b']'*10000]
        for data in malformed:
            with self.subTest(size=len(data)), self.assertRaises(ManifestError):
                Manifest.read(data)

    def test_image_reference_cannot_be_mutable_or_retargeted(self):
        for image in [None, {}, {'repository': 'other', 'digest': 'sha256:'+'b'*64},
                      {'repository': fixture()['image']['repository'], 'digest': 'latest'},
                      dict(fixture()['image'], tag='latest'),
                      dict(fixture()['image'], digest='sha256:'+'B'*64)]:
            value = fixture(); value['image'] = image
            self.reject(value)

    def test_exact_roles_and_platforms_are_required(self):
        for mode in ['missing', 'extra', 'duplicate', 'reordered', 'unknown']:
            value = fixture(); rows = value['artefacts']
            if mode == 'missing': rows.pop()
            if mode == 'extra': rows.append(copy.deepcopy(rows[0]))
            if mode == 'duplicate': rows[1] = copy.deepcopy(rows[0])
            if mode == 'reordered': rows.reverse()
            if mode == 'unknown': rows[0]['role'] = 'plugin'
            self.reject(value)
        for platform in ['linux/arm64', 'linux/amd64/v2', 'any', None]:
            value = fixture(); value['artefacts'][2]['sboms'][0]['platform'] = platform
            self.reject(value)
        value = fixture(); value['artefacts'][2]['sboms'].pop()
        self.reject(value)

    def test_blobs_have_safe_unique_names_and_bounded_sizes(self):
        for name in ['../secret', '/tmp/data', 'a/b', '.secret', 'x\nfile',
                     'x;command', 'a'*129, 'trace-release.json', 'release-subjects.txt']:
            value = fixture(); value['artefacts'][0]['payload']['name'] = name
            self.reject(value)
        for size in [0, -1, True, '10', None, 8*1024*1024+1]:
            value = fixture(); value['artefacts'][0]['payload']['size'] = size
            self.reject(value)
        value = fixture(); value['artefacts'][1]['payload']['name'] = value['artefacts'][0]['payload']['name']
        self.reject(value)
        for key in ['name', 'sha256', 'size']:
            value = fixture(); del value['artefacts'][0]['payload'][key]
            self.reject(value)

    def test_errors_do_not_echo_untrusted_content(self):
        value = fixture(); value['artefacts'][0]['payload']['name'] = 'secret\npath'
        try:
            Manifest.read(canonical(value))
        except ManifestError as error:
            self.assertNotIn('secret', str(error))
        else:
            self.fail('unsafe path accepted')


if __name__ == '__main__':
    unittest.main()
