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

    def test_unknown_ownership_and_mutable_images_are_rejected(self):
        for name, image, run in [('default', IMAGE, RUN), ('kml-isolation-a', 'fixture:latest', RUN),
                                  ('kml-isolation-a', IMAGE, 'missing')]:
            with self.assertRaises(QualificationError):
                fixtures.pod(name, 'target', 'owned-node', image, run)


if __name__ == '__main__':
    unittest.main()
