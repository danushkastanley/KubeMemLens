import json
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from fixtures import Fixtures
from profile import load_profile


class FixtureCleanupTests(unittest.TestCase):
    def fixture(self):
        case = Mock()
        case.cfg = {'owner': 'test-owned', 'standardNamespace': 'kml-active-standard', 'release': 'kml-active-release'}
        case.runtime.cfg = {'kubeconfig': 'private', 'context': 'kind-local'}
        fixture = Fixtures(case, Mock())
        fixture.namespaces = {'kml-active-a': 'uid-a', 'kml-active-standard': 'uid-standard'}
        case.namespace.side_effect = lambda ns: {'metadata': {'uid': fixture.namespaces[ns], 'resourceVersion': '10'}}
        case.kube.return_value = b'{}'
        return case, fixture

    def test_paired_fixture_covers_both_windows_with_a_fixed_deadline(self):
        case, fixture = self.fixture()
        case.node = "owned-worker"
        case.cfg["fixtureImage"] = "fixture@sha256:" + "a" * 64
        pod = fixture.pod("kml-active-a", "target")
        profile = load_profile()
        duration = 2 * (profile["windowSeconds"] + profile["warmupSeconds"]) + 600
        self.assertLess(duration, pod["spec"]["activeDeadlineSeconds"])
        self.assertEqual(pod["spec"]["activeDeadlineSeconds"], 3600)
        self.assertEqual(pod["spec"]["containers"][0]["command"],
                         ["/usr/local/bin/kml-io-workload", "paired-idle"])

    def test_replaced_reader_is_preserved_and_owned_namespaces_still_removed(self):
        case, fixture = self.fixture()
        fixture.reader_uid = 'old-reader'
        def kube(args, *rest, **kwargs):
            if args[:2] == ['get', 'clusterrolebinding']:
                return json.dumps({'metadata': {'uid': 'replacement', 'labels': {'kube-memlens.io/fixture': 'test-owned'}}}).encode()
            return b'{}'
        case.kube.side_effect = kube
        with self.assertRaises(ValueError):
            fixture.cleanup()
        deleted = [c.args for c in case.kube.call_args_list if c.args[0][:2] == ['delete', '--raw']]
        self.assertEqual(len(deleted), 2)
        self.assertTrue(all('/namespaces/' in args[0][2] for args in deleted))
        for args in deleted:
            self.assertIn('uid', json.loads(args[1])['preconditions'])
            self.assertEqual(json.loads(args[1])['preconditions']['resourceVersion'], '10')
        self.assertFalse(fixture.cleaned)

    def test_failed_helm_cleanup_retains_standard_namespace_and_continues_fixtures(self):
        case, fixture = self.fixture()
        fixture.helm_started = True
        with patch('fixtures.command', side_effect=RuntimeError('helm unavailable')):
            with self.assertRaises(ValueError):
                fixture.cleanup()
        deleted = [c.args[0][2] for c in case.kube.call_args_list if c.args[0][:2] == ['delete', '--raw']]
        self.assertEqual(deleted, ['/api/v1/namespaces/kml-active-a'])
        self.assertTrue(fixture.helm_started)

    def test_successful_cleanup_is_idempotent(self):
        case, fixture = self.fixture()
        fixture.cleanup()
        calls = len(case.kube.call_args_list)
        fixture.cleanup()
        self.assertTrue(fixture.cleaned)
        self.assertEqual(len(case.kube.call_args_list), calls)


if __name__ == '__main__':
    unittest.main()
