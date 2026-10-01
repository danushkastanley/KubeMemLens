import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from profile import load_profile
from scheduler_test_fixture import BOOT, owner_binding
from test_verifier_capture import owner_for
from window import Window


class VerifierIntegrationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name) / 'window'
        self.case = Mock()
        self.case.cfg = {'standardNamespace': 'owned-standard'}
        self.case.namespaces = ['owned-a', 'owned-b']
        self.case.runtime.exec.return_value = (BOOT + '\n').encode()
        self.case.runtime.service.return_value = {'identity': 'owned-service',
                                                  'group': {'role': 'node', 'path': '/owned', 'inode': 1}}
        self.case.runtime.deployment.return_value = {'spec': {'replicas': 1}}
        self.case.runtime.api_cluster.return_value = {'certificate-authority-data': 'Y2E='}
        self.case.runtime.observer_server.return_value = 'https://owned-server'
        self.case.runtime.observer_network_scope.return_value = 'local'
        self.case.kube.return_value = b'private-test-token'
        standard = {role: owner_binding() for role in ('agent', 'collector')}
        self.case.standard.side_effect = standard.__getitem__
        self.owner = owner_for('enabled'); self.owner['flags'] = ['--owned']
        self.case.node_owner.return_value = self.owner
        self.case.snapshot.return_value = {'workers': 0, 'excludedWorkers': 0, 'activeControls': 0,
                                           'objects': {'map': [], 'prog': [], 'link': []}}
        self.case.remaining.return_value = {'remaining': {'map': [], 'prog': [], 'link': []}}
        self.fixtures = Mock()
        self.fixtures.mapping.return_value = []
        self.fixtures.bindings = {}
        self.fixtures.binding.side_effect = lambda ns, name, role: {
            'namespace': ns, 'podName': 'target', 'group': {'role': role, 'path': '/owned/' + ns, 'inode': 1},
            'identity': ns}
        self.processes = Mock()
        self.processes.configuration.return_value = '/private/config'
        self.lease = Mock()
        self.events = []
        self.processes.close.side_effect = lambda: self.events.append('processes')
        self.lease.close.side_effect = lambda: self.events.append('lease')

    def run_window(self, phase):
        bound = owner_for(phase)
        with patch('window.Processes', return_value=self.processes), \
             patch('window.prepare_scheduler', return_value='/private/scheduler'), \
             patch('window.prepare_verifier', return_value=('/private/verifier', self.lease, bound)) as prepare, \
             patch('window.write_envelope') as envelope, patch('window.Admissions'), \
             patch('window.target_ordinals', return_value=[0, 1]):
            window = Window(self.case, self.fixtures, load_profile(), self.directory, phase, 1)
            window.run_workloads = Mock()
            result = window.run()
            return result, prepare, envelope

    def test_enabled_node_and_control_collector_are_explicitly_scoped(self):
        result, prepare, envelope = self.run_window('enabled')
        self.assertEqual(prepare.call_args.args[-2:], (self.owner, 'enabled'))
        self.processes.scheduler.assert_called_once_with('/private/scheduler')
        self.processes.native.assert_any_call('verifier', 'verifier', '--config', '/private/verifier', '--acknowledge-owned-node')
        self.assertTrue(result['verifierCleanup'])
        self.assertEqual(result['traceNodeOwnerIdentity'], self.owner['identity'])
        self.assertEqual(self.events, ['processes', 'lease'])
        envelope.assert_called_once()
        self.directory = self.directory.parent / 'control'
        result, prepare, _ = self.run_window('control')
        self.assertEqual(prepare.call_args.args[-1], 'control')
        self.assertEqual(prepare.call_args.args[-2]['group']['role'], 'collector')
        self.assertIsNone(result['traceNodeOwnerIdentity'])

    def test_probe_cleanup_failure_retains_failed_receipt_without_envelope(self):
        self.lease.close.side_effect = ValueError('definition changed')
        with self.assertRaisesRegex(ValueError, 'cleanup unconfirmed'):
            self.run_window('enabled')
        receipt = json.loads((self.directory / 'window.private.json').read_text())
        self.assertFalse(receipt['verifierCleanup'])
        self.assertIn('owned verifier probes', receipt['cleanupFailures'])
        self.assertFalse((self.directory / 'envelope.json').exists())

    def test_failed_observation_still_closes_processes_before_probe_reconciliation(self):
        self.processes.wait_all.side_effect = ValueError('required observer failed')
        with self.assertRaisesRegex(ValueError, 'required observer failed'):
            self.run_window('enabled')
        receipt = json.loads((self.directory / 'window.private.json').read_text())
        self.assertFalse(receipt['completed'])
        self.assertTrue(receipt['verifierCleanup'])
        self.assertEqual(self.events, ['processes', 'lease'])
        self.assertFalse((self.directory / 'envelope.json').exists())


if __name__ == '__main__':
    unittest.main()
