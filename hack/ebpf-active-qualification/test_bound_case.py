from copy import deepcopy
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from bound_case import BoundCase
from local_case import LocalCase


def configuration():
    return {'fixtureNamespaces': ['kml-active-a', 'kml-active-b'], 'standardNamespace': 'kml-active-standard',
            'release': 'kml-active-release', 'owner': 'owned-test',
            'standardImage': 'standard@sha256:' + 'a' * 64, 'fixtureImage': 'fixture@sha256:' + 'b' * 64,
            'fixtureSHA256': 'c' * 64, 'standardSHA256': {'agent': 'd' * 64, 'collector': 'e' * 64},
            'helpers': {role: {'path': '/usr/local/bin/kml-' + role, 'sha256': 'f' * 64}
                        for role in ('measure', 'watch', 'delivery', 'standard', 'scheduler', 'verifier')}}


class BoundCaseTests(unittest.TestCase):
    def test_common_ownership_contract_accepts_verified_runtime_node_identity(self):
        runtime = SimpleNamespace(cfg={'node': 'ip-10-0-0-1.ec2.internal'})
        case = BoundCase(configuration(), runtime)
        self.assertIs(case.runtime, runtime)
        self.assertEqual(case.node, runtime.cfg['node'])
        self.assertEqual(case.namespaces, ('kml-active-a', 'kml-active-b'))
        case.json = Mock()
        with self.assertRaisesRegex(ValueError, 'outside campaign'):
            case.namespace('foreign')
        case.json.assert_not_called()
        case.json.return_value = {'metadata': {'labels': {'kube-memlens.io/fixture': 'foreign'}}}
        with self.assertRaisesRegex(ValueError, 'ownership changed'):
            case.namespace(case.namespaces[0])

    def test_common_artifact_namespace_and_helper_guards_are_retained(self):
        mutations = [lambda c: c['helpers'].pop('verifier'), lambda c: c['helpers'].pop('scheduler'), lambda c: c.update(fixtureNamespaces=['kml-active-a', 'kml-active-a']),
                     lambda c: c.update(standardNamespace='kml-active-a'),
                     lambda c: c.update(standardImage='standard:latest'),
                     lambda c: c['standardSHA256'].update(agent='unknown'),
                     lambda c: c['helpers']['watch'].update(path='/tmp/unbound'),
                     lambda c: c.update(owner='invalid/owner')]
        for change in mutations:
            cfg = deepcopy(configuration()); change(cfg)
            with self.subTest(change=mutations.index(change)), self.assertRaises(ValueError):
                BoundCase(cfg, SimpleNamespace(cfg={'node': 'verified-worker'}))

    def test_local_entrypoint_still_refuses_cloud_before_running_commands(self):
        with patch('local_case.command') as command:
            with self.assertRaisesRegex(ValueError, 'local kind context'):
                LocalCase({**configuration(), 'trace': {'context': 'eks-owned'}})
            command.assert_not_called()


if __name__ == '__main__':
    unittest.main()
