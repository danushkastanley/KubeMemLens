import copy
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from common import ContractError, digest, instant, load
from finalize_provider_record import ACKNOWLEDGEMENT, finalize
from provider_record import cleanup_cluster
import test_provider_record


class ProviderCleanupTest(unittest.TestCase):
    def setUp(self):
        self.fixture = test_provider_record.ProviderRecordTest()
        self.fixture.setUp()
        self.profile, self.receipt = self.fixture.profile, self.fixture.receipt
        self.evidence = cleanup_cluster(self.fixture.execution, self.fixture.assemble())
        self.attestation = {'schemaVersion': 1, 'recordDigest': self.evidence['recordDigest'],
            'profileDigest': self.profile['profileDigest'], 'independentCheck': True, 'cloudResourcesRemoved': True,
            'checkedAt': '2026-09-11T13:00:00Z'}
        self.attestation['attestationDigest'] = digest(self.attestation, 'attestationDigest')

    def check(self):
        return finalize(self.profile, self.evidence, self.receipt, self.attestation,
                        now=instant('2026-09-11T14:00:00Z'))

    def test_confirmation_preserves_measurements_and_does_not_grant_review(self):
        before = copy.deepcopy(self.evidence)
        record, evaluation = self.check()
        self.assertEqual(self.evidence, before)
        self.assertEqual(record['nodes'], before['nodes'])
        self.assertEqual(record['completedAt'], before['completedAt'])
        self.assertEqual(record['cleanup']['cloudResources'], 'confirmed')
        self.assertNotEqual(record['recordDigest'], before['recordDigest'])
        self.assertEqual(evaluation['outcome'], 'pass')
        self.assertFalse(evaluation['qualified'])
        self.assertEqual(evaluation['reviewState'], 'pending')

    def test_changed_unconfirmed_non_independent_or_bad_time_attestations_fail(self):
        for key, value in (('recordDigest', 'sha256:' + 'f' * 64), ('profileDigest', 'sha256:' + 'f' * 64),
                           ('independentCheck', False), ('cloudResourcesRemoved', False),
                           ('checkedAt', '2026-09-11T12:00:00Z'), ('checkedAt', '2026-09-12T14:00:00Z')):
            self.setUp(); self.attestation[key] = value
            self.attestation['attestationDigest'] = digest(self.attestation, 'attestationDigest')
            with self.subTest(field=key), self.assertRaises(ContractError):
                self.check()

    def test_incomplete_kubernetes_cleanup_and_forged_digest_are_rejected(self):
        self.evidence['cleanup']['rbacRemoved'] = False
        self.evidence['recordDigest'] = digest(self.evidence, 'recordDigest')
        with self.assertRaises(ContractError):
            self.check()
        self.setUp(); self.attestation['attestationDigest'] = 'sha256:' + 'f' * 64
        with self.assertRaises(ContractError):
            self.check()

    def test_cleanup_cannot_turn_failed_replacement_into_passing_measurements(self):
        self.evidence['lifecycle']['providerNodeReplacement']['state'] = 'failed'
        self.evidence['recordDigest'] = digest(self.evidence, 'recordDigest')
        self.attestation['recordDigest'] = self.evidence['recordDigest']
        self.attestation['attestationDigest'] = digest(self.attestation, 'attestationDigest')
        _, result = self.check()
        self.assertEqual(result['outcome'], 'fail')
        self.assertFalse(result['qualified'])

    def test_real_command_retains_bound_inputs_with_private_exclusive_outputs(self):
        with tempfile.TemporaryDirectory() as private:
            root = Path(private)
            inputs = {'profile': self.profile, 'evidence': self.evidence, 'provider-receipt': self.receipt,
                      'cleanup-attestation': self.attestation}
            command = [sys.executable, str(Path(__file__).with_name('finalize_provider_record.py'))]
            for name, value in inputs.items():
                path = root / (name + '.json'); path.write_text(json.dumps(value))
                command += ['--' + name, str(path)]
            output = root / 'final'
            command += ['--output-dir', str(output), '--acknowledge', ACKNOWLEDGEMENT]
            first = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(first.returncode, 0, first.stderr)
            self.assertEqual(output.stat().st_mode & 0o777, 0o700)
            paths = sorted(output.glob('*.json'))
            self.assertEqual(len(paths), 5)
            for path in paths:
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(load(output / 'cleanup-attestation.json'), self.attestation)
            self.assertEqual(load(output / 'qualification-observations.json'), self.evidence)
            self.assertFalse(load(output / 'qualification-evaluation.json')['qualified'])
            second = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(second.returncode, 2)
            self.assertNotIn('private-', first.stdout + first.stderr + second.stdout + second.stderr)


if __name__ == '__main__':
    unittest.main()
