import copy
import unittest

from manifest import Manifest, ManifestError, canonical
from metadata import BUILD_TYPE, PLATFORM_REFERENCE, SOURCE, verify_provenance, verify_sbom
from test_manifest import fixture

BUILDER = 'https://example.invalid/test-builder'


def provenance(manifest, artefact):
    return {'_type': 'https://in-toto.io/Statement/v1',
            'subject': [{'name': artefact.payload.name, 'digest': {'sha256': artefact.payload.sha256}}],
            'predicateType': 'https://slsa.dev/provenance/v1',
            'predicate': {'buildDefinition': {'buildType': BUILD_TYPE,
                'externalParameters': {'source': SOURCE, 'sourceCommit': manifest.source_commit,
                    'releaseVersion': manifest.version, 'traceContract': 1, 'role': artefact.role},
                'resolvedDependencies': [{'uri': SOURCE, 'digest': {'gitCommit': manifest.source_commit}}]},
                'runDetails': {'builder': {'id': BUILDER},
                               'metadata': {'startedOn': '2026-09-30T00:00:00Z',
                                            'finishedOn': '2026-09-30T00:01:00Z'}}}}


def sbom(payload, platform, version):
    return {'spdxVersion': 'SPDX-2.3', 'dataLicense': 'CC0-1.0', 'SPDXID': 'SPDXRef-DOCUMENT',
            'documentNamespace': 'https://example.invalid/spdx/test',
            'creationInfo': {'creators': ['Tool: synthetic-test'], 'created': '2026-09-30T00:00:00Z'},
            'documentDescribes': ['SPDXRef-release'],
            'packages': [{'SPDXID': 'SPDXRef-release', 'name': payload.name, 'versionInfo': version,
                          'filesAnalyzed': False, 'downloadLocation': 'NOASSERTION',
                          'licenseConcluded': 'NOASSERTION', 'copyrightText': 'NOASSERTION',
                          'checksums': [{'algorithm': 'SHA256', 'checksumValue': payload.sha256}],
                          'externalRefs': [{'referenceCategory': 'OTHER', 'referenceType': PLATFORM_REFERENCE,
                                            'referenceLocator': platform}]}]}


