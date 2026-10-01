import base64
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from profile import load_profile
from window import Window
from local_runtime import Runtime
from eks_host_runtime import EKSHostRuntime
from campaign import sources


class NetworkConfigurationTests(unittest.TestCase):
    def window(self, provider):
        runtime = (EKSHostRuntime if provider else Runtime).__new__(EKSHostRuntime if provider else Runtime)
        runtime.cfg = {'context': 'kind-owned', 'engineSHA256': 'a' * 64,
                       'programmeIndexSHA256': 'b' * 64, 'nodeUID': 'owned-node'}
        if provider:
            runtime.provider = {'clusterEndpoint': 'https://owned.us-east-1.eks.amazonaws.com'}
        runtime.api_cluster = Mock(return_value={'certificate-authority-data': base64.b64encode(b'verified-ca').decode()})
        runtime.exec = Mock(return_value=b'owned-boot\n')
        case = SimpleNamespace(runtime=runtime, cfg={'standardNamespace': 'owned-standard'},
                               kube=Mock(return_value=b'observer-token\n'), namespaces=['owned-workload'])
        window = Window(case, None, load_profile(), None, 'control', 1)
        window.client = SimpleNamespace(token='delivery-token', ca='verified-ca')
        window.boot = 'owned-boot'
        window.targets = [{'podUID': 'owned-pod', 'container': 'c' * 64,
                           'startedAt': '2026-10-01T01:00:00Z', 'group': {'inode': 100}}]
        return window

    def test_local_and_explicit_provider_routes_reach_both_native_configs(self):
        for provider, scope, server in [(False, 'local', 'https://owned-control-plane:6443'),
                                       (True, 'eks', 'https://owned.us-east-1.eks.amazonaws.com')]:
            with self.subTest(scope=scope):
                window = self.window(provider)
                standard = window.standard_config({})
                window.case.runtime.api_cluster.assert_called_once_with()
                delivery = window.receiver_config({'metadata': {'name': 'owned-session'}})
                for cfg in (standard, delivery):
                    self.assertEqual(cfg['networkScope'], scope)
                    self.assertEqual(cfg['server'], server)
                    self.assertEqual(cfg['caPEM'], 'verified-ca')
                self.assertEqual(standard['token'], 'observer-token')
                self.assertEqual(delivery['token'], 'delivery-token')

    def test_changed_cluster_binding_stops_standard_configuration(self):
        window = self.window(True)
        window.case.runtime.api_cluster.side_effect = ValueError('binding changed')
        with self.assertRaisesRegex(ValueError, 'binding changed'):
            window.standard_config({})
        window.case.kube.assert_not_called()

    def test_shared_validator_is_included_in_frozen_source(self):
        manifest = sources()
        self.assertIn('internal/qualificationendpoint/endpoint.go', manifest)
        self.assertIn('internal/qualificationendpoint/endpoint_test.go', manifest)


if __name__ == '__main__':
    unittest.main()
