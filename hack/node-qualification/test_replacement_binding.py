import copy
import unittest
import uuid

from common import ContractError
from provider_inventory import collect_inventory, inspect_inventory
from replacement_binding import capture, detect, rebind
from test_provider_plan import ProviderFixture
import test_provider_inventory


class ReplacementBindingTest(ProviderFixture, unittest.TestCase):
    setup_case = test_provider_inventory.CurrentInventoryTest.setup_case

    def scenario(self):
        bundle, read, _, nodes = self.setup_case('gke-standard', 'gke-cos-containerd-amd64')
        for index, node in enumerate(nodes['items']):
            node['status']['nodeInfo'].update(systemUUID=str(uuid.UUID(int=index + 1)), bootID=str(uuid.UUID(int=index + 10)))
        receipt, bound = collect_inventory(bundle, read)
        machines = capture(bound, nodes)
        after = copy.deepcopy(nodes)
        node = after['items'][0]
        node['metadata'].update(uid='replacement-uid', name='z-last-name')
        node['status']['nodeInfo'].update(systemUUID=str(uuid.UUID(int=100)), bootID=str(uuid.UUID(int=110)))
        node['status']['addresses'][0]['address'] = '10.0.1.9'
        return bundle, bound, machines, receipt, after

    def test_replacement_preserves_slots_and_does_not_mutate_approved_inputs(self):
        b, before, machines, receipt, after = self.scenario()
        original = copy.deepcopy((b.configuration, before))
        bound, routes = rebind(b, before, machines, after, receipt, receipt, before['nodes'][0]['uid'])
        self.assertEqual(bound['nodes'][0]['name'], 'z-last-name')
        self.assertEqual(bound['nodes'][1], before['nodes'][1])
        self.assertEqual(bound['nodes'][0]['route'], '10.0.1.9/32')
        self.assertIn('10.0.1.9/32', routes)
        self.assertEqual((b.configuration, before), original)

    def test_reused_node_name_and_provider_resource_name_require_a_new_machine(self):
        b, before, machines, receipt, after = self.scenario()
        after['items'][0]['metadata']['name'] = before['nodes'][0]['name']
        bound, _ = rebind(b, before, machines, after, receipt, receipt, before['nodes'][0]['uid'])
        self.assertEqual(bound['nodes'][0]['name'], before['nodes'][0]['name'])
        self.assertNotEqual(bound['nodes'][0]['uid'], before['nodes'][0]['uid'])

    def test_detection_starts_before_readiness_and_waits_through_pool_overlap(self):
        b, before, machines, receipt, after = self.scenario()
        approved = before['nodes'][0]['uid']
        after['items'][0]['status']['conditions'][0]['status'] = 'False'
        self.assertEqual(detect(before, after, approved), 'replacement-uid')
        with self.assertRaises(ContractError):
            rebind(b, before, machines, after, receipt, receipt, approved)
        after['items'].pop()
        self.assertIsNone(detect(before, after, approved))
        after['items'] *= 2
        with self.assertRaises(ContractError):
            detect(before, after, approved)

    def test_registration_reboot_or_retained_machine_changes_are_rejected(self):
        for scenario in ('registration', 'reboot', 'retained'):
            b, before, machines, receipt, after = self.scenario()
            info = after['items'][0]['status']['nodeInfo']
            if scenario == 'registration':
                info.update(machines[before['nodes'][0]['uid']])
            elif scenario == 'reboot':
                info['systemUUID'] = machines[before['nodes'][0]['uid']]['systemUUID']
            else:
                after['items'][1]['status']['nodeInfo']['bootID'] = str(uuid.UUID(int=200))
            with self.subTest(scenario=scenario), self.assertRaises(ContractError):
                rebind(b, before, machines, after, receipt, receipt, before['nodes'][0]['uid'])

    def test_changed_pool_runtime_route_or_provider_receipt_is_rejected(self):
        for scenario in ('pool', 'runtime', 'route', 'receipt', 'unapproved target'):
            b, before, machines, receipt, after = self.scenario()
            fresh = copy.deepcopy(receipt)
            target = before['nodes'][0]['uid']
            if scenario == 'pool':
                after['items'][0]['metadata']['labels']['cloud.google.com/gke-nodepool'] = 'other'
            elif scenario == 'runtime':
                for node in after['items']:
                    node['status']['nodeInfo']['kernelVersion'] = '6.12.11'
            elif scenario == 'route':
                after['items'][0]['status']['addresses'][0]['address'] = '127.0.0.1'
            elif scenario == 'receipt':
                fresh['nodeImage'] = 'different'
            else:
                target = before['nodes'][1]['uid']
            with self.subTest(scenario=scenario), self.assertRaises(ContractError):
                rebind(b, before, machines, after, receipt, fresh, target)

    def test_unavailable_machine_identity_is_rejected_before_any_replacement(self):
        for value in ('', 'unknown', str(uuid.UUID(int=0)), str(uuid.UUID(int=(1 << 128)-1))):
            b, read, _, nodes = self.setup_case('gke-standard', 'gke-cos-containerd-amd64')
            _, binding = collect_inventory(b, read)
            nodes['items'][0]['status']['nodeInfo'].update(systemUUID=value, bootID=str(uuid.UUID(int=1)))
            with self.assertRaises(ContractError):
                capture(binding, nodes)

    def test_raw_inventory_inspection_does_not_weaken_initial_route_binding(self):
        b, read, _, nodes = self.setup_case('gke-standard', 'gke-cos-containerd-amd64')
        nodes['items'][0]['status']['addresses'][0]['address'] = '10.0.1.9'
        receipt, observed = inspect_inventory(b, read)
        self.assertEqual(observed, nodes)
        self.assertEqual(receipt['provider'], b.profile['provider'])
        with self.assertRaises(ContractError):
            collect_inventory(b, read)


if __name__ == '__main__':
    unittest.main()
