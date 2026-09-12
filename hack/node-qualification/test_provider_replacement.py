import copy
import json
import tempfile
import unittest
import uuid
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

from common import ContractError, load
from owned_resources import OwnedResources, Resource
from provider_inventory import collect_inventory
from provider_replacement import ACKNOWLEDGEMENT, ProviderReplacement
from test_owned_mutations import VersionedKubernetes
from test_provider_plan import ProviderFixture
from test_replacement_policy import policy
import test_provider_inventory


class ReplacementObservationTest(ProviderFixture, unittest.TestCase):
    setup_case = test_provider_inventory.CurrentInventoryTest.setup_case

    def setUp(self):
        private = tempfile.TemporaryDirectory(); self.addCleanup(private.cleanup)
        self.private = Path(private.name)
        bundle, read, _, nodes = self.setup_case('gke-standard', 'gke-cos-containerd-amd64')
        for slot, node in enumerate(nodes['items']):
            node['status']['nodeInfo'].update(systemUUID=str(uuid.UUID(int=slot+1)), bootID=str(uuid.UUID(int=slot+10)))
        self.receipt, binding = collect_inventory(bundle, read)
        self.before = copy.deepcopy(nodes); self.after = copy.deepcopy(nodes)
        changed = self.after['items'][0]
        changed['metadata'].update(name='replacement', uid='replacement-uid')
        changed['status']['addresses'][0]['address'] = '10.0.1.9'
        changed['status']['nodeInfo'].update(systemUUID=str(uuid.UUID(int=100)), bootID=str(uuid.UUID(int=110)))
        self.calls = 0; self.now = 0; self.keep_stale = False
        def k(*args):
            self.calls += 1
            return json.dumps(self.before if self.calls == 1 else self.after)
        self.kube = VersionedKubernetes()
        owner = OwnedResources(self.kube, self.private/'ownership')
        namespace = Resource('v1', 'Namespace', bundle.configuration['namespace'])
        owner.create({'apiVersion':'v1','kind':'Namespace','metadata':{'name':namespace.name}})
        document = policy(namespace.name); resource = Resource.from_object(document); owner.create(document)
        self.policy = resource
        image = {'imageDigest':bundle.configuration['imageDigest'],'architecture':'amd64'}
        self.images = {**image,'allMatched':True,'checkedPods':5}
        self.e = SimpleNamespace(bundle=bundle, binding=binding, initial_binding=copy.deepcopy(binding), replacement=None,
            windows={'baseline':{},'enabled':{}}, image_proof=image, api_bridge=self.private/'bridge', private=self.private,
            verify_binding=Mock(), ownership=owner, k=k,
            installer=SimpleNamespace(namespace=namespace,desired={'enabled':{resource:copy.deepcopy(document)}}))
        self.e.runtimes = [self.runtime(n['name'],n['uid']) for n in binding['nodes']]
        self.subject = ProviderReplacement(self.e,self.receipt,clock=lambda:self.now,sleep=self.sleep)

    def sleep(self, seconds):
        self.now += seconds

    def runtime(self, name, uid):
        def api(path):
            if path == '/clusterstatus/current':
                return {'store':{'reliability':{'freshNodes':2},'nodeContext':{'freshRecords':2}}}
            changed = self.e.binding['nodes'][0]['uid'] == 'replacement-uid' and not self.keep_stale
            good = {'nodeName':name,'nodeUID':uid,'reportedAt':'2026-09-11T12:10:00Z' if changed else '2026-09-11T12:00:00Z',
                    'stats':{'provenance':'unknown','memory':{'usageBytes':0}},'context':{}}
            return {'record':{'nodeUID':uid,'freshness':'fresh','lastGood':good,'report':{'nodeUID':uid}}}
        return SimpleNamespace(node=name,node_uid=uid,api=api,
            containers=lambda:{'agent':{'id':'a'*64},'collector':{'id':'b'*64},'node-context':{'id':'c'*64}},
            component_metrics=lambda *args:{'kubememlens_agent_snapshot_posts_total{result="success"}':1})

    def run_observer(self, inspect=None):
        def factory(kubeconfig, context, namespace, namespace_uid, node, bridge, node_uid):
            return self.runtime(node,node_uid)
        with patch('provider_replacement.inspect_inventory',side_effect=inspect or (lambda b:(self.receipt,self.after))), \
             patch('provider_replacement.KubernetesRuntime',side_effect=factory), \
             patch('provider_replacement.attach') as attach, \
             patch('provider_replacement.verify_images',return_value=self.images):
            result = self.subject.run(0,ACKNOWLEDGEMENT)
            self.assertEqual([call.args[2] for call in attach.call_args_list],['agent','node-context'])
            return result

    def test_full_observation_rebinds_one_slot_and_retains_private_evidence(self):
        original = copy.deepcopy(self.e.initial_binding)
        result = self.run_observer()
        self.assertEqual(result['state'],'passed')
        self.assertEqual(self.e.initial_binding,original)
        self.assertEqual(self.e.binding['nodes'][0]['uid'],'replacement-uid')
        self.assertEqual(self.e.binding['nodes'][1],original['nodes'][1])
        self.assertEqual(self.e.replacement['liveImages'],self.images)
        self.assertEqual(self.e.replacement['sourceSummary']['fields']['usage'],'available')
        self.assertNotIn('private-',json.dumps(self.e.replacement))
        target=load(self.private/'replacement-target.private.json')
        self.assertEqual(target['node']['uid'],original['nodes'][0]['uid'])
        bound=load(self.private/'replacement-binding.private.json')
        self.assertEqual(bound['nodeCIDRs'],['10.0.1.9/32','10.0.1.3/32'])
        for path in self.private.glob('*.json'):
            self.assertEqual(path.stat().st_mode & 0o777,0o600)

    def test_approval_and_slot_are_checked_before_any_target_read(self):
        for slot,ack in ((0,'not-approved'),(True,ACKNOWLEDGEMENT),(2,ACKNOWLEDGEMENT)):
            with self.assertRaises(ContractError):
                self.subject.run(slot,ack)
        self.assertEqual(self.calls,0)
        self.assertEqual(self.kube.patch_count,0)

    def test_inventory_over_budget_prevents_policy_mutation(self):
        def slow(bundle):
            self.now += 121
            return self.receipt,self.after
        with self.assertRaisesRegex(ContractError,'replacement inventory'):
            self.run_observer(slow)
        self.assertEqual(self.kube.patch_count,0)
        self.assertIsNone(self.e.replacement)

    def test_stale_replacement_cannot_get_a_passing_result(self):
        self.keep_stale=True
        with self.assertRaisesRegex(ContractError,'replacement source recovery'):
            self.run_observer()
        self.assertIn('bindingDigest',self.e.replacement)
        self.assertNotIn('liveImages',self.e.replacement)
        self.assertTrue((self.private/'replacement-binding.private.json').exists())

    def test_registration_timeout_does_not_change_resources(self):
        self.after=copy.deepcopy(self.before)
        with patch('provider_replacement.REGISTRATION_WAIT_SECONDS',4), self.assertRaisesRegex(ContractError,'replacement registration'):
            self.run_observer()
        self.assertEqual(self.kube.patch_count,0)
        self.assertIsNone(self.e.replacement)


if __name__=='__main__':
    unittest.main()
