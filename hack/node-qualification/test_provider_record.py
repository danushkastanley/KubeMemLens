import copy
import json
import unittest
from types import SimpleNamespace
from unittest.mock import Mock

from common import ContractError, digest, load
from evaluate import evaluate
from provider_record import assemble, cleanup_cluster
from test_contract import ROOT, fixture


class ProviderRecordTest(unittest.TestCase):
    def setUp(self):
        self.profile, self.example = fixture(load(ROOT / 'profiles/gke-standard.json'))
        e = self.example
        e['environment'].update(nodeImage='COS_CONTAINERD', cni='GKE Dataplane V2', architecture='amd64', osImage='COS 125')
        self.receipt = {'schemaVersion': 2,
            'profile': {'id': 'gke-cos-containerd-amd64', 'digest': load(ROOT.parent / 'provider-profiles/gke-cos-containerd-amd64.json')['profileDigest']},
            'observedAt': '2026-09-11T12:00:00Z', 'provider': 'gke-standard', 'nodeImage': 'COS_CONTAINERD',
            'cniName': 'GKE Dataplane V2', 'controlPlaneVersion': '1.37.0', 'proofSource': 'gcloud:control-plane+node-pool',
            'providerChecks': {k: True for k in ('profileCanonical', 'providerMode', 'nodeImage', 'cni', 'controlPlaneVersion', 'contextBinding', 'nodePoolBinding')},
            'qualificationToolCommit': e['artefacts']['toolCommit']}
        self.receipt['receiptDigest'] = digest(self.receipt, 'receiptDigest')
        config = {k: e['artefacts'][k] for k in ('sourceCommit', 'imageDigest', 'chartDigest', 'cliDigest', 'producerDigest')}
        config['kubeletAudience'] = 'private-audience'
        plan = {'files': {'baseline-values.json': 'sha256:' + 'c' * 64}, 'observation': e['observation'],
                'toolSourceDirty': False, 'qualificationToolCommit': e['artefacts']['toolCommit'],
                'servingTrustDigest': e['artefacts']['trustDigest']}
        image = {'imageDigest': config['imageDigest'], 'architecture': 'amd64',
                 'binaries': {'memlens-node-context': config['producerDigest']}}
        self.proof = {'candidate': {k: config[k] for k in ('sourceCommit', 'imageDigest', 'chartDigest', 'cliDigest')},
                      'image': image, 'sourceTreeDigest': e['artefacts']['sourceTreeDigest']}
        measured = {k: e[k] for k in ('schemaVersion', 'nodes', 'observation')}
        binding = {'nodes': [{'name': 'private-node-0'}, {'name': 'private-node-1'}],
                   'runtime': {k: e['environment'][k] for k in ('kubernetes', 'kernel', 'runtime', 'osImage', 'architecture')}}
        observations = [{'nodeName': n['name'], 'stats': {'provenance': 'unknown', 'memory': {'usageBytes': 0}}, 'context': {}}
                        for n in binding['nodes']]
        self.execution = SimpleNamespace(bundle=SimpleNamespace(profile=self.profile, configuration=config, plan=plan),
            binding=binding, initial_binding=copy.deepcopy(binding), observations=observations, image_proof=image,
            replacement={'bindingDigest': 'sha256:' + 'd' * 64, 'providerReceiptDigest': self.receipt['receiptDigest'],
                         'liveImages': {'imageDigest': config['imageDigest'], 'architecture': 'amd64', 'checkedPods': 5, 'allMatched': True},
                         'sourceSummary': {'fields': {k: 'available' if k == 'usage' else 'unreported' for k in e['fields']},
                                           'provenance': 'unknown'}},
            image_checks={phase: {'imageDigest': config['imageDigest'], 'architecture': 'amd64', 'checkedPods': pods, 'allMatched': True}
                          for phase, pods in (('baseline', 3), ('enabled', 5))},
            measurements=Mock(return_value=measured), cleanup=Mock(),
            installer=SimpleNamespace(namespace='owned-namespace'), ownership=SimpleNamespace(owned={'owned-namespace': 'private-uid'}))
        check = {'allowedBefore': True, 'blocked': True, 'allowedAfter': True}
        self.network = {'method': 'controlled-ingress-producer-egress-v1', 'qualified': False, 'ownNodeIsolation': 'not-claimed',
                        'passed': True, 'freshSourceRetained': True,
                        'nodes': [{'slot': slot, 'ingress': dict(check), 'egress': dict(check)} for slot in (0, 1)]}

    def assemble(self):
        return assemble(self.execution, self.proof, self.receipt, self.example['startedAt'],
                        self.example['lifecycle'], self.network, 'completed', self.example['completedAt'])

    def test_record_retains_measured_data_and_never_self_confirms_cleanup(self):
        record = self.assemble()
        self.assertEqual(record['nodes'], self.example['nodes'])
        self.assertEqual(record['fields']['usage'], 'available')
        self.assertEqual(record['fields']['available'], 'unreported')
        self.assertEqual(record['provenance'], 'unknown')
        self.assertEqual(record['transport']['networkPolicy'], 'passed')
        self.assertEqual(record['cleanup'], {'workloadsRemoved': False, 'rbacRemoved': False, 'cloudResources': 'pending'})
        self.assertNotIn('private-', json.dumps(record))
        result = evaluate(self.profile, record)
        self.assertFalse(result['qualified'])
        self.assertEqual([c['id'] for c in result['checks'] if not c['passed']], ['cleanup'])
        self.example['nodes'][0]['samples']['baseline'].clear()
        self.assertTrue(record['nodes'][0]['samples']['baseline'])

    def test_partial_or_mismatched_image_evidence_is_rejected(self):
        for change in (lambda: self.execution.image_checks.pop('enabled'),
                       lambda: self.execution.image_checks['enabled'].update(checkedPods=4),
                       lambda: self.proof['candidate'].update(chartDigest='sha256:' + 'f' * 64)):
            self.setUp(); change()
            with self.assertRaises(ContractError):
                self.assemble()

    def test_missing_failed_or_partial_network_checks_cannot_pass(self):
        for change, expected in ((lambda: setattr(self, 'network', None), 'not-qualified'),
                                 (lambda: self.network['nodes'][1]['egress'].update(blocked=False), 'failed'),
                                 (lambda: self.network.update(freshSourceRetained=False), 'failed')):
            self.setUp(); change()
            record = self.assemble()
            self.assertEqual(record['transport']['networkPolicy'], expected)
            self.assertIn('network-policy', [c['id'] for c in evaluate(self.profile, record)['checks'] if not c['passed']])

    def test_missing_replacement_or_foreign_receipt_cannot_be_promoted(self):
        self.example['lifecycle']['providerNodeReplacement'].update(state='not-run', elapsedSeconds=None, identityVerified=False, freshEvidence=False)
        record = self.assemble()
        self.assertIn('providerNodeReplacement', [c['id'] for c in evaluate(self.profile, record)['checks'] if not c['passed']])
        self.receipt['qualificationToolCommit'] = 'f' * 40
        self.receipt['receiptDigest'] = digest(self.receipt, 'receiptDigest')
        with self.assertRaises(ContractError):
            self.assemble()

    def test_passing_replacement_needs_complete_proof_and_preserves_original_measurements(self):
        self.execution.replacement = None
        with self.assertRaises(ContractError):
            self.assemble()
        self.execution.replacement = {'bindingDigest': 'sha256:' + 'd' * 64}
        with self.assertRaises(ContractError):
            self.assemble()
        self.example['lifecycle']['providerNodeReplacement'].update(state='failed')
        partial = self.assemble()
        self.assertEqual(partial['nodes'], self.example['nodes'])
        self.execution.replacement = None
        original = self.assemble()
        self.assertNotEqual(partial['artefacts']['valuesDigest'], original['artefacts']['valuesDigest'])

    def test_replacement_can_only_reduce_field_and_provenance_claims(self):
        self.execution.binding = copy.deepcopy(self.execution.binding)
        self.execution.binding['nodes'][0]['name'] = 'replacement-name'
        self.execution.replacement['sourceSummary']['fields']['usage'] = 'unreported'
        self.execution.replacement['sourceSummary']['provenance'] = 'cri'
        record = self.assemble()
        self.assertEqual(record['fields']['usage'], 'unreported')
        self.assertEqual(record['provenance'], 'unknown')
        self.assertEqual(record['nodes'], self.example['nodes'])
        self.assertNotIn('replacement-name', json.dumps(record))

    def test_cleanup_failure_preserves_the_original_pending_record(self):
        record = self.assemble()
        before = copy.deepcopy(record)
        self.execution.cleanup.side_effect = ContractError('owned resource was replaced')
        with self.assertRaises(ContractError):
            cleanup_cluster(self.execution, record)
        self.assertEqual(record, before)

    def test_successful_cluster_cleanup_still_requires_provider_confirmation(self):
        record = self.assemble()
        cleaned = cleanup_cluster(self.execution, record)
        self.execution.cleanup.assert_called_once_with()
        self.assertTrue(cleaned['cleanup']['workloadsRemoved'])
        self.assertTrue(cleaned['cleanup']['rbacRemoved'])
        self.assertEqual(cleaned['cleanup']['cloudResources'], 'pending')
        self.assertFalse(record['cleanup']['workloadsRemoved'])
        self.assertEqual(evaluate(self.profile, cleaned)['outcome'], 'fail')
        with self.assertRaises(ContractError):
            cleanup_cluster(self.execution, cleaned)

    def test_cleanup_refuses_another_plan_before_any_mutation(self):
        record = self.assemble()
        self.execution.bundle.plan['files']['enabled-values.json'] = 'sha256:' + 'e' * 64
        with self.assertRaises(ContractError):
            cleanup_cluster(self.execution, record)
        self.execution.cleanup.assert_not_called()


if __name__ == '__main__':
    unittest.main()
