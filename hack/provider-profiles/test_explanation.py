"""Keep the provider gate aligned with the current explanation output contract."""
import copy
import json
from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
FILTER = Path(__file__).with_name('validate_explanation.jq')


class ExplanationContractTests(unittest.TestCase):
    def setUp(self):
        source = (ROOT / 'internal/api/types.go').read_text()
        version = re.search(r'^const CurrentExplanationSchemaVersion = (\d+)$', source, re.M)
        self.assertIsNotNone(version)
        self.document = {
            'schemaVersion': int(version[1]),
            'finding': {
                'severity': 'info', 'confidence': 'medium',
                'caveats': ['Point-in-time evidence does not predict future use.'],
                'evidenceWindow': {'counterDeltaKnown': False},
            },
        }

    def accepts(self, document):
        result = subprocess.run(['jq', '-e', '-f', str(FILTER)],
                                input=json.dumps(document), text=True,
                                capture_output=True, timeout=5)
        return result.returncode == 0

    def test_current_contract_is_accepted_without_a_counter_delta(self):
        self.assertTrue(self.accepts(self.document))

    def test_old_unknown_and_missing_versions_are_rejected(self):
        for version in (1, 2, 999, None, '3'):
            with self.subTest(version=version):
                document = copy.deepcopy(self.document)
                document['schemaVersion'] = version
                self.assertFalse(self.accepts(document))
        del self.document['schemaVersion']
        self.assertFalse(self.accepts(self.document))

    def test_required_diagnosis_metadata_is_still_required(self):
        for key in ('severity', 'confidence', 'caveats', 'evidenceWindow'):
            with self.subTest(key=key):
                document = copy.deepcopy(self.document)
                del document['finding'][key]
                self.assertFalse(self.accepts(document))
        for key, value in (('severity', ''), ('confidence', ''), ('caveats', []),
                           ('evidenceWindow', [])):
            with self.subTest(key=key, value=value):
                document = copy.deepcopy(self.document)
                document['finding'][key] = value
                self.assertFalse(self.accepts(document))


if __name__ == '__main__':
    unittest.main()
