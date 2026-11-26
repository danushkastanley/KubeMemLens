import unittest

from access import AccessCases, admission_id, denial
from transport import Observation, QualificationError


def status(code=403, reason='Forbidden'):
    return {'apiVersion': 'v1', 'kind': 'Status', 'metadata': {}, 'status': 'Failure', 'code': code, 'reason': reason, 'message': 'request denied'}


def observation(body, code=403):
    return Observation(code, 1000, 100, body)


class AccessAssertionsTests(unittest.TestCase):
    def test_denial_retains_kubernetes_reason_without_private_target(self):
        self.assertEqual(denial(observation(status()), 403, ['private-uid']), 'Forbidden')
        self.assertEqual(denial(observation(status(404, 'NotFound'), 404), 404), 'NotFound')

    def test_success_throttle_partial_shape_and_identity_are_not_denial_evidence(self):
        variants = [observation(status(), 200), observation(status(), 429),
                    observation(dict(status(), code=True)), observation(dict(status(), reason='Other')),
                    observation(dict(status(), summary={})), observation(dict(status(), metadata={'uid': 'private-uid'})),
                    observation(dict(status(), details={'uid': 'another-private-uid'})),
                    observation(dict(status(), message='private-uid'))]
        for value in variants:
            with self.assertRaises(QualificationError):
                denial(value, 403, ['private-uid'])

    def test_admission_must_be_exact_namespace_engine_and_private_handle(self):
        body = {'apiVersion': 'tracing.kubememlens.io/v1alpha1', 'kind': 'TraceAdmission',
                'metadata': {'name': 'a'*32, 'namespace': 'fixture-a'}, 'state': 'admitted',
                'expiresAt': '2026-09-30T00:00:15Z', 'engineDigest': 'sha256:'+'b'*64}
        got = Observation(201, 1000, 100, body)
        self.assertEqual(admission_id(got, 'fixture-a', body['engineDigest']), 'a'*32)
        for namespace, engine in [('other', body['engineDigest']), ('fixture-a', 'unverified')]:
            with self.assertRaises(QualificationError):
                admission_id(got, namespace, engine)
        for invalid in [None, 'not-a-clock', '2026-09-30', '0001-01-01T00:00:00Z']:
            with self.assertRaises(QualificationError):
                admission_id(observation(dict(body, expiresAt=invalid), 201), 'fixture-a', body['engineDigest'])
        body['target'] = 'private-uid'
        with self.assertRaises(QualificationError):
            admission_id(got, 'fixture-a', body['engineDigest'])

    def test_missing_actors_or_identical_namespaces_do_not_narrow_the_suite(self):
        with self.assertRaises(QualificationError):
            AccessCases({}, {'a': 'one', 'b': 'two'}, 'engine', [])
        with self.assertRaises(QualificationError):
            AccessCases(dict.fromkeys(['a', 'b', 'admin', 'colleague', 'none']), {'a': 'one', 'b': 'one'}, 'engine', [])

    def test_malformed_success_still_cancels_its_known_owned_handle(self):
        calls = []

        class Actor:
            def __init__(self, name):
                self.name = name

            def call(self, method, path, body=None):
                calls.append((self.name, method, path))
                if self.name == 'a' and method == 'POST' and path.endswith('/fixture-a/traces'):
                    return observation({'metadata': {'name': 'a'*32}}, 201)
                if self.name == 'a' and method == 'DELETE':
                    return observation(dict(status(200, 'Success'), status='Success'), 200)
                return observation(status())

        suite = AccessCases({name: Actor(name) for name in ['a', 'b', 'colleague', 'admin', 'none']},
                            {'a': 'fixture-a', 'b': 'fixture-b'}, 'sha256:'+'b'*64, [])
        with self.assertRaises(QualificationError):
            suite.verify()
        self.assertEqual(len([call for call in calls if call[1] == 'DELETE']), 1)
        self.assertEqual(suite.pending, [])


if __name__ == '__main__':
    unittest.main()
