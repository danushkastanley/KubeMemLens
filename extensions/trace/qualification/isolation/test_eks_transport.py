import base64
import copy
import hashlib
import ssl
import unittest
from unittest.mock import patch

from eks_transport import EKSTransport, endpoint
from test_transport import Connection, Response
from transport import QualificationError, Transport, resource_path


class EKSTransportTests(unittest.TestCase):
    def setUp(self):
        self.server = 'https://owned.us-east-1.eks.amazonaws.com'
        self.ca = b'fixture CA bytes'
        self.binding = {'endpoint': self.server, 'caSHA256': hashlib.sha256(self.ca).hexdigest()}
        self.config = {'current-context': 'qualification-eks',
            'users': [{'name': 'actor', 'user': {'token': 'private-token'}}],
            'clusters': [{'name': 'owned', 'cluster': {'server': self.server,
                'certificate-authority-data': base64.b64encode(self.ca).decode()}}],
            'contexts': [{'name': 'qualification-eks', 'context': {'cluster': 'owned', 'user': 'actor'}}]}
        self.tls = ssl.create_default_context()

    def make(self):
        with patch('eks_transport.ssl.create_default_context', return_value=self.tls) as create:
            result = EKSTransport(self.binding, self.config)
            create.assert_called_once_with(cadata=self.ca.decode())
            return result

    def test_exact_bound_endpoint_reuses_bounded_requests_and_redaction(self):
        client = self.make(); response = Response(); connection = Connection(response)
        with patch('transport.http.client.HTTPSConnection', return_value=connection) as factory:
            observation = client.call('GET', resource_path('kml-isolation-a', name='a'*32))
        self.assertEqual(factory.call_args.args, ('owned.us-east-1.eks.amazonaws.com', 443))
        self.assertEqual(factory.call_args.kwargs['timeout'], 5)
        self.assertEqual(response.limit, 65537)
        self.assertTrue(connection.closed)
        self.assertNotIn('private-token', repr(client) + repr(observation))
        with self.assertRaises(QualificationError): Transport(self.server, self.tls, 'private-token')

    def test_wrong_endpoint_ca_context_or_alternate_auth_cannot_open_connection(self):
        original = copy.deepcopy(self.config)
        changes = [lambda c: c['clusters'][0]['cluster'].update(server='https://other.eks.amazonaws.com'),
            lambda c: c['clusters'][0]['cluster'].update({'certificate-authority-data': base64.b64encode(b'other').decode()}),
            lambda c: c['clusters'][0]['cluster'].update({'insecure-skip-tls-verify': True}),
            lambda c: c['users'][0]['user'].update(exec={'command': 'credential-helper'}),
            lambda c: c['users'][0]['user'].update({'client-certificate-data': 'admin'}),
            lambda c: c['contexts'][0]['context'].update(user='administrator'),
            lambda c: c.update({'current-context': 'another'}),
            lambda c: c['users'].append(c['users'][0])]
        for change in changes:
            self.config = copy.deepcopy(original); change(self.config)
            with patch('transport.http.client.HTTPSConnection') as connection:
                with self.assertRaises(QualificationError): self.make()
                connection.assert_not_called()

    def test_header_injection_and_unverified_tls_stay_rejected(self):
        self.config['users'][0]['user']['token'] = 'token\r\nInjected: header'
        with self.assertRaises(QualificationError): self.make()
        self.config['users'][0]['user']['token'] = 'token'
        self.tls = ssl._create_unverified_context()
        with self.assertRaises(QualificationError): self.make()

    def test_non_provider_endpoints_and_nonstandard_ports_are_rejected(self):
        for value in ['https://localhost', 'https://example.com', 'https://127.0.0.1',
            'http://owned.eks.amazonaws.com', 'https://user@owned.eks.amazonaws.com',
            self.server+'/', self.server+'?', self.server+'#', 'https://.eks.amazonaws.com', self.server+'?query=1', self.server+'#fragment', self.server+':6443',
            self.server+'.attacker.invalid']:
            with self.subTest(endpoint=value), self.assertRaises(QualificationError): endpoint(value)
        self.assertEqual(endpoint('https://owned.eks.us-east-1.api.aws'), ('owned.eks.us-east-1.api.aws', 443))

    def test_invalid_ca_is_rejected_by_real_tls_constructor(self):
        with self.assertRaises(QualificationError): EKSTransport(self.binding, self.config)

    def test_routes_and_response_bounds_are_not_relaxed_for_eks(self):
        client = self.make()
        with patch('transport.http.client.HTTPSConnection') as connection:
            with self.assertRaises(QualificationError): client.call('GET', '/api/v1/secrets')
            connection.assert_not_called()
        connection = Connection(Response(b'{}'+b' '*65535))
        with patch('transport.http.client.HTTPSConnection', return_value=connection):
            with self.assertRaises(QualificationError): client.call('GET', resource_path('kml-isolation-a'))
        self.assertTrue(connection.closed)


if __name__ == '__main__': unittest.main()
