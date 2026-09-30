import http.client
import json
import ssl
import unittest
from unittest.mock import patch

from transport import QualificationError, Transport, local_endpoint, pod_path, resource_path, strict_object


class Response:
    status = 403

    def __init__(self, data=b'{"reason":"Forbidden"}', content_type='application/json'):
        self.data, self.content_type = data, content_type
        self.limit = None

    def getheader(self, _, default):
        return self.content_type or default

    def read(self, limit):
        self.limit = limit
        return self.data[:limit]


class Connection:
    def __init__(self, response):
        self.response, self.closed = response, False
        self.request_value = None

    def request(self, *args):
        self.request_value = args

    def getresponse(self):
        return self.response

    def close(self):
        self.closed = True


class TransportTests(unittest.TestCase):
    def setUp(self):
        self.tls = ssl.create_default_context()
        self.client = Transport('https://127.0.0.1:6443', self.tls, 'private-token')
        self.path = resource_path('fixture-a')

    def test_remote_or_ambiguous_endpoints_are_rejected(self):
        for value in ['http://127.0.0.1', 'https://example.com', 'https://10.0.0.1', 'https://user@localhost',
                      'https://localhost/path', 'https://localhost?q=1', 'https://localhost#fragment', 'https://localhost:bad']:
            with self.subTest(value=value), self.assertRaises(QualificationError):
                local_endpoint(value)
        self.assertEqual(local_endpoint('https://[::1]:6443'), ('::1', 6443))

    def test_insecure_tls_and_header_injection_are_rejected(self):
        insecure = ssl._create_unverified_context()
        with self.assertRaises(QualificationError):
            Transport('https://localhost', insecure, 'token')
        for token in ['', 'private\r\nInjected: value', 'token value', 'x'*16385]:
            with self.assertRaises(QualificationError):
                Transport('https://localhost', self.tls, token)

    def test_request_and_response_are_bounded_and_public_receipt_is_redacted(self):
        response = Response(b'{"private":"private-pod-uid"}')
        connection = Connection(response)
        with patch('transport.http.client.HTTPSConnection', return_value=connection) as factory:
            got = self.client.call('POST', self.path, {'pod': 'fixture'})
        self.assertEqual(factory.call_args.kwargs['timeout'], 5)
        self.assertIs(factory.call_args.kwargs['context'], self.tls)
        self.assertEqual(response.limit, 65537)
        self.assertTrue(connection.closed)
        self.assertEqual(connection.request_value[3]['Authorization'], 'Bearer private-token')
        self.assertEqual(got.body['private'], 'private-pod-uid')
        self.assertEqual(set(got.public()), {'status', 'elapsedNanos', 'responseBytes'})
        self.assertNotIn('private-token', repr(self.client))
        self.assertNotIn('private-pod-uid', repr(got)+json.dumps(got.public()))

    def test_malformed_oversized_and_stream_responses_close_transport(self):
        for response in [Response(b'{}'+b' '*65535), Response(b'{"a":1,"a":2}'),
                         Response(b'[]'), Response(b'{"a":NaN}'), Response(b'{}', 'application/x-ndjson')]:
            connection = Connection(response)
            with patch('transport.http.client.HTTPSConnection', return_value=connection):
                with self.assertRaises(QualificationError):
                    self.client.call('GET', self.path)
            self.assertTrue(connection.closed)

    def test_transport_errors_never_include_dependency_text(self):
        connection = Connection(None)
        def fail(*_):
            raise http.client.HTTPException('private-token')
        connection.request = fail
        with patch('transport.http.client.HTTPSConnection', return_value=connection):
            with self.assertRaises(QualificationError) as caught:
                self.client.call('GET', self.path)
        self.assertNotIn('private-token', str(caught.exception))
        self.assertTrue(connection.closed)

    def test_invalid_paths_and_large_requests_never_open_transport(self):
        for method, path, body in [('PUT', self.path, None), ('GET', self.path+'?x=1', None),
                ('GET', self.path+'/../other', None), ('GET', self.path, {}),
                ('POST', self.path, {'value': 'x'*4096}), ('POST', self.path, {'value': float('nan')})]:
            with patch('transport.http.client.HTTPSConnection') as factory:
                with self.assertRaises(QualificationError):
                    self.client.call(method, path, body)
                factory.assert_not_called()

    def test_resource_routes_cannot_escape_scope(self):
        for name in ['../other', None, 7]:
            with self.assertRaises(QualificationError):
                resource_path(name)
        with self.assertRaises(QualificationError):
            resource_path('fixture-a', stream=True)
        with self.assertRaises(QualificationError):
            resource_path('fixture-a', 'tracepreflights', 'a'*32)
        self.assertTrue(resource_path('fixture-a', name='a'*32, stream=True).endswith('/stream'))
        with self.assertRaises(QualificationError):
            strict_object(b'{"nested":{"key":1,"key":2}}')

    def test_pod_discovery_allows_only_a_named_read(self):
        path = pod_path('fixture-a', 'target')
        connection = Connection(Response())
        with patch('transport.http.client.HTTPSConnection', return_value=connection):
            self.assertEqual(self.client.call('GET', path).status, 403)
        for method, route in [('DELETE', path), ('POST', path), ('GET', path+'/log'),
                              ('GET', '/api/v1/namespaces/fixture-a/secrets/target')]:
            with patch('transport.http.client.HTTPSConnection') as factory:
                with self.assertRaises(QualificationError):
                    self.client.call(method, route)
                factory.assert_not_called()


if __name__ == '__main__':
    unittest.main()
