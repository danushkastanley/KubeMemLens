from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from admissions import Admissions

ENGINE = 'a' * 64


class AdmissionCleanupTests(unittest.TestCase):
    def manager(self, count=2):
        clients = [Mock() for _ in range(count)]
        for index, client in enumerate(clients):
            client.call.return_value = (201, {'metadata': {'name': f'owned-{index}'}, 'engineDigest': 'sha256:' + ENGINE})
        with patch('admissions.TraceClient', side_effect=clients):
            manager = Admissions(SimpleNamespace(cfg={'engineSHA256': ENGINE}), [f'kml-active-{i}' for i in range(count)])
        return manager, clients

    def test_both_created_identities_are_retained_and_cancelled_with_their_own_clients(self):
        manager, clients = self.manager()
        for index in range(2):
            manager.create(index, {'pod': 'target'})
        for client in clients:
            client.call.return_value = (200, {})
        manager.cancel_all()
        for index, client in enumerate(clients):
            client.call.assert_called_with('DELETE', f'/owned-{index}')
        self.assertEqual(manager.pending, {})
        counts = [c.call.call_count for c in clients]
        manager.cancel_all()
        self.assertEqual([c.call.call_count for c in clients], counts)

    def test_one_cleanup_failure_does_not_prevent_cancelling_the_other(self):
        manager, clients = self.manager()
        for index in range(2):
            manager.create(index, {})
        clients[0].call.side_effect = RuntimeError('transport unavailable')
        clients[1].call.return_value = (410, {})
        with self.assertRaisesRegex(ValueError, 'cleanup incomplete'):
            manager.cancel_all()
        clients[1].call.assert_called_with('DELETE', '/owned-1')
        self.assertEqual(manager.pending, {0: '/owned-0'})

    def test_candidate_mismatch_still_retains_created_identity_for_cleanup(self):
        manager, clients = self.manager(1)
        clients[0].call.return_value = (201, {'metadata': {'name': 'created'}, 'engineDigest': 'wrong'})
        with self.assertRaisesRegex(ValueError, 'did not match candidate'):
            manager.create(0, {})
        self.assertEqual(manager.pending, {0: '/created'})
        clients[0].call.return_value = (404, {})
        manager.cancel_all()
        self.assertEqual(manager.pending, {})

    def test_failed_second_creation_preserves_the_first_session(self):
        manager, clients = self.manager()
        manager.create(0, {})
        clients[1].call.return_value = (429, {'error': 'limit'})
        with self.assertRaises(ValueError):
            manager.create(1, {})
        self.assertEqual(manager.pending, {0: '/owned-0'})
        clients[0].call.return_value = (200, {'state': 'active'})
        self.assertEqual(manager.get(0), (200, {'state': 'active'}))
        clients[0].call.assert_called_with('GET', '/owned-0')
        manager.cancel_all()
        self.assertEqual(manager.pending, {})

    def test_pending_identity_cannot_be_overwritten_and_failed_cancel_remains_pending(self):
        manager, clients = self.manager(1)
        manager.create(0, {})
        with self.assertRaisesRegex(ValueError, 'previous admission'):
            manager.create(0, {})
        self.assertEqual(clients[0].call.call_count, 1)
        clients[0].call.return_value = (500, {})
        with self.assertRaises(ValueError):
            manager.cancel(0)
        self.assertEqual(manager.pending, {0: '/owned-0'})

    def test_invalid_target_or_unbounded_created_identity_fails(self):
        manager, clients = self.manager(1)
        for index in (-1, 1, True):
            with self.subTest(index=index), self.assertRaises(ValueError):
                manager.create(index, {})
        clients[0].call.assert_not_called()
        for name in ('../foreign', '', 'x' * 129):
            clients[0].call.return_value = (201, {'metadata': {'name': name}, 'engineDigest': 'sha256:' + ENGINE})
            with self.subTest(name=name), self.assertRaises(ValueError):
                manager.create(0, {})
        clients[0].call.return_value = (201, {'metadata': None, 'engineDigest': 'sha256:' + ENGINE})
        with self.assertRaises(ValueError):
            manager.create(0, {})
        self.assertEqual(manager.pending, {})

    def test_invalid_namespace_inventory_is_rejected_before_token_creation(self):
        with patch('admissions.TraceClient') as client:
            for namespaces in ([], ['same', 'same'], ['a', 'b', 'c']):
                with self.subTest(namespaces=namespaces), self.assertRaises(ValueError):
                    Admissions(SimpleNamespace(cfg={'engineSHA256': ENGINE}), namespaces)
            client.assert_not_called()


if __name__ == '__main__':
    unittest.main()
