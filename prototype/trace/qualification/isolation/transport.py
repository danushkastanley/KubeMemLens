"""Bounded local HTTPS observations; private response bodies never enter repr."""
from dataclasses import dataclass
import http.client
import ipaddress
import json
import re
import ssl
import time
import urllib.parse


class QualificationError(RuntimeError):
    """Fixed diagnostic categories only; no response, token or identity text."""


def strict_object(data):
    def pairs(items):
        value = {}
        for key, item in items:
            if key in value:
                raise ValueError('duplicate member')
            value[key] = item
        return value

    def constant(_):
        raise ValueError('non-JSON number')

    try:
        value = json.loads(data, object_pairs_hook=pairs, parse_constant=constant)
    except (ValueError, UnicodeError, RecursionError):
        raise QualificationError('invalid JSON response') from None
    if not isinstance(value, dict):
        raise QualificationError('response must be an object')
    return value


def local_endpoint(value):
    try:
        endpoint = urllib.parse.urlsplit(value)
        host, port = endpoint.hostname, endpoint.port or 443
        local = host == 'localhost' or ipaddress.ip_address(host).is_loopback
    except (TypeError, ValueError):
        raise QualificationError('local HTTPS endpoint required') from None
    if (endpoint.scheme != 'https' or not local or endpoint.username is not None or
            endpoint.password is not None or endpoint.path not in ('', '/') or
            endpoint.query or endpoint.fragment or not 1 <= port <= 65535):
        raise QualificationError('local HTTPS endpoint required')
    return host, port


def fixture_name(value):
    return isinstance(value, str) and re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', value)


def pod_path(namespace, pod):
    if not fixture_name(namespace) or not fixture_name(pod):
        raise QualificationError('invalid fixture name')
    return '/api/v1/namespaces/'+namespace+'/pods/'+pod


def resource_path(namespace, resource='traces', name=None, stream=False):
    if not fixture_name(namespace):
        raise QualificationError('invalid fixture namespace')
    if resource not in ('traces', 'tracepreflights'):
        raise QualificationError('unsupported qualification resource')
    path = '/apis/tracing.kubememlens.io/v1alpha1/namespaces/'+namespace+'/'+resource
    if name is not None:
        if resource != 'traces' or not isinstance(name, str) or not re.fullmatch(r'[a-f0-9]{32}', name):
            raise QualificationError('invalid admission reference')
        path += '/'+name
    if stream:
        if name is None:
            raise QualificationError('stream requires an admission')
        path += '/stream'
    return path


@dataclass(frozen=True, repr=False)
class Observation:
    status: int
    elapsed_nanos: int
    size: int
    body: dict

    def __repr__(self):
        return '[private qualification HTTP observation]'

    def public(self):
        return {'status': self.status, 'elapsedNanos': self.elapsed_nanos, 'responseBytes': self.size}


class Transport:
    def __init__(self, endpoint, tls, token):
        self.host, self.port = local_endpoint(endpoint)
        if not tls.check_hostname or tls.verify_mode != ssl.CERT_REQUIRED:
            raise QualificationError('verified TLS required')
        if (not isinstance(token, str) or not 1 <= len(token) <= 16384 or
                any(ord(c) < 33 or ord(c) > 126 for c in token)):
            raise QualificationError('valid fixture credential required')
        self.tls, self.token = tls, token

    def __repr__(self):
        return '[private qualification transport]'

    def call(self, method, path, body=None):
        if method not in ('GET', 'POST', 'DELETE') or not isinstance(path, str) or any(c in path for c in '?%#\r\n'):
            raise QualificationError('unsupported qualification request')
        parts = path.split('/')
        if path.startswith('/api/v1/namespaces/') and method == 'GET' and len(parts) == 7 and parts[5] == 'pods':
            expected = pod_path(parts[4], parts[6])
        elif path.startswith('/apis/tracing.kubememlens.io/v1alpha1/namespaces/') and len(parts) in (7, 8, 9):
            expected = resource_path(parts[5], parts[6], parts[7] if len(parts) >= 8 else None, len(parts) == 9)
        else:
            raise QualificationError('unsupported qualification request')
        if expected != path:
            raise QualificationError('unsupported qualification request')
        encoded = None
        if body is not None:
            try:
                encoded = json.dumps(body, allow_nan=False, separators=(',', ':')).encode()
            except (TypeError, ValueError):
                raise QualificationError('invalid qualification request body') from None
            if method != 'POST' or len(encoded) > 4096:
                raise QualificationError('qualification request exceeds bounds')
        headers = {'Authorization': 'Bearer '+self.token, 'Accept': 'application/json', 'Cache-Control': 'no-store'}
        if encoded is not None:
            headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPSConnection(self.host, self.port, context=self.tls, timeout=5)
        started = time.monotonic_ns()
        try:
            connection.request(method, path, encoded, headers)
            response = connection.getresponse()
            # Stream probes expect another owner's denial. Reject a successful
            # stream immediately; the owning case must still verify cleanup.
            if response.getheader('Content-Type', '').split(';')[0] != 'application/json':
                raise QualificationError('unexpected control response type')
            data = response.read(65537)
            if len(data) > 65536:
                raise QualificationError('qualification response exceeds bounds')
            value = strict_object(data)
            return Observation(response.status, time.monotonic_ns()-started, len(data), value)
        except (OSError, http.client.HTTPException):
            raise QualificationError('qualification transport failed') from None
        finally:
            connection.close()
