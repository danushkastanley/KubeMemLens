"""Bounded trace release inventory. Parsing establishes structure, never trust.

Signature/provenance verification must authenticate the original bytes against an
independent trust policy before any artefact is installed or executed.
"""
from dataclasses import dataclass
import hashlib
import json
import re

MAX_MANIFEST_BYTES = 65536
ROLES = ('chart', 'engine', 'image', 'programmes')
PAYLOAD_LIMITS = {'chart': 8 << 20, 'engine': 512 << 20,
                  'image': 1 << 30, 'programmes': 128 << 20}
SBOM_PLATFORMS = {'image': ('linux/amd64', 'linux/arm64'),
                  'chart': ('independent',), 'engine': ('independent',),
                  'programmes': ('independent',)}
RESERVED = {'trace-release.json', 'trace-release.sigstore.json', 'release-subjects.txt'}
SHA256 = re.compile(r'[a-f0-9]{64}')
VERSION = re.compile(r'(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)-(?:dev|beta)\.[1-9][0-9]*')
FILENAME = re.compile(r'[a-z0-9][a-z0-9._-]{0,127}')
IMAGE_REPOSITORY = 'ghcr.io/danushkastanley/kube-memlens-trace'


class ManifestError(ValueError):
    """Fixed diagnostic text; untrusted input is never included in an error."""


def require(condition, category):
    if not condition:
        raise ManifestError(category)


def fields(value, expected):
    require(type(value) is dict and set(value) == set(expected), 'invalid manifest fields')


def match(pattern, value):
    return type(value) is str and pattern.fullmatch(value) is not None


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'),
                       ensure_ascii=True, allow_nan=False)+'\n').encode('ascii')


def unique(items):
    value = {}
    for key, item in items:
        require(key not in value, 'duplicate JSON member')
        value[key] = item
    return value


def invalid_number(_):
    raise ManifestError('non-integer JSON number')


@dataclass(frozen=True)
class Blob:
    name: str
    sha256: str
    size: int

    @classmethod
    def read(cls, value, maximum):
        fields(value, ('name', 'sha256', 'size'))
        require(match(FILENAME, value['name']) and '..' not in value['name'] and
                value['name'] not in RESERVED, 'invalid artefact filename')
        require(match(SHA256, value['sha256']), 'invalid artefact digest')
        require(type(value['size']) is int and 0 < value['size'] <= maximum,
                'artefact size outside bound')
        return cls(value['name'], value['sha256'], value['size'])

    def verify(self, data):
        require(type(data) is bytes and len(data) == self.size and
                hashlib.sha256(data).hexdigest() == self.sha256,
                'artefact content mismatch')


@dataclass(frozen=True)
class SBOM:
    platform: str
    blob: Blob


@dataclass(frozen=True)
class Artefact:
    role: str
    payload: Blob
    sboms: tuple[SBOM, ...]
    provenance: Blob

    @classmethod
    def read(cls, value):
        fields(value, ('role', 'payload', 'sboms', 'provenance'))
        role = value['role']
        require(type(role) is str and role in ROLES, 'unknown artefact role')
        payload = Blob.read(value['payload'], PAYLOAD_LIMITS[role])
        provenance = Blob.read(value['provenance'], 1 << 20)
        rows = value['sboms']
        expected = SBOM_PLATFORMS[role]
        require(type(rows) is list and len(rows) == len(expected), 'incomplete SBOM platforms')
        sboms = []
        for platform, row in zip(expected, rows):
            fields(row, ('platform', 'blob'))
            require(row['platform'] == platform, 'invalid SBOM platform order')
            sboms.append(SBOM(platform, Blob.read(row['blob'], 16 << 20)))
        return cls(role, payload, tuple(sboms), provenance)

    def blobs(self):
        return (self.payload, *(item.blob for item in self.sboms), self.provenance)


@dataclass(frozen=True)
class Manifest:
    version: str
    source_commit: str
    image_digest: str
    artefacts: tuple[Artefact, ...]
    original: bytes

    @classmethod
    def read(cls, data):
        require(type(data) is bytes and 0 < len(data) <= MAX_MANIFEST_BYTES,
                'manifest size outside bound')
        try:
            value = json.loads(data, object_pairs_hook=unique,
                               parse_float=invalid_number, parse_constant=invalid_number)
        except (ValueError, UnicodeError, RecursionError) as error:
            raise ManifestError('invalid manifest JSON') from error
        fields(value, ('schemaVersion', 'releaseVersion', 'sourceCommit',
                       'traceContract', 'image', 'artefacts'))
        require(type(value['schemaVersion']) is int and value['schemaVersion'] == 1,
                'unsupported manifest schema')
        require(type(value['traceContract']) is int and value['traceContract'] == 1,
                'unsupported trace contract')
        require(match(VERSION, value['releaseVersion']) and len(value['releaseVersion']) <= 64,
                'invalid development or beta version')
        require(type(value['sourceCommit']) is str and
                re.fullmatch('[a-f0-9]{40}', value['sourceCommit']), 'invalid source commit')
        image = value['image']
        fields(image, ('repository', 'digest'))
        require(image['repository'] == IMAGE_REPOSITORY and type(image['digest']) is str and
                image['digest'].startswith('sha256:') and match(SHA256, image['digest'][7:]),
                'invalid immutable image reference')
        rows = value['artefacts']
        require(type(rows) is list and len(rows) == len(ROLES), 'incomplete artefact inventory')
        artefacts = tuple(Artefact.read(row) for row in rows)
        require(tuple(item.role for item in artefacts) == ROLES, 'invalid artefact role order')
        names = [blob.name for item in artefacts for blob in item.blobs()]
        require(len(set(names)) == len(names), 'duplicate artefact filename')
        require(canonical(value) == data, 'non-canonical manifest')
        return cls(value['releaseVersion'], value['sourceCommit'], image['digest'], artefacts, data)

    def blobs(self):
        return tuple(blob for item in self.artefacts for blob in item.blobs())

    def subjects(self):
        """Derived checksum subjects; no separately supplied inventory is accepted."""
        return ''.join(f'{blob.sha256}  {blob.name}\n'
                       for blob in sorted(self.blobs(), key=lambda item: item.name)).encode('ascii')

    def asset_names(self):
        return tuple(sorted((*RESERVED, *(blob.name for blob in self.blobs()))))
