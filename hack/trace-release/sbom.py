"""Bind real scanner output to an inspected payload and its complete file set.

Preserve scanner packages and checksums. An added outer package describes the
downloadable archive; an image scanner's original configuration digest stays
attached to its own package. File accounting is not a licence audit or proof
that every ecosystem dependency was discoverable by the scanner.
"""
import copy
import hashlib

from manifest import SBOM_PLATFORMS, SHA256, canonical, fields, match, require
from metadata import PLATFORM_REFERENCE, document, verify_sbom

WRAPPER = 'SPDXRef-TraceReleasePayload'
SCANNER = 'Tool: syft-1.49.0'


def inventories(rows):
    expected = [(role, platform) for role, platforms in SBOM_PLATFORMS.items() for platform in platforms]
    # Manifest order is explicit; the mapping above is not another asset inventory.
    expected = sorted(expected)
    require(type(rows) is list and len(rows) == len(expected), 'missing payload file inventories')
    result = {}
    for row in rows:
        fields(row, ('role', 'platform', 'files'))
        key = (row['role'], row['platform'])
        require(all(type(part) is str for part in key) and key in expected and key not in result,
                'unexpected payload file inventory')
        require(type(row['files']) is list and 1 <= len(row['files']) <= 10000,
                'file inventory outside bound')
        files = {}
        for item in row['files']:
            fields(item, ('name', 'sha256'))
            name = item['name']
            require(type(name) is str and 0 < len(name) <= 1024 and
                    not name.startswith('/') and not any(part in ('', '.', '..') for part in name.split('/')) and
                    all(32 < ord(character) < 127 for character in name) and '\\' not in name and
                    name not in files and match(SHA256, item['sha256']), 'invalid inventory file')
            files[name] = item['sha256']
        result[key] = files
    return result


def file_rows(value):
    rows = value.get('files', [])
    require(type(rows) is list and len(rows) <= 10000, 'invalid SBOM file inventory')
    result, identities = {}, set()
    for row in rows:
        require(type(row) is dict, 'invalid SBOM file')
        name, identity = row.get('fileName'), row.get('SPDXID')
        require(type(name) is str and name not in result and type(identity) is str and
                identity.startswith('SPDXRef-') and identity not in identities, 'ambiguous SBOM file')
        checksums = row.get('checksums')
        require(type(checksums) is list, 'missing SBOM file checksum')
        hashes = [item.get('checksumValue') for item in checksums
                  if type(item) is dict and item.get('algorithm') == 'SHA256']
        require(len(hashes) == 1 and match(SHA256, hashes[0]), 'invalid SBOM file checksum')
        result[name] = (hashes[0], identity)
        identities.add(identity)
    return result


def bind_scan(raw, payload, platform, version, files):
    value = copy.deepcopy(document(raw, 16 << 20))
    require(SCANNER in value.get('creationInfo', {}).get('creators', []), 'unexpected SBOM scanner')
    packages, relationships = value.get('packages'), value.get('relationships')
    require(type(packages) is list and type(relationships) is list, 'missing scanner relationships')
    roots = [item.get('relatedSpdxElement') for item in relationships if type(item) is dict and
             item.get('spdxElementId') == 'SPDXRef-DOCUMENT' and item.get('relationshipType') == 'DESCRIBES']
    require(len(roots) == 1 and type(roots[0]) is str and
            value.get('documentDescribes', roots) == roots and
            sum(item.get('SPDXID') == roots[0] for item in packages if type(item) is dict) == 1 and
            all(type(item) is dict and item.get('SPDXID') != WRAPPER for item in packages),
            'ambiguous scanner primary package')
    scanned = file_rows(value)
    require(all(name in files and digest == files[name] for name, (digest, _) in scanned.items()),
            'scanner file differs from inspected payload')
    value.setdefault('files', [])
    for name, digest in sorted(files.items()):
        if name in scanned:
            continue
        identity = 'SPDXRef-TraceFile-'+hashlib.sha256(name.encode()).hexdigest()
        value['files'].append({'fileName': name, 'SPDXID': identity,
                               'checksums': [{'algorithm': 'SHA256', 'checksumValue': digest}],
                               'licenseConcluded': 'NOASSERTION', 'licenseInfoInFiles': ['NOASSERTION'],
                               'copyrightText': 'NOASSERTION'})
    value['documentDescribes'] = [WRAPPER]
    for row in relationships:
        if row.get('spdxElementId') == 'SPDXRef-DOCUMENT' and row.get('relationshipType') == 'DESCRIBES':
            row['relatedSpdxElement'] = WRAPPER
    relationships.append({'spdxElementId': WRAPPER, 'relatedSpdxElement': roots[0], 'relationshipType': 'CONTAINS'})
    for _, identity in file_rows(value).values():
        relationships.append({'spdxElementId': WRAPPER, 'relatedSpdxElement': identity, 'relationshipType': 'CONTAINS'})
    packages.append({'name': payload.name, 'SPDXID': WRAPPER, 'versionInfo': version,
                     'downloadLocation': 'NOASSERTION', 'filesAnalyzed': False,
                     'checksums': [{'algorithm': 'SHA256', 'checksumValue': payload.sha256}],
                     'licenseConcluded': 'NOASSERTION', 'licenseDeclared': 'NOASSERTION',
                     'copyrightText': 'NOASSERTION', 'primaryPackagePurpose': 'FILE',
                     'externalRefs': [{'referenceCategory': 'OTHER', 'referenceType': PLATFORM_REFERENCE,
                                       'referenceLocator': platform}]})
    value['creationInfo']['creators'].append('Tool: kube-memlens-trace-release-v1')
    encoded = canonical(value)
    verify_sbom(encoded, payload, platform, version)
    verify_files(encoded, files)
    return encoded


def verify_files(raw, expected):
    value = document(raw, 16 << 20)
    require(SCANNER in value.get('creationInfo', {}).get('creators', []), 'unexpected SBOM scanner')
    files = file_rows(value)
    require({name: item[0] for name, item in files.items()} == expected,
            'SBOM file inventory differs from inspected payload')
    links = value.get('relationships')
    require(type(links) is list, 'missing SBOM relationships')
    root = value.get('documentDescribes')
    require(type(root) is list and len(root) == 1 and type(root[0]) is str, 'missing SBOM root')
    contained = [link.get('relatedSpdxElement') for link in links if type(link) is dict and
                 link.get('spdxElementId') == root[0] and link.get('relationshipType') == 'CONTAINS']
    require(all(contained.count(identity) == 1 for _, identity in files.values()),
            'SBOM file ownership differs from payload')
    return {'files': len(files), 'fileInventoryVerified': True}