class MetadataTests(unittest.TestCase):
    def setUp(self):
        self.manifest = Manifest.read(canonical(fixture()))
        self.artefact = self.manifest.artefacts[0]
        self.provenance = provenance(self.manifest, self.artefact)
        self.sbom = sbom(self.artefact.payload, 'independent', self.manifest.version)

    def verify_provenance(self, value):
        return verify_provenance(canonical(value), self.manifest, self.artefact, BUILDER)

    def verify_sbom(self, value):
        return verify_sbom(canonical(value), self.artefact.payload, 'independent', self.manifest.version)

    def test_bound_subject_parameters_source_builder_and_timing_pass(self):
        result = self.verify_provenance(self.provenance)
        self.assertEqual(result['sourceCommit'], self.manifest.source_commit)
        self.assertEqual(result['role'], 'chart')
        # Standard envelope fields can evolve; project input parameters cannot.
        self.provenance['futureDescription'] = {'message': 'non-executable explanation', 'futureFraction': 0.5}
        self.verify_provenance(self.provenance)

    def test_wrong_subject_builder_source_or_parameters_fail(self):
        variants = []
        value = copy.deepcopy(self.provenance); value['subject'][0]['digest']['sha256'] = 'c'*64
        variants.append(value)
        value = copy.deepcopy(self.provenance); value['subject'].append(copy.deepcopy(value['subject'][0]))
        variants.append(value)
        value = copy.deepcopy(self.provenance); value['predicate']['runDetails']['builder']['id'] = 'untrusted'
        variants.append(value)
        for key, replacement in [('sourceCommit', 'c'*40), ('role', 'image'), ('traceContract', True),
                                 ('acceptUnverified', True), ('releaseVersion', '0.1.0-beta.1')]:
            value = copy.deepcopy(self.provenance)
            value['predicate']['buildDefinition']['externalParameters'][key] = replacement
            variants.append(value)
        value = copy.deepcopy(self.provenance)
        value['predicate']['buildDefinition']['resolvedDependencies'][0]['digest']['gitCommit'] = 'c'*40
        variants.append(value)
        for value in variants:
            with self.assertRaises(ManifestError): self.verify_provenance(value)

    def test_dependency_and_time_ambiguity_fail(self):
        for dependencies in [[], None, [{'uri': SOURCE, 'digest': {'gitCommit': 'c'*40}}],
                             [{'uri': SOURCE, 'digest': {'gitCommit': 'a'*40}}]*2,
                             [{'uri': 'https://name:secret@example.invalid/x', 'digest': {'sha256': 'a'*64}}],
                             [{'uri': 'file:/root/local', 'digest': {'sha256': 'a'*64}}],
                             [{'uri': 'https://[invalid', 'digest': {'sha256': 'a'*64}}]]:
            value = copy.deepcopy(self.provenance)
            value['predicate']['buildDefinition']['resolvedDependencies'] = dependencies
            with self.assertRaises(ManifestError): self.verify_provenance(value)
        for ending in ['2026-09-29T00:00:00Z', 'not-time', '2026-99-30T00:00:00Z', None]:
            value = copy.deepcopy(self.provenance)
            value['predicate']['runDetails']['metadata']['finishedOn'] = ending
            with self.assertRaises(ManifestError): self.verify_provenance(value)

    def test_spdx_subject_digest_platform_and_version_pass(self):
        result = self.verify_sbom(self.sbom)
        self.assertEqual(result['payloadSHA256'], self.artefact.payload.sha256)
        self.assertEqual(result['packages'], 1)

    def test_spdx_cannot_substitute_payload_or_platform(self):
        for field, replacement in [('name', 'other.tar'), ('versionInfo', '0.1.0-beta.2'),
                                   ('filesAnalyzed', None), ('downloadLocation', None),
                                   ('checksums', [{'algorithm': 'SHA256', 'checksumValue': 'c'*64}]),
                                   ('externalRefs', [])]:
            value = copy.deepcopy(self.sbom); value['packages'][0][field] = replacement
            with self.assertRaises(ManifestError): self.verify_sbom(value)
        for described in [[], ['SPDXRef-absent'], ['SPDXRef-release']*2, [None]]:
            value = copy.deepcopy(self.sbom); value['documentDescribes'] = described
            with self.assertRaises(ManifestError): self.verify_sbom(value)
        value = copy.deepcopy(self.sbom); value['packages'].append(copy.deepcopy(value['packages'][0]))
        with self.assertRaises(ManifestError): self.verify_sbom(value)

    def test_invalid_sbom_namespaces_fail_without_uri_exceptions(self):
        for namespace in ['https://[invalid', 'https://user:secret@example.invalid/x',
                          'https://example.invalid:wrong/x', 'https://space invalid/x',
                          'file:/somewhere', 'https://example.invalid/x?token=value']:
            value = copy.deepcopy(self.sbom); value['documentNamespace'] = namespace
            with self.assertRaises(ManifestError): self.verify_sbom(value)

    def test_private_build_material_duplicate_json_and_deep_input_fail(self):
        for text in ['/Users/person/build', '/home/runner/work/private', '/tmp/key',
                     '-----BEGIN PRIVATE KEY-----', 'Bearer secret']:
            value = copy.deepcopy(self.provenance); value['extra'] = text
            with self.assertRaises(ManifestError): self.verify_provenance(value)
        raw = canonical(self.sbom).replace(b'"spdxVersion":"SPDX-2.3"',
                                          b'"spdxVersion":"SPDX-2.3","spdxVersion":"SPDX-2.3"')
        with self.assertRaises(ManifestError):
            verify_sbom(raw, self.artefact.payload, 'independent', self.manifest.version)
        value = self.provenance
        for _ in range(40): value = {'nested': value}
        with self.assertRaises(ManifestError): self.verify_provenance(value)


if __name__ == '__main__':
    unittest.main()
