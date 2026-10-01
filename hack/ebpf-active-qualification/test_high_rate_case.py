import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

import campaign
from high_rate_profile import EXPECTED, load_high_rate_profile
from profile import load_profile, trace_slots
from workload_case import compare_case, workload_arguments
from test_activity import witness
from test_mixed_series import records, encode


class HighRateCaseTests(unittest.TestCase):
    def test_frozen_schedule_has_full_windows_and_unchanged_admission_bounds(self):
        normal, mixed = load_profile(), load_high_rate_profile()
        self.assertEqual(mixed['pairs'], 5)
        self.assertEqual(mixed['trace'], normal['trace'])
        self.assertEqual(mixed['windowSeconds'], 1350)
        self.assertEqual(mixed['minimumActiveOperationsPerMode'] * 3, 9000)
        self.assertEqual(mixed['workload']['count'], 13200)
        self.assertLess(5 + 13200 * 100 / 1000, mixed['windowSeconds'])
        self.assertLess(trace_slots(mixed)[-1] + mixed['trace']['durationSeconds'], mixed['windowSeconds'])
        self.assertEqual(workload_arguments(normal), ['series', 'cached', '1320', '1000'])
        self.assertEqual(workload_arguments(mixed), ['mixed-series', '13200', '100'])

    def test_shortened_changed_cycle_or_type_cannot_load(self):
        for field, value in (('pairs', 1), ('minimumActiveOperationsPerMode', 2999),
                             ('windowSeconds', 900), ('schemaVersion', True),
                             ('workload', {'mode': 'cached', 'count': 13200, 'periodMilliseconds': 100, 'fileBytes': 8388608})):
            profile = copy.deepcopy(EXPECTED)
            profile[field] = value
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'profile.json'
                path.write_text(json.dumps(profile))
                with self.subTest(field=field), self.assertRaises(ValueError):
                    load_high_rate_profile(path)

    def test_case_replay_reaches_real_mixed_validator(self):
        # Small offline evidence verifies dispatch; the live profile loader above
        # rejects this shortened synthetic schedule.
        profile = load_high_rate_profile()
        profile.update(windowSeconds=13, minimumActiveOperationsPerMode=2)
        profile['workload'].update(count=6, periodMilliseconds=1000)
        raw = encode(records(6, 1000))
        activity = witness(13)
        for row in activity:
            row['snapshot']['objects'] = {kind: list(range(1, count + 1))
                                           for kind, count in profile['trace']['objects'].items()}
        result, passed = compare_case(raw, raw, profile, activity)
        self.assertTrue(passed)
        self.assertEqual(set(result['byMode']), {'cached', 'uncached', 'write'})
        with self.assertRaises(ValueError):
            compare_case(raw, raw.replace('"mode": "uncached"', '"mode": "cached"'), profile, activity)
        profile['case'] = 'undeclared'
        with self.assertRaises(ValueError):
            compare_case(raw, raw, profile, activity)

    def test_cli_requires_exactly_one_explicit_case_acknowledgement(self):
        base = ['campaign.py', '--config', 'private.json', '--output', 'new-output']
        for flag, case in (('--acknowledge-local-normal-campaign', 'normal-confirmed-files'),
                           ('--acknowledge-local-high-rate-campaign', 'high-rate-mixed-files')):
            with patch('sys.argv', base + [flag]), patch('campaign.read_configuration', return_value={}), \
                    patch('campaign.Campaign') as runner, patch('campaign.signal.signal'), patch('campaign.os.umask'):
                campaign.main()
                self.assertEqual(runner.call_args.args[2]['case'], case)
                runner.return_value.run.assert_called_once_with()
        for flags in ([], ['--acknowledge-local-normal-campaign', '--acknowledge-local-high-rate-campaign']):
            with patch('sys.argv', base + flags), patch('sys.stderr', Mock()), patch('campaign.read_configuration') as read:
                with self.assertRaises(SystemExit) as error:
                    campaign.main()
                self.assertEqual(error.exception.code, 2)
                read.assert_not_called()


if __name__ == '__main__':
    unittest.main()
