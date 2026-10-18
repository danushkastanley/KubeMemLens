"""Verify signed metadata relationships, not scanner completeness or SLSA levels.

SLSA's extensible envelope remains extensible. This project's build parameters
are exact and fail closed. Authenticating the enclosing manifest is separate.
"""
from datetime import datetime
import json
import math
import re
from urllib.parse import urlsplit

from manifest import ManifestError, canonical, invalid_number, require, unique

SOURCE = 'git+https://github.com/danushkastanley/KubeMemLens'
BUILD_TYPE = 'https://github.com/danushkastanley/KubeMemLens/blob/main/docs/ebpf/RELEASE_BUILD.md#v1'
PLATFORM_REFERENCE = 'https://github.com/danushkastanley/KubeMemLens/trace-platform'


def document(data, maximum):
    require(type(data) is bytes and 0 < len(data) <= maximum, 'metadata outside byte bound')
    try:
        value = json.loads(data, object_pairs_hook=unique, parse_constant=invalid_number)
    except (ValueError, UnicodeError, RecursionError) as error:
        raise ManifestError('invalid metadata JSON') from error
    require(type(value) is dict, 'metadata must be an object')
    pending = [(value, 0)]
    count = 0
    while pending:
        item, depth = pending.pop(); count += 1
        require(count <= 200000 and depth <= 32, 'metadata structure outside bound')
        if type(item) is dict:
            pending.extend((entry, depth+1) for pair in item.items() for entry in pair)
        elif type(item) is list:
            pending.extend((entry, depth+1) for entry in item)
        elif type(item) is float:
            require(math.isfinite(item), 'metadata number outside bound')
        elif type(item) is str:
            require(len(item) <= 16384 and not any(ord(c) < 32 and c not in '\n\t' for c in item),
                    'metadata text outside bound')
            require(not any(marker in item for marker in
                            ('/Users/', '/home/runner/', '/private/', '/workspace/',
                             '/tmp/', 'PRIVATE KEY-----', 'Bearer ')),
                    'metadata contains private build material')
    return value


def timestamp(value):
    require(type(value) is str and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z', value),
            'invalid provenance timestamp')
    try:
        return datetime.fromisoformat(value[:-1]+'+00:00')
    except ValueError:
        raise ManifestError('invalid provenance timestamp') from None


def verify_provenance(data, manifest, artefact, builder):
    value = document(data, 1 << 20)
    require(value.get('_type') == 'https://in-toto.io/Statement/v1' and
            value.get('predicateType') == 'https://slsa.dev/provenance/v1',
            'unsupported provenance format')
    # Exactly the packaged payload, not a similarly named output or source hash.
    require(value.get('subject') == [{'name': artefact.payload.name,
                                     'digest': {'sha256': artefact.payload.sha256}}],
            'provenance subject mismatch')
    predicate = value.get('predicate')
    require(type(predicate) is dict, 'missing provenance predicate')
    definition, run = predicate.get('buildDefinition'), predicate.get('runDetails')
    require(type(definition) is dict and type(run) is dict, 'missing provenance build context')
    require(definition.get('buildType') == BUILD_TYPE, 'unexpected provenance build type')
    expected = {'source': SOURCE, 'sourceCommit': manifest.source_commit,
                'releaseVersion': manifest.version, 'traceContract': 1, 'role': artefact.role}
    # Exact canonical parameters reject unknown fields and bool-as-integer aliases.
    require(canonical(definition.get('externalParameters')) == canonical(expected),
            'unexpected external build parameters')
    actual_builder = run.get('builder')
    require(type(actual_builder) is dict and actual_builder.get('id') == builder and
            type(builder) is str and 0 < len(builder) <= 512,
            'unexpected provenance builder')
    dependencies = definition.get('resolvedDependencies')
    require(type(dependencies) is list and 1 <= len(dependencies) <= 128,
            'incomplete resolved dependencies')
    sources = []
    identities = set()
    for dependency in dependencies:
        require(type(dependency) is dict, 'invalid resolved dependency')
        uri, digests = dependency.get('uri'), dependency.get('digest')
        require(type(uri) is str and 0 < len(uri) <= 2048 and not any(c.isspace() for c in uri) and
                type(digests) is dict and digests,
                'invalid resolved dependency')
        try:
            parsed = urlsplit(uri)
        except ValueError:
            raise ManifestError('invalid resolved dependency URI') from None
        require(parsed.scheme in ('git+https', 'https', 'oci', 'pkg', 'file') and
                parsed.username is None and parsed.password is None and not parsed.query and
                not parsed.fragment and not (parsed.scheme == 'file' and parsed.path.startswith('/')),
                'unsafe resolved dependency URI')
        require(uri not in identities, 'duplicate resolved dependency')
        identities.add(uri)
        for algorithm, digest in digests.items():
            require(algorithm in ('sha256', 'gitCommit') and type(digest) is str and
                    re.fullmatch('[a-f0-9]{'+('64' if algorithm == 'sha256' else '40')+'}', digest),
                    'unrecognised dependency digest')
        if uri == SOURCE:
            sources.append(digests)
    require(sources == [{'gitCommit': manifest.source_commit}], 'source dependency mismatch')
    metadata = run.get('metadata')
    require(type(metadata) is dict, 'missing provenance timing')
    start, finish = timestamp(metadata.get('startedOn')), timestamp(metadata.get('finishedOn'))
    require(finish >= start, 'reversed provenance timing')
    return {'builder': builder, 'sourceCommit': manifest.source_commit, 'role': artefact.role}


