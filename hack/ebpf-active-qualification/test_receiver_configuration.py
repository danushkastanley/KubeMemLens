import base64
import copy
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from profile import load_profile
from receiver_configuration import receiver_configuration, target_ordinals
from window import Window


ENGINE = 'a' * 64


class ReceiverConfigurationTests(unittest.TestCase):
    def setUp(self):
        self.case = SimpleNamespace(node='kind-owned-worker', namespaces=('kml-active-a', 'kml-active-b'),
                                    runtime=SimpleNamespace(observer_network_scope=lambda: 'local', observer_server=lambda: 'https://kind-owned-control-plane:6443',
                                        cfg={'engineSHA256': ENGINE,
                                        'programmeIndexSHA256': 'b' * 64, 'nodeUID': 'owned-node'}))
        self.targets = [{'podName': f'target-owned-child-{i}', 'podUID': f'pod-{i}', 'container': str(i + 1) * 64,
                         'startedAt': f'2026-10-01T01:00:0{i}Z',
                         'group': {'inode': inode, 'path': f'/sys/fs/cgroup/owned-{i}'}}
                        for i, inode in enumerate((200, 100))]
        self.clients = [SimpleNamespace(namespace=ns, token=f'private-{i}', ca='fixture-ca')
                        for i, ns in enumerate(self.case.namespaces)]
        self.admissions = Mock()
        self.admissions.client.side_effect = lambda index: self.clients[index]
        self.admissions.pending = {0: '/first', 1: '/second'}
        self.responses = [{'metadata': {'name': name}, 'engineDigest': 'sha256:' + ENGINE}
                          for name in ('first', 'second')]
        self.trace = load_profile()['trace']

    def config(self, index):
        return receiver_configuration(self.case, self.admissions, self.targets, index,
                                      self.responses[index], self.trace, 'boot')

    def test_explicit_runtime_network_scope_reaches_receiver(self):
        self.assertEqual(self.config(0)['networkScope'], 'local')
        self.case.runtime.observer_network_scope = lambda: 'eks'
        self.case.runtime.observer_server = lambda: 'https://owned.us-east-1.eks.amazonaws.com'
        cfg = self.config(0)
        self.assertEqual(cfg['networkScope'], 'eks')
        self.assertEqual(cfg['server'], self.case.runtime.observer_server())

    def test_standard_observer_uses_same_explicit_scope_and_verified_ca(self):
        self.case.runtime.api_cluster = lambda: {'certificate-authority-data': base64.b64encode(b'bound-ca').decode()}
        self.case.runtime.exec = lambda args: b'boot-id\n'
        self.case.cfg = {'standardNamespace': 'owned-standard'}
        self.case.kube = lambda args: b'fixture-token\n'
        window = Window(self.case, None, load_profile(), None, 'control', 1)
        for scope in ('local', 'eks'):
            self.case.runtime.observer_network_scope = lambda: scope
            cfg = window.standard_config({})
            self.assertEqual(cfg['networkScope'], scope)
            self.assertEqual(cfg['caPEM'], 'bound-ca')
            self.assertEqual(cfg['token'], 'fixture-token')

    def test_each_receiver_uses_its_own_principal_session_and_target(self):
        for index in (0, 1):
            result = self.config(index)
            self.assertEqual(result['token'], f'private-{index}')
            self.assertEqual(result['sessionID'], self.responses[index]['metadata']['name'])
            self.assertEqual(result['target'], {
                'Namespace': self.case.namespaces[index], 'PodName': f'target-owned-child-{index}', 'PodUID': f'pod-{index}',
                'ContainerName': 'worker', 'ContainerID': self.targets[index]['container'],
                'ContainerStartedAt': self.targets[index]['startedAt'], 'NodeUID': 'owned-node',
                'CgroupID': self.targets[index]['group']['inode']})
            self.assertEqual(result['server'], 'https://kind-owned-control-plane:6443')
            self.assertEqual(result['programmeDigest'], 'sha256:' + 'b' * 64)
            for key in ('durationSeconds', 'maxEvents', 'maxOutputBytes', 'maxMapBytes', 'maxPathBytes'):
                self.assertEqual(result[key], self.trace[key])

    def test_current_single_receiver_path_retains_default_first_target(self):
        window = Window(self.case, None, load_profile(), None, 'enabled', 1)
        window.targets, window.admissions, window.boot = self.targets, self.admissions, 'boot'
        self.assertEqual(window.receiver_config(self.responses[0]), self.config(0))
        self.assertEqual(window.receiver_config(self.responses[1], 1), self.config(1))

    def test_crossed_namespace_session_or_candidate_fails_before_stream_start(self):
        self.clients[1].namespace = self.case.namespaces[0]
        with self.assertRaises(ValueError):
            self.config(1)
        self.clients[1].namespace = self.case.namespaces[1]
        self.admissions.pending[1] = '/first'
        with self.assertRaises(ValueError):
            self.config(1)
        self.admissions.pending[1] = '/second'
        self.responses[1]['engineDigest'] = 'wrong'
        with self.assertRaises(ValueError):
            self.config(1)

    def test_probe_or_invalid_index_cannot_become_a_measured_receiver(self):
        for index in (2, -1, True, 0.0):
            with self.subTest(index=index), self.assertRaises(ValueError):
                receiver_configuration(self.case, self.admissions, self.targets, index,
                                       self.responses[0], self.trace, 'boot')
        self.admissions.client.assert_not_called()

    def test_witness_ordinals_follow_inode_order_not_namespace_order(self):
        self.assertEqual(target_ordinals(self.targets), [1, 0])
        self.assertEqual(target_ordinals(list(reversed(self.targets))), [0, 1])

    def test_reused_pod_container_or_cgroup_cannot_claim_two_targets(self):
        for field in ('podUID', 'container', 'inode', 'path'):
            targets = copy.deepcopy(self.targets)
            parent = 'group' if field in ('inode', 'path') else None
            if parent:
                targets[1][parent][field] = targets[0][parent][field]
            else:
                targets[1][field] = targets[0][field]
            with self.subTest(field=field), self.assertRaises(ValueError):
                target_ordinals(targets)
        for value in (0, True, -1):
            targets = copy.deepcopy(self.targets)
            targets[0]['group']['inode'] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                target_ordinals(targets)
        with self.assertRaises(ValueError):
            target_ordinals(self.targets[:1])


if __name__ == '__main__':
    unittest.main()
