import copy
import tempfile
import unittest
from pathlib import Path

from common import ContractError
from owned_resources import OwnedResources, Resource
from replacement_policy import replace_route, spec
from test_owned_mutations import VersionedKubernetes


def policy(namespace='fixture'):
    return {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
            'metadata': {'name': 'kube-memlens-node-context', 'namespace': namespace},
            'spec': {'podSelector': {'matchLabels': {'app.kubernetes.io/name': 'kube-memlens-node-context'}},
                     'policyTypes': ['Ingress', 'Egress'], 'ingress': [],
                     'egress': [{'to': [{'ipBlock': {'cidr': '10.0.0.1/32'}}],
                                 'ports': [{'protocol': 'TCP', 'port': 443}]},
                                {'to': [{'ipBlock': {'cidr': route}} for route in ('10.0.1.2/32', '10.0.1.3/32')],
                                 'ports': [{'protocol': 'TCP', 'port': 10250}]}]}}


class ReplacementPolicyTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(); self.addCleanup(temporary.cleanup)
        self.k = VersionedKubernetes()
        self.owner = OwnedResources(self.k, Path(temporary.name) / 'ownership')
        self.policy = policy(); self.resource = Resource.from_object(self.policy)
        self.owner.create(self.policy)

    def replace(self, old='10.0.1.2/32', new='10.0.1.9/32'):
        return replace_route(self.owner, 'fixture', self.policy['spec'], old, new)

    def test_only_the_selected_kubelet_host_changes_and_empty_ingress_is_preserved(self):
        self.k.objects[self.resource]['spec'].pop('ingress')
        before = copy.deepcopy(self.policy)
        result = self.replace()
        self.assertEqual(result['egress'][0], before['spec']['egress'][0])
        self.assertEqual(result['egress'][1]['ports'], before['spec']['egress'][1]['ports'])
        self.assertEqual(result['egress'][1]['to'], [{'ipBlock': {'cidr': '10.0.1.9/32'}}, {'ipBlock': {'cidr': '10.0.1.3/32'}}])
        self.assertEqual(spec(result)['ingress'], [])
        self.assertEqual(self.policy, before)

    def test_same_route_is_checked_without_an_unnecessary_mutation(self):
        self.assertEqual(self.replace(new='10.0.1.2/32'), self.policy['spec'])
        self.assertEqual(self.k.patch_count, 0)

    def test_widening_duplicate_wrong_family_or_missing_route_is_rejected(self):
        for old, new in (('10.0.1.2/32', '10.0.0.0/8'), ('10.0.1.2/32', '10.0.1.3/32'),
                         ('10.0.1.2/32', 'fd00::9/128'), ('10.0.1.8/32', '10.0.1.9/32')):
            with self.subTest(new=new), self.assertRaises(ContractError):
                self.replace(old, new)
        self.assertEqual(self.k.patch_count, 0)

    def test_operator_edit_or_resource_replacement_is_not_overwritten(self):
        self.k.objects[self.resource]['spec']['egress'][0]['ports'][0]['port'] = 8080
        with self.assertRaises(ContractError):
            self.replace()
        self.assertEqual(self.k.patch_count, 0)
        self.k.objects[self.resource]['spec'] = copy.deepcopy(self.policy['spec'])
        self.k.before_patch = lambda: self.k.objects[self.resource]['metadata'].update(resourceVersion='99')
        with self.assertRaises(ContractError):
            self.replace()
        self.assertEqual(self.k.objects[self.resource]['metadata']['resourceVersion'], '99')
        self.assertEqual(self.k.patch_count, 0)


if __name__ == '__main__':
    unittest.main()
