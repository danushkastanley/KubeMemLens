from copy import deepcopy
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from fixture_jobs import FixtureJobs, child, job_from_pod
from fixtures import Fixtures


def fixture():
    case = SimpleNamespace(node='owned-worker', namespaces=('kml-active-a',), owner='owned-run',
        cfg={'owner': 'owned-run', 'standardNamespace': 'kml-active-standard', 'fixtureImage': 'fixture@sha256:' + 'a' * 64})
    create = Mock()
    manager = FixtureJobs(case, create, Mock())
    template = Fixtures(case, Mock()).pod('kml-active-a', 'target')
    parent = job_from_pod(template)
    parent['metadata']['uid'] = 'job-lifetime'
    parent['spec']['selector'] = {'matchLabels': {'batch.kubernetes.io/controller-uid': 'job-lifetime'}}
    create.return_value = parent
    case.kube = Mock(return_value=b'')
    pod = deepcopy(template)
    pod['metadata'].update(name='target-controller-child', uid='pod-lifetime', ownerReferences=[{
        'apiVersion': 'batch/v1', 'kind': 'Job', 'controller': True, 'name': 'target', 'uid': 'job-lifetime'}])
    pod['status'] = {'phase': 'Running', 'containerStatuses': [{'name': 'worker', 'ready': True,
        'restartCount': 0, 'state': {'running': {}}}]}
    case.json = lambda args: parent if args[3] == 'job' else {'items': [pod]}
    return case, manager, template, parent, pod


class FixtureJobTests(unittest.TestCase):
    def test_job_preserves_exact_bounded_fixture_and_has_no_fabricated_owner(self):
        _, _, template, _, _ = fixture()
        job = job_from_pod(template)
        self.assertEqual(job['spec']['template']['spec'], template['spec'])
        self.assertEqual(job['spec']['backoffLimit'], 0)
        self.assertEqual(job['spec']['parallelism'], 1)
        self.assertEqual(job['spec']['completions'], 1)
        self.assertEqual(job['spec']['activeDeadlineSeconds'], 3600)
        self.assertNotIn('ownerReferences', job['spec']['template']['metadata'])
        self.assertNotIn('name', job['spec']['template']['metadata'])
        job['spec']['template']['spec']['containers'][0]['image'] = 'changed'
        self.assertNotEqual(job['spec']['template']['spec'], template['spec'])

    def test_resolves_real_child_name_and_rejects_replacement_after_binding(self):
        _, manager, template, _, pod = fixture()
        manager.add(template); manager.ready(); manager.verify()
        self.assertEqual(manager.pod_name('kml-active-a', 'target'), 'target-controller-child')
        pod['metadata']['uid'] = 'replacement'
        with self.assertRaisesRegex(ValueError, 'replaced its bound child'):
            manager.verify()

    def test_foreign_duplicate_terminated_and_restarted_children_fail(self):
        _, _, _, parent, original = fixture()
        mutations = [lambda p: p['metadata']['ownerReferences'][0].update(uid='foreign'),
                     lambda p: p['metadata'].update(namespace='foreign'),
                     lambda p: p['metadata'].update(deletionTimestamp='now'),
                     lambda p: p['spec'].update(nodeName='foreign'),
                     lambda p: p['status'].update(phase='Failed'),
                     lambda p: p['status']['containerStatuses'][0].update(restartCount=1)]
        for change in mutations:
            pod = deepcopy(original); change(pod)
            with self.subTest(change=mutations.index(change)), self.assertRaises(ValueError):
                child(parent, [pod], 'owned-worker')
        with self.assertRaises(ValueError):
            child(parent, [original, original], 'owned-worker')

    def test_changed_controller_or_failed_job_is_not_retried(self):
        for changed in ('uid', 'spec', 'status'):
            _, manager, template, parent, _ = fixture()
            manager.add(template); manager.ready()
            if changed == 'uid': parent['metadata']['uid'] = 'foreign'
            elif changed == 'spec': parent['spec']['backoffLimit'] = 1
            else: parent['status'] = {'failed': 1}
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                manager.verify()

    def test_admission_cannot_expand_retry_resource_or_privilege_bounds(self):
        mutations = [lambda p: p['spec'].update(backoffLimit=1),
                     lambda p: p['spec']['template']['spec'].update(hostPID=True),
                     lambda p: p['spec']['template']['spec']['containers'][0]['resources']['limits'].update(memory='128Mi'),
                     lambda p: p['spec']['template']['spec']['containers'][0]['securityContext'].update(privileged=True)]
        for change in mutations:
            _, manager, template, parent, _ = fixture()
            change(parent)
            with self.subTest(change=mutations.index(change)), self.assertRaises(ValueError):
                manager.add(template)

    def test_existing_job_is_not_adopted_and_unbound_name_is_unavailable(self):
        case, manager, template, _, _ = fixture()
        case.kube.return_value = b'job/target'
        with self.assertRaisesRegex(ValueError, 'do not adopt'):
            manager.add(template)
        manager.create.assert_not_called()
        with self.assertRaisesRegex(ValueError, 'not been bound'):
            manager.pod_name('kml-active-a', 'target')

    def test_missing_child_has_a_fixed_shared_deadline(self):
        case, manager, template, parent, _ = fixture()
        manager.add(template)
        case.json = lambda args: parent if args[3] == 'job' else {'items': []}
        with patch('fixture_jobs.time.monotonic', side_effect=[0, 91]):
            with self.assertRaisesRegex(ValueError, 'deadline'):
                manager.ready()


if __name__ == '__main__':
    unittest.main()
