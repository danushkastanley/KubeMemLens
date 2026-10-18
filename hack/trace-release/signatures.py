"""Authenticate the manifest using caller-owned trust, never bundled trust keys.

Local keys are restricted to development artefacts. Public beta verification
requires the exact GitHub workflow/tag identity and a separately pinned Sigstore
trusted root. This module does not establish build completeness or install safety.
"""
from dataclasses import dataclass
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile

from bundle import open_directory, open_regular, read_small, write_new
from manifest import ManifestError, SHA256, match, require

WORKFLOW = 'https://github.com/danushkastanley/KubeMemLens/.github/workflows/trace-release.yml'
LOCAL_BUILDER = 'https://github.com/danushkastanley/KubeMemLens/trace-local-build/v1'
ISSUER = 'https://token.actions.githubusercontent.com'


def pinned_file(path, digest, maximum):
    path = Path(path)
    require(path.is_absolute() and match(SHA256, digest), 'invalid external trust input')
    directory = open_directory(path.parent)
    try:
        data = read_small(directory, path.name, maximum)
    finally:
        os.close(directory)
    require(hashlib.sha256(data).hexdigest() == digest, 'external trust digest mismatch')
    return data


@dataclass(frozen=True)
class Cosign:
    path: Path
    sha256: str

    def verify(self):
        path = Path(self.path)
        require(path.is_absolute() and match(SHA256, self.sha256), 'invalid verifier identity')
        directory = open_directory(path.parent)
        digest = hashlib.sha256()
        try:
            with open_regular(directory, path.name, 256 << 20) as source:
                for block in iter(lambda: source.read(1 << 20), b''):
                    digest.update(block)
        finally:
            os.close(directory)
        require(digest.hexdigest() == self.sha256 and os.access(path, os.X_OK),
                'verifier identity mismatch')


@dataclass(frozen=True)
class LocalTrust:
    public_key: Path
    sha256: str
    version: str
    source_commit: str

    @property
    def builder(self):
        return LOCAL_BUILDER

    def arguments(self, manifest, private):
        require(manifest.version == self.version and '-dev.' in self.version and
                manifest.source_commit == self.source_commit, 'local trust is development-only')
        data = pinned_file(self.public_key, self.sha256, 16384)
        require(b'-----BEGIN PUBLIC KEY-----' in data and b'PRIVATE' not in data,
                'public verification key required')
        write_new(private, 'verification.pub', data)
        # An explicitly local test signature has no public transparency evidence.
        # This option is never present on the GitHub release path.
        return ['--key', str(private/'verification.pub'), '--insecure-ignore-tlog=true']


@dataclass(frozen=True)
class GitHubTrust:
    trusted_root: Path
    sha256: str
    version: str
    source_commit: str

    @property
    def builder(self):
        return WORKFLOW+'@refs/tags/v'+self.version

    def arguments(self, manifest, private):
        require(manifest.version == self.version and '-beta.' in self.version and
                manifest.source_commit == self.source_commit, 'release trust identity mismatch')
        data = pinned_file(self.trusted_root, self.sha256, 4 << 20)
        write_new(private, 'trusted-root.json', data)
        return ['--trusted-root', str(private/'trusted-root.json'),
                '--certificate-identity', self.builder, '--certificate-oidc-issuer', ISSUER]


@dataclass(frozen=True)
class AuthenticatedManifest:
    manifest_sha256: str
    signature_sha256: str
    trust_sha256: str
    builder: str
    authority: str


def verify_manifest(directory, manifest, tool, trust):
    require(type(trust) in (LocalTrust, GitHubTrust), 'explicit signature trust required')
    require(type(tool) is Cosign, 'explicit verifier identity required')
    directory = Path(directory)
    opened = open_directory(directory)
    try:
        raw = read_small(opened, 'trace-release.json', 65536)
        signature = read_small(opened, 'trace-release.sigstore.json', 4 << 20)
    finally:
        os.close(opened)
    require(raw == manifest.original, 'manifest changed before signature verification')
    tool.verify()
    with tempfile.TemporaryDirectory(prefix='kml-trace-signature-') as temporary:
        private = Path(temporary)
        # All verification inputs are owned copies. The tool cannot obtain keys,
        # configuration, tokens or executables from the untrusted bundle.
        arguments = trust.arguments(manifest, private)
        write_new(private, 'manifest.json', raw)
        write_new(private, 'signature.json', signature)
        command = [str(tool.path), 'verify-blob', '--timeout=30s',
                   '--bundle', str(private/'signature.json'), *arguments,
                   str(private/'manifest.json')]
        environment = {'PATH': '/usr/bin:/bin', 'TMPDIR': str(private),
                       'XDG_CACHE_HOME': str(private/'cache'), 'GOMAXPROCS': '2'}
        try:
            result = subprocess.run(command, env=environment, stdin=subprocess.DEVNULL,
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                    timeout=35, check=False)
        except (OSError, subprocess.TimeoutExpired):
            raise ManifestError('signature verifier failed') from None
        require(result.returncode == 0, 'manifest signature verification failed')
    return AuthenticatedManifest(hashlib.sha256(raw).hexdigest(), hashlib.sha256(signature).hexdigest(),
                                 trust.sha256, trust.builder, 'local-development' if type(trust) is LocalTrust else 'github-release')
