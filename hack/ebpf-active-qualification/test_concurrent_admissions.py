import base64
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from concurrent_admissions import ConcurrentAdmissions, probe_resources
from local_case import TraceClient


ENGINE = 'a' * 64
DENIED = {'apiVersion': 'v1', 'kind': 'Status', 'status': 'Failure',
          'reason': 'TooManyRequests', 'code': 429}


def active(index):
    return {'metadata': {'name': f'owned-{index}'}, 'engineDigest': 'sha256:' + ENGINE, 'state': 'active'}


class ConcurrentAdmissionTests(unittest.TestCase):
    def manager(self):
        clients = [Mock() for _ in range(3)]
        runtime = SimpleNamespace(cfg={'engineSHA256': ENGINE})
        with patch('admissions.TraceClient', side_effect=clients[:2]), \
                patch('concurrent_admissions.TraceClient', return_value=clients[2]) as probe:
            manager = ConcurrentAdmissions(runtime, ['kml-active-a', 'kml-active-b'])
            probe.assert_called_once_with(runtime, 'kml-active-a', service_account='limit-probe')
        for index in (0, 1):
            clients[index].call.return_value = (201, active(index))
            manager.create(index, {'pod': 'target'})
            clients[index].call.return_value = (200, active(index))
        clients[2].call.return_value = (429, dict(DENIED))
        return manager, clients

    def test_third_principal_denial_rechecks_both_active_lifetimes(self):
        manager, clients = self.manager()
        result = manager.probe_capacity({'pod': 'target'})
        self.assertTrue(result['thirdRequestCapacityDenied'])
        self.assertIn('attachment evidence required', result['scope'])
        clients[2].call.assert_called_once_with('POST', value={'pod': 'target'})
        for index in (0, 1):
            self.assertEqual(clients[index].call.call_count, 3)
            clients[index].call.assert_called_with('GET', f'/owned-{index}')
        self.assertEqual(set(manager.pending), {0, 1})

    def test_unexpected_acceptance_is_retained_and_cancelled_with_its_own_client(self):
        manager, clients = self.manager()
        clients[2].call.return_value = (201, active(2))
        with self.assertRaisesRegex(ValueError, 'unexpectedly accepted'):
            manager.probe_capacity({})
        self.assertEqual(set(manager.pending), {0, 1, 2})
        for client in clients:
            client.call.return_value = (200, {})
        manager.cancel_all()
        for index, client in enumerate(clients):
            client.call.assert_called_with('DELETE', f'/owned-{index}')
        self.assertEqual(manager.pending, {})

    def test_wrong_candidate_acceptance_still_retains_the_probe_identity(self):
        manager, clients = self.manager()
        clients[2].call.return_value = (201, {**active(2), 'engineDigest': 'wrong'})
        with self.assertRaisesRegex(ValueError, 'did not match candidate'):
            manager.probe_capacity({})
        self.assertEqual(manager.pending[2], '/owned-2')

    def test_failed_probe_cancellation_does_not_lose_any_unconfirmed_identity(self):
        manager, clients = self.manager()
        clients[2].call.return_value = (201, active(2))
        with self.assertRaises(ValueError):
            manager.probe_capacity({})
        clients[0].call.return_value = (500, {})
        clients[1].call.return_value = (200, {})
        clients[2].call.side_effect = TimeoutError('unconfirmed')
        with self.assertRaisesRegex(ValueError, 'cleanup incomplete'):
            manager.cancel_all()
        self.assertEqual(manager.pending, {0: '/owned-0', 2: '/owned-2'})
        clients[1].call.assert_called_with('DELETE', '/owned-1')
        clients[2].call.assert_called_with('DELETE', '/owned-2')

    def test_missing_identity_or_uncertain_transport_requires_outer_teardown(self):
        for response in (None, {'metadata': None}, {'metadata': {'name': '../foreign'}}):
            manager, clients = self.manager()
            clients[2].call.return_value = (201, response)
            with self.subTest(response=response), self.assertRaises(ValueError):
                manager.probe_capacity({})
            self.assertEqual(set(manager.pending), {0, 1})
        manager, clients = self.manager()
        clients[2].call.side_effect = TimeoutError('response unavailable')
        with self.assertRaises(TimeoutError):
            manager.probe_capacity({})
        self.assertEqual(set(manager.pending), {0, 1})

    def test_other_denials_and_malformed_capacity_responses_do_not_pass(self):
        for response in ((403, DENIED), (503, DENIED), (429, None),
                         (429, {**DENIED, 'code': True}), (429, {**DENIED, 'reason': 'Forbidden'})):
            manager, clients = self.manager()
            clients[2].call.return_value = response
            with self.subTest(response=response), self.assertRaises(ValueError):
                manager.probe_capacity({})

    def test_changed_or_expired_active_session_invalidates_probe(self):
        for moment in ('before', 'after'):
            manager, clients = self.manager()
            stale = (200, {**active(1), 'state': 'expired'})
            clients[1].call.side_effect = [stale] if moment == 'before' else [(200, active(1)), stale]
            with self.subTest(moment=moment), self.assertRaisesRegex(ValueError, 'must remain active'):
                manager.probe_capacity({})
            self.assertEqual(clients[2].call.call_count, 0 if moment == 'before' else 1)

    def test_probe_grant_reuses_only_the_existing_namespaced_fixture_role(self):
        account, binding = probe_resources('kml-active-a', 'owned')
        self.assertFalse(account['automountServiceAccountToken'])
        self.assertEqual(binding['roleRef'], {'apiGroup': 'rbac.authorization.k8s.io',
                                             'kind': 'Role', 'name': 'trace-fixture'})
        self.assertEqual(binding['subjects'], [{'kind': 'ServiceAccount', 'name': 'limit-probe',
                                                'namespace': 'kml-active-a'}])
        self.assertNotIn('rules', binding)

    def test_unknown_principals_fail_before_any_runtime_operation(self):
        runtime = Mock()
        with self.assertRaisesRegex(ValueError, 'service account'):
            TraceClient(runtime, 'kml-active-a', service_account='admin')
        runtime.json.assert_not_called()
        runtime.api_cluster.assert_not_called()
        with patch('admissions.TraceClient') as client, self.assertRaises(ValueError):
            ConcurrentAdmissions(runtime, ['kml-active-a'])
        client.assert_not_called()

    def test_tokens_are_requested_for_the_explicit_principal_only(self):
        runtime = Mock()
        runtime.kube = ['kubectl', '--context', 'kind-owned']
        runtime.json.return_value = {'clusters': [{'cluster': {
            'server': 'https://127.0.0.1:1234',
            'certificate-authority-data': base64.b64encode(b'fixture-ca').decode()}}]}
        runtime.api_cluster.return_value = runtime.json.return_value['clusters'][0]['cluster']
        for account in ('tenant', 'limit-probe'):
            with patch('local_case.ssl.create_default_context'), \
                    patch('local_case.command', return_value=b'private-token\n') as command:
                client = TraceClient(runtime, 'kml-active-a', service_account=account)
                command.assert_called_once_with(runtime.kube + [
                    '-n', 'kml-active-a', 'create', 'token', account, '--duration=1h'])
                self.assertNotIn('private-token', repr(client))


if __name__ == '__main__':
    unittest.main()
