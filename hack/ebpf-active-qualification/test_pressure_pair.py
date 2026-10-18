import json
from pathlib import Path
import tempfile
import unittest

from pressure_evaluate import evaluate_pressure_pair
from pressure_profile import load_pressure_profile
from pressure_test_fixture import make_pair
from scheduler_test_fixture import BOOT
from provenance import profile_stream_names, write_envelope


class PressurePairTests(unittest.TestCase):
    def test_full_paired_replay_and_resource_failure_remain_distinct(self):
        with tempfile.TemporaryDirectory() as path:
            root = Path(path)
            profile, cfg = make_pair(root)
            result = evaluate_pressure_pair(root / 'control', root / 'enabled', profile)
            self.assertTrue(result['measuredPressureBudgetsPassed'])
            self.assertEqual(len(result['sessions']), 20)
            self.assertGreaterEqual(result['workload']['commonIntervals'], 890)
            self.assertNotIn('measuredNormalBudgetsPassed', result)
            self.assertTrue(result['unavailableMeasurements'])
            resources = root / 'enabled/resources.jsonl'
            rows = [json.loads(line) for line in resources.read_text().splitlines()]
            for row in rows:
                row['groups']['node']['containment']['memoryPeakBytes']['value'] = 513 << 20
            resources.write_text(''.join(json.dumps(row) + '\n' for row in rows))
            with self.assertRaisesRegex(ValueError, 'stream changed'):
                evaluate_pressure_pair(root / 'control', root / 'enabled', profile)
            (root / 'enabled/envelope.json').unlink()
            write_envelope(root / 'enabled', 'enabled', 1, profile, cfg, BOOT)
            result = evaluate_pressure_pair(root / 'control', root / 'enabled', profile)
            self.assertFalse(result['measuredPressureBudgetsPassed'])
            self.assertTrue(all(row['sessionChecksPassed'] for row in result['sessions']))

    def test_continuous_streams_have_their_own_explicit_inventory(self):
        profile = load_pressure_profile()
        self.assertEqual(profile_stream_names('control', profile),
                         ['resources.jsonl', 'standard.jsonl', 'pressure.jsonl', 'scheduler.jsonl', 'scheduler-input.private.json',
                          'verifier.jsonl', 'verifier-input.private.json', 'verifier-probes.private.json'])
        self.assertEqual(len(profile_stream_names('enabled', profile)), 29)
        self.assertEqual(profile['windowSeconds'], 900)
        self.assertEqual(profile['pairs'], 5)


if __name__ == '__main__':
    unittest.main()
