"""Check the rendered collector port through evidence writing and bundle review."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from bundle_semantics import validate_supported_bundle
from bundle_test_support import write_supported_evidence_files
from evidence_manifest import create_manifest, ManifestError

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[1]


class LifecyclePortTests(unittest.TestCase):
    def pending(self, profile_id):
        value = json.loads((ROOT / 'fixtures/gke-cos-pass.json').read_text())
        value.pop('reviewedAt'); value.pop('reviewDueAt')
        profile = json.loads((ROOT / (profile_id + '.json')).read_text())
        value['profile'] = {'id': profile_id, 'digest': profile['profileDigest']}
        if profile_id.startswith('eks-'):
            value['environment'].update(provider='eks-managed-nodes',
                nodeImage='AL2023_x86_64_STANDARD@1.36.1-20260930',
                cniName='Amazon VPC CNI v1.22.4-eksbuild.3 network-policy=enabled')
        return value

    def validate_port(self, profile_id, port):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pending = self.pending(profile_id)
            receipt = {'schemaVersion': 1, 'profile': pending['profile']}
            receipt['receiptDigest'] = 'sha256:' + hashlib.sha256(
                json.dumps(receipt, sort_keys=True).encode()).hexdigest()
            pending['artefacts']['providerReceiptDigest'] = receipt['receiptDigest']
            write_supported_evidence_files(root, pending, receipt)
            lifecycle = json.loads((root / 'lifecycle.json').read_text())
            lifecycle['checks']['networkPolicy']['servicePort'] = port
            (root / 'lifecycle.json').write_text(json.dumps(lifecycle))
            (root / 'evidence-manifest.json').unlink()
            manifest = create_manifest(root, pending['artefacts']['probeImageDigest'])
            pending['artefacts']['evidenceManifestDigest'] = manifest['manifestDigest']
            (root / 'provider-qualification.pending.json').write_text(json.dumps(pending))
            return validate_supported_bundle(root, pending, receipt)

    def test_supported_provider_ports_are_accepted(self):
        for profile, port in [('eks-al2023-containerd-amd64', 8443),
                              ('gke-cos-containerd-amd64', 443)]:
            with self.subTest(profile=profile):
                self.validate_port(profile, port)

    def test_wrong_and_non_integer_ports_are_rejected(self):
        for profile, port in [('eks-al2023-containerd-amd64', 443),
                              ('gke-cos-containerd-amd64', 8443),
                              ('eks-al2023-containerd-amd64', '8443'),
                              ('eks-al2023-containerd-amd64', 8443.0)]:
            with self.subTest(profile=profile, port=port), self.assertRaises(ManifestError):
                self.validate_port(profile, port)

    def test_writer_records_the_verified_rendered_port(self):
        for port in (443, 8443):
            with self.subTest(port=port), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / 'doctor.json').write_text(json.dumps({'checks': [{}], 'mapping': {'mapped': 1}}))
                (root / 'helm-history.json').write_text(json.dumps([{'revision': 2}, {'revision': 3}]))
                (root / 'expected-service.json').write_text(json.dumps({'ports': [{'port': port}]}))
                environment = dict(os.environ, artifact_dir=directory, work_dir=directory,
                    image_digest='sha256:' + '1' * 64, chart_digest='sha256:' + '2' * 64,
                    values_digest='sha256:' + '3' * 64, source_commit='4' * 40,
                    probe_image='fixture@sha256:' + '5' * 64, cgroup_version='v2',
                    cni_name='fixture', linux_nodes='2', windows_nodes='0', desired='2', ready='2',
                    first_explanation_seconds='12', agent_recovery_seconds='8',
                    collector_recovery_seconds='9', node_recovery_seconds='20')
                subprocess.run(['bash', '-c', 'set -euo pipefail\n'
                    'source hack/lib/provider-qualification-evidence.sh\nwrite_lifecycle_evidence'],
                    cwd=REPO, env=environment, check=True, capture_output=True, timeout=10)
                result = json.loads((root / 'lifecycle.json').read_text())
                self.assertEqual(result['checks']['networkPolicy']['servicePort'], port)


if __name__ == '__main__':
    unittest.main()
