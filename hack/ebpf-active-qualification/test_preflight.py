import base64
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from preflight import FIELDS, read_configuration, certificate_lifetimes
from profile import load_profile


class PreflightTests(unittest.TestCase):
    def test_private_input_rejects_duplicates_unknown_fields_and_unsafe_files(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'input.json'
            good = {key: {} for key in FIELDS}
            path.write_text(json.dumps(good))
            path.chmod(0o600)
            self.assertEqual(read_configuration(path), good)
            for raw in ['{"trace": {}, "trace": {}}', json.dumps({**good, 'extra': 1}), '{"trace": NaN}']:
                path.write_text(raw)
                with self.assertRaises(ValueError):
                    read_configuration(path)
            path.write_text(json.dumps(good))
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                read_configuration(path)
            link = Path(directory) / 'link'
            link.symlink_to(path)
            with self.assertRaises(OSError):
                read_configuration(link)

    def test_all_mounted_certificates_must_cover_complete_campaign(self):
        pem = b'-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n'
        encoded = base64.b64encode(pem).decode()
        runtime = Mock()
        runtime.deployment.side_effect = lambda role: {'spec': {'template': {'spec': {'volumes': [
            {'name': 'tls', 'secret': {'secretName': role}}]}}}}
        runtime.get.side_effect = lambda _, role: {'data': dict.fromkeys(
            ['tls.crt', 'control-ca.crt'] if role == 'node' else ['tls.crt', 'node-ca.crt', 'node-client.crt'], encoded)}
        with patch('preflight.command', return_value=b'Certificate will not expire') as check:
            receipts = certificate_lifetimes(runtime, load_profile())
        self.assertEqual(len(receipts), 5)
        self.assertEqual({r['remainingSecondsAtLeast'] for r in receipts}, {17700})
        self.assertEqual(check.call_count, 5)
        with patch('preflight.command', side_effect=RuntimeError('certificate expires')):
            with self.assertRaises(RuntimeError):
                certificate_lifetimes(runtime, load_profile())


if __name__ == '__main__':
    unittest.main()
