import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import campaign
from concurrent_profile import CASE, load_concurrent_profile, maximum_node_arguments
from fixture_roster import fixture_roster
from profile import load_profile
from provenance import profile_stream_names, write_envelope, verify_envelope
from workload_case import workload_arguments, read_workloads


class ConcurrentCaseTests(unittest.TestCase):
    def test_frozen_schedule_and_per_trace_bounds_preserve_normal_limits(self):
        profile, normal = load_concurrent_profile(), load_profile()
        self.assertEqual(profile['pairs'], 5)
        self.assertEqual(profile['windowSeconds'], 1350)
        self.assertEqual(profile['minimumActiveSeconds'], 900)
        self.assertEqual(profile['selectedTargets'], 2)
        for key in normal['trace'].keys() - {'objects'}:
            self.assertEqual(profile['trace'][key], normal['trace'][key])
        self.assertEqual(workload_arguments(profile), ['series', 'cached', '1320', '1000'])
        for field, value in (('pairs', 1), ('selectedTargets', 1), ('windowSeconds', 899), ('schemaVersion', True)):
            with tempfile.TemporaryDirectory() as directory:
                changed = copy.deepcopy(profile)
                changed[field] = value
                path = Path(directory) / 'changed.json'
                path.write_text(json.dumps(changed))
                with self.subTest(field=field), self.assertRaises(ValueError):
                    load_concurrent_profile(path)

    def test_two_targets_have_separate_resource_roles_without_changing_density(self):
        roster = fixture_roster(load_concurrent_profile(), ['kml-active-a', 'kml-active-b'])
        self.assertEqual(len(roster), 32)
        self.assertEqual([x['role'] for x in roster[:2]], ['selected', 'selected-peer'])
        self.assertEqual([x['name'] for x in roster[:2]], ['target', 'target'])
        self.assertEqual(len({(x['namespace'], x['name']) for x in roster}), 32)

    def test_default_node_argument_forms_are_normalised_without_other_policy_changes(self):
        for before in (['serve'], ['serve', '--max-node-traces', '1'], ['serve', '--max-node-traces=1']):
            self.assertEqual(maximum_node_arguments(before), ['serve', '--max-node-traces=2'])
        for before in (['--max-node-traces'], ['--max-node-traces=2'], ['--max-node-traces=3'],
                       ['--max-node-traces=1', '--max-node-traces', '1']):
            with self.subTest(before=before), self.assertRaises(ValueError):
                maximum_node_arguments(before)

    def test_every_selected_and_delivery_stream_is_bound_and_replayed(self):
        profile = load_concurrent_profile()
        streams = profile_stream_names('enabled', profile)
        self.assertEqual(len(streams), 82)
        self.assertEqual(len(set(streams)), len(streams))
        self.assertNotIn('workload.jsonl', streams)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in streams + ['window.private.json']:
                (root / name).write_text('{}\n')
            write_envelope(root, 'enabled', 1, profile, {'sourceSHA256': 'a' * 64}, 'boot')
            verify_envelope(root, 'enabled', profile)
            self.assertEqual(set(read_workloads(root, profile)), {'target-0', 'target-1'})
            (root / 'delivery-35-target-1.jsonl').write_text('{"changed":true}\n')
            with self.assertRaises(ValueError):
                verify_envelope(root, 'enabled', profile)

    def test_explicit_concurrent_acknowledgement_selects_only_this_profile(self):
        args = ['campaign.py', '--config', 'private', '--output', 'new', '--acknowledge-local-concurrent-campaign']
        with patch('sys.argv', args), patch('campaign.read_configuration', return_value={}), \
                patch('campaign.Campaign') as runner, patch('campaign.signal.signal'), patch('campaign.os.umask'):
            campaign.main()
            self.assertEqual(runner.call_args.args[2]['case'], CASE)
            self.assertEqual(runner.call_args.args[2], load_concurrent_profile())


if __name__ == '__main__':
    unittest.main()
