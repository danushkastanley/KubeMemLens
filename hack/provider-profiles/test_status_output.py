"""Exercise public status projection without weakening the privacy validator."""
import copy
import json
from pathlib import Path
import subprocess
import unittest

from privacy_contract import reject_sensitive_content

ROOT = Path(__file__).resolve().parent


class StatusOutputTests(unittest.TestCase):
    def setUp(self):
        self.status = {
            'evidence': {'queries': [{'query': 'node-context', 'status': 'disabled'}]},
            'connection': {'mode': 'kubernetes-api', 'collector': 'private-target',
                           'description': 'private description', 'healthy': True},
            'store': {'nodeRecords': 2}, 'metrics': {'available': True},
            'data': {'status': 'ready'},
        }

    def sanitise(self, value):
        return json.loads(subprocess.check_output(
            ['jq', '-f', str(ROOT / 'sanitise_status.jq')], input=json.dumps(value).encode()))

    def test_query_diagnostics_are_omitted_and_aggregate_status_is_preserved(self):
        with self.assertRaisesRegex(ValueError, 'identifier-bearing token'):
            reject_sensitive_content(self.status, ValueError)
        result = self.sanitise(self.status)
        self.assertNotIn('evidence', result)
        self.assertEqual(result['connection']['collector'], 'redacted')
        self.assertEqual(result['connection']['description'], 'redacted')
        for field in ('store', 'metrics', 'data'):
            self.assertEqual(result[field], self.status[field])
        reject_sensitive_content(result, ValueError)

    def test_unhealthy_and_error_states_are_not_hidden(self):
        self.status['connection']['healthy'] = False
        self.status['data']['status'] = 'unavailable'
        self.status['error'] = 'failed'
        result = self.sanitise(self.status)
        self.assertFalse(result['connection']['healthy'])
        self.assertEqual(result['data']['status'], 'unavailable')
        self.assertEqual(result['error'], 'failed')
        with self.assertRaisesRegex(ValueError, 'forbidden key: error'):
            reject_sensitive_content(result, ValueError)

    def test_sensitive_content_outside_query_diagnostics_is_still_rejected(self):
        for field, value in [('token', 'private'), ('detail', 'https://private.invalid')]:
            status = copy.deepcopy(self.status)
            status['store'][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                reject_sensitive_content(self.sanitise(status), ValueError)


if __name__ == '__main__':
    unittest.main()
