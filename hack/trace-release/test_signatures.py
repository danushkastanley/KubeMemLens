import hashlib
from pathlib import Path
import tempfile
import unittest

from manifest import Manifest, ManifestError, canonical
from signatures import Cosign, GitHubTrust, LocalTrust, WORKFLOW, ISSUER
from test_manifest import fixture


class SignatureTrustTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.manifest = Manifest.read(canonical(fixture()))

    def file(self, name, data):
        path = self.root/name; path.write_bytes(data)
        return path, hashlib.sha256(data).hexdigest()

    def test_local_key_cannot_authorise_beta(self):
        path, digest = self.file('caller.pub', b'-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----')
        beta = fixture(); beta['releaseVersion'] = '0.1.0-beta.1'
        beta = Manifest.read(canonical(beta))
        trust = LocalTrust(path, digest, beta.version, beta.source_commit)
        with self.assertRaises(ManifestError): trust.arguments(beta, self.root)
        self.assertFalse((self.root/'verification.pub').exists())

    def test_github_trust_is_exact_and_never_disables_transparency_checks(self):
        path, digest = self.file('external-root.json', b'{}')
        beta = fixture(); beta['releaseVersion'] = '0.1.0-beta.1'
        beta = Manifest.read(canonical(beta))
        trust = GitHubTrust(path, digest, beta.version, beta.source_commit)
        args = trust.arguments(beta, self.root)
        self.assertEqual(args, ['--trusted-root', str(self.root/'trusted-root.json'),
                               '--certificate-identity', WORKFLOW+'@refs/tags/v0.1.0-beta.1',
                               '--certificate-oidc-issuer', ISSUER])
        self.assertFalse(any('insecure' in argument or 'regexp' in argument for argument in args))
        with self.assertRaises(ManifestError): trust.arguments(self.manifest, self.root)

    def test_local_root_requires_pinned_external_public_bytes(self):
        path, digest = self.file('caller.pub', b'-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----')
        trust = LocalTrust(path, '0'*64, self.manifest.version, self.manifest.source_commit)
        with self.assertRaises(ManifestError): trust.arguments(self.manifest, self.root)
        private_path, private_digest = self.file('private.pem', b'-----BEGIN PRIVATE KEY-----')
        trust = LocalTrust(private_path, private_digest, self.manifest.version, self.manifest.source_commit)
        with self.assertRaises(ManifestError): trust.arguments(self.manifest, self.root)
        path.write_bytes(b'replaced')
        trust = LocalTrust(path, digest, self.manifest.version, self.manifest.source_commit)
        with self.assertRaises(ManifestError): trust.arguments(self.manifest, self.root)

    def test_verifier_identity_is_checked_without_executing(self):
        path, digest = self.file('unused-verifier', b'not an executable')
        path.chmod(0o700)
        Cosign(path, digest).verify()
        with self.assertRaises(ManifestError): Cosign(path, '0'*64).verify()
        path.chmod(0o600)
        with self.assertRaises(ManifestError): Cosign(path, digest).verify()
        alias = self.root/'alias'; alias.symlink_to(path)
        with self.assertRaises(ManifestError): Cosign(alias, digest).verify()


if __name__ == '__main__':
    unittest.main()
