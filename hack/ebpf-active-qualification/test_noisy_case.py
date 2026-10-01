import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

import campaign
from fixture_roster import fixture_roster, noise_targets
from fixtures import Fixtures
from high_rate_profile import load_high_rate_profile
from noisy_profile import CASE, EXPECTED, load_noisy_profile
from nonselected_workload import ROLES
from profile import load_profile
from provenance import profile_stream_names, write_envelope, verify_envelope
from workload_case import workload_arguments


class NoisyCaseTests(unittest.TestCase):
    def test_roster_preserves_density_with_five_peers_in_each_namespace(self):
        profile = load_noisy_profile()
        namespaces = ('kml-active-a', 'kml-active-b')
        roster = fixture_roster(profile, namespaces)
        peers = noise_targets(profile, namespaces)
        self.assertEqual(len(roster), 32)
        self.assertEqual(len({(r['namespace'], r['name']) for r in roster}), 32)
        self.assertEqual({p['role'] for p in peers}, set(ROLES))
        self.assertEqual([sum(p['namespace'] == ns for p in peers) for ns in namespaces], [5, 5])
        self.assertTrue(all(p['name'] != 'target' for p in peers))
        self.assertEqual(workload_arguments(profile), ['series', 'cached', '1320', '1000'])
        self.assertEqual(profile['trace'], load_profile()['trace'])
        for plain in (load_profile(), load_high_rate_profile()):
            self.assertEqual(noise_targets(plain, namespaces), [])
            original = fixture_roster(plain, namespaces)
            self.assertEqual([r['name'] for r in original[2:]], [f'passive-{i:02}' for i in range(30)])
            self.assertTrue(all(r['namespace'] == namespaces[0] for r in original[2:]))

    def test_frozen_counts_split_and_schedule_cannot_be_reduced(self):
        for field, value in (('pairs', 1), ('workloadContainers', 31), ('minimumActiveSeconds', 899),
                             ('noise', {**EXPECTED['noise'], 'count': 9}),
                             ('noise', {**EXPECTED['noise'], 'sameNamespace': 10, 'otherNamespace': 0}),
                             ('schemaVersion', True)):
            profile = copy.deepcopy(EXPECTED)
            profile[field] = value
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'profile.json'
                path.write_text(json.dumps(profile))
                with self.subTest(field=field), self.assertRaises(ValueError):
                    load_noisy_profile(path)

    def test_every_noise_stream_is_bound_and_mutation_is_rejected(self):
        profile = load_noisy_profile()
        expected = {'resources.jsonl', 'standard.jsonl', 'workload.jsonl', 'scheduler.jsonl', 'scheduler-input.private.json',
                    'verifier.jsonl', 'verifier-input.private.json', 'verifier-probes.private.json'} | {r + '.jsonl' for r in ROLES}
        self.assertEqual(set(profile_stream_names('control', profile)), expected)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in expected | {'window.private.json'}:
                (root / name).write_text('{}\n')
            write_envelope(root, 'control', 1, profile, {'sourceSHA256': 'a' * 64}, 'boot')
            verify_envelope(root, 'control', profile)
            (root / 'nonselected-9.jsonl').write_text('{"changed":true}\n')
            with self.assertRaises(ValueError):
                verify_envelope(root, 'control', profile)

    def test_binding_recheck_keeps_the_peer_role(self):
        case = Mock()
        case.cfg = {'owner': 'owned'}
        fixture = Fixtures(case, Mock())
        bound = {'group': {'role': 'nonselected-3'}, 'identity': 'same'}
        fixture.bindings = {'kml-active-a/noise-3': bound}
        case.fixture.return_value = bound
        fixture.verify()
        case.fixture.assert_called_once_with('kml-active-a', 'noise-3', 'nonselected-3')
        case.fixture.return_value = {**bound, 'identity': 'replaced'}
        with self.assertRaises(ValueError):
            fixture.verify()

    def test_explicit_noisy_acknowledgement_loads_only_its_frozen_profile(self):
        args = ['campaign.py', '--config', 'private', '--output', 'new', '--acknowledge-local-noisy-campaign']
        with patch('sys.argv', args), patch('campaign.read_configuration', return_value={}), \
                patch('campaign.Campaign') as runner, patch('campaign.signal.signal'), patch('campaign.os.umask'):
            campaign.main()
            self.assertEqual(runner.call_args.args[2]['case'], CASE)
            self.assertEqual(runner.call_args.args[2], EXPECTED)


if __name__ == '__main__':
    unittest.main()
