import unittest

import fixtures
from transport import QualificationError

RUN = 'a'*32
IMAGE = 'docker.io/library/fixture@sha256:'+'b'*64


class FixtureTests(unittest.TestCase):
    def test_pod_is_restricted_finite_and_cannot_pull_or_restart(self):
        value = fixtures.pod('kml-isolation-a', 'target', 'owned-node', IMAGE, RUN)
        spec = value['spec']; container = spec['containers'][0]
        self.assertEqual(value['metadata']['labels'][fixtures.LABEL], RUN)
        self.assertEqual(spec['nodeName'], 'owned-node')
        self.assertFalse(spec['automountServiceAccountToken'])
        self.assertEqual(spec['restartPolicy'], 'Never')
        self.assertEqual(spec['activeDeadlineSeconds'], 1800)
        self.assertTrue(spec['securityContext']['runAsNonRoot'])
        self.assertEqual(container['image'], IMAGE)
        self.assertEqual(container['imagePullPolicy'], 'Never')
        self.assertEqual(container['securityContext'], {'allowPrivilegeEscalation': False,
                         'readOnlyRootFilesystem': True, 'capabilities': {'drop': ['ALL']}})
        self.assertEqual(container['resources']['limits'], {'cpu': '500m', 'memory': '64Mi'})
        self.assertEqual(spec['volumes'], [{'name': 'work', 'emptyDir': {'sizeLimit': '128Mi'}}])

    def test_grants_are_namespace_scoped_and_accounts_have_no_automatic_token(self):
        account = fixtures.service_account('kml-isolation-a', 'tenant', RUN)
        grant = fixtures.role_binding('kml-isolation-b', 'fixture-admin', 'reviewed-operator',
                                      'kml-isolation-control', 'administrator', RUN)
        self.assertFalse(account['automountServiceAccountToken'])
        self.assertEqual(grant['kind'], 'RoleBinding')
        self.assertEqual(grant['metadata']['namespace'], 'kml-isolation-b')
        self.assertEqual(grant['roleRef']['name'], 'reviewed-operator')
        self.assertEqual(grant['subjects'], [{'kind': 'ServiceAccount', 'namespace': 'kml-isolation-control', 'name': 'administrator'}])

    def test_provider_job_has_one_controller_created_child_and_no_retries(self):
        value = fixtures.job('kml-isolation-a', 'target', 'ip-10-0-1-2.ec2.internal', IMAGE, RUN)
        self.assertEqual(value['apiVersion'], 'batch/v1')
        self.assertEqual(value['kind'], 'Job')
        self.assertEqual(value['spec']['backoffLimit'], 0)
        self.assertEqual(value['spec']['parallelism'], 1)
        self.assertEqual(value['spec']['completions'], 1)
        self.assertEqual(value['spec']['activeDeadlineSeconds'], 1800)
        template = value['spec']['template']
        self.assertEqual(template['metadata']['labels'][fixtures.LABEL], RUN)
        self.assertNotIn('ownerReferences', template['metadata'])
        expected = fixtures.pod('kml-isolation-a', 'target', 'ip-10-0-1-2.ec2.internal', IMAGE, RUN)['spec']
        expected['containers'][0]['imagePullPolicy'] = 'IfNotPresent'
        self.assertEqual(template['spec'], expected)

    def test_node_dns_names_are_validated_without_restricting_them_to_pod_labels(self):
        for node in ('ip-10-0-1-2.ec2.internal', 'worker', 'a'*63+'.'+'b'*63):
            self.assertEqual(fixtures.pod('kml-isolation-a', 'target', node, IMAGE, RUN)['spec']['nodeName'], node)
        for node in ('', 'a..b', '.a', 'a.', '-a', 'a-', 'A.example', 'a'*64, 'a/'+'b', '.'.join(['a'*63]*4)):
            with self.subTest(node=node), self.assertRaises(QualificationError):
                fixtures.job('kml-isolation-a', 'target', node, IMAGE, RUN)

    def test_unknown_ownership_and_mutable_images_are_rejected(self):
        for name, image, run in [('default', IMAGE, RUN), ('kml-isolation-a', 'fixture:latest', RUN),
                                  ('kml-isolation-a', IMAGE, 'missing')]:
            with self.assertRaises(QualificationError):
                fixtures.pod(name, 'target', 'owned-node', image, run)


if __name__ == '__main__':
    unittest.main()
