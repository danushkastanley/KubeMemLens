"""Verify actual child names reach RBAC, workload preparation and density bindings."""
import json
from unittest.mock import Mock, patch
import unittest

from fixtures import Fixtures
from bound_case import BoundCase
from fixture_roster import fixture_roster
from profile import load_profile
from high_rate_profile import load_high_rate_profile
from noisy_profile import load_noisy_profile
from concurrent_profile import load_concurrent_profile
from flood_profile import load_flood_profile
from pressure_profile import load_pressure_profile


class FixtureJobPipelineTests(unittest.TestCase):
    def test_all_profiles_bind_controller_child_names_before_tenant_access(self):
        for profile in (load_profile(), load_high_rate_profile(), load_noisy_profile(),
                        load_concurrent_profile(), load_flood_profile(), load_pressure_profile()):
            with self.subTest(case=profile['case']):
                case = Mock()
                case.namespaces = ('kml-active-a', 'kml-active-b')
                case.node, case.owner = 'owned-worker', 'owned-run'
                case.cfg = {'owner': case.owner, 'standardNamespace': 'kml-active-standard', 'release': 'owned',
                            'fixtureImage': 'fixture@sha256:' + 'a' * 64, 'standardImage': 'standard@sha256:' + 'b' * 64,
                            'chartInventory': {'path': '/approved/inventory', 'sha256': 'c' * 64}}
                case.runtime.cfg = {'kubeconfig': '/private/config', 'context': 'kind-owned'}
                case.standard_values.return_value = BoundCase.standard_values(case)
                case.json.return_value = {'rules': [{'apiGroups': ['memory.kubememlens.io'],
                                                    'resources': ['metrics'], 'verbs': ['get']}]}
                case.kube.side_effect = lambda args, *rest, **kw: (json.dumps({'writeBytes': 8 << 20}).encode()
                                                                 if 'exec' in args else b'')
                case.fixture.side_effect = lambda ns, name, role: {'group': {'path': '/owned/' + name, 'inode': 7, 'role': role}}
                fixture = Fixtures(case, Mock())
                fixture.namespace = Mock()
                fixture.verify = Mock()
                fixture.jobs = Mock()
                def pod_name(ns, name):
                    self.assertEqual(fixture.jobs.ready.call_count, 1)
                    return name + '-controller-child'
                fixture.jobs.pod_name.side_effect = pod_name
                created = []
                def create(obj):
                    created.append(obj)
                    return {'metadata': {'uid': 'owned-resource'}}
                fixture.create = create
                with patch('fixtures.command', side_effect=[b'rendered', b'installed']), patch('fixtures.inventory', return_value=[]) as inventory:
                    fixture.prepare(profile)
                inventory.assert_called_once_with(case.cfg['chartInventory'], b'rendered')
                roster = fixture_roster(profile, case.namespaces)
                self.assertEqual(fixture.jobs.add.call_count, profile['workloadContainers'])
                fixture.jobs.ready.assert_called_once()
                expected = {item['namespace'] + '/' + item['name'] + '-controller-child' for item in roster}
                self.assertEqual(set(fixture.bindings), expected)
                roles = [obj for obj in created if obj['kind'] == 'Role']
                self.assertEqual(len(roles), 2)
                self.assertTrue(all(obj['rules'][0]['resourceNames'] == ['target-controller-child'] for obj in roles))
                commands = [call.args[0] for call in case.kube.call_args_list if 'exec' in call.args[0]]
                self.assertTrue(commands)
                self.assertTrue(all(args[args.index('exec') + 1].endswith('-controller-child') for args in commands))
                self.assertTrue(all(call.args[1].endswith('-controller-child') for call in case.fixture.call_args_list))
                fixture.verify.assert_called_once()


if __name__ == '__main__':
    unittest.main()