def verify_sbom(data, payload, platform, version):
    value = document(data, 16 << 20)
    require(value.get('spdxVersion') == 'SPDX-2.3' and value.get('dataLicense') == 'CC0-1.0' and
            value.get('SPDXID') == 'SPDXRef-DOCUMENT', 'unsupported SBOM document')
    namespace = value.get('documentNamespace')
    require(type(namespace) is str and len(namespace) <= 2048 and
            not any(c.isspace() for c in namespace), 'invalid SBOM namespace')
    try:
        parsed = urlsplit(namespace)
        require(parsed.scheme == 'https' and parsed.hostname and parsed.username is None and
                parsed.password is None and not parsed.query and not parsed.fragment and
                (parsed.port is None or 1 <= parsed.port <= 65535), 'invalid SBOM namespace')
    except ValueError:
        raise ManifestError('invalid SBOM namespace') from None
    creation = value.get('creationInfo')
    require(type(creation) is dict and type(creation.get('creators')) is list and
            1 <= len(creation['creators']) <= 16 and
            all(type(item) is str and item.startswith(('Tool: ', 'Organization: '))
                for item in creation['creators']), 'invalid SBOM creation context')
    timestamp(creation.get('created'))
    packages = value.get('packages')
    require(type(packages) is list and 1 <= len(packages) <= 10000, 'invalid SBOM package inventory')
    by_id = {}
    for package in packages:
        require(type(package) is dict and type(package.get('SPDXID')) is str and
                re.fullmatch(r'SPDXRef-[A-Za-z0-9.-]+', package['SPDXID']) and
                package['SPDXID'] not in by_id, 'ambiguous SBOM package identity')
        by_id[package['SPDXID']] = package
    described = value.get('documentDescribes')
    require(type(described) is list and len(described) == 1 and type(described[0]) is str and
            described[0] in by_id, 'missing SBOM primary package')
    root = by_id[described[0]]
    require(root.get('name') == payload.name and root.get('versionInfo') == version,
            'SBOM primary package mismatch')
    require(type(root.get('filesAnalyzed')) is bool and
            all(type(root.get(key)) is str and root[key] for key in
                ('downloadLocation', 'licenseConcluded', 'copyrightText')),
            'incomplete SBOM primary package')
    checksums = root.get('checksums')
    require(checksums ==
            [{'algorithm': 'SHA256', 'checksumValue': payload.sha256}], 'SBOM subject digest mismatch')
    references = root.get('externalRefs', [])
    require(type(references) is list and
            [item for item in references if type(item) is dict and item.get('referenceType') == PLATFORM_REFERENCE] ==
            [{'referenceCategory': 'OTHER', 'referenceType': PLATFORM_REFERENCE,
              'referenceLocator': platform}], 'SBOM platform mismatch')
    return {'platform': platform, 'packages': len(packages), 'payloadSHA256': payload.sha256}
