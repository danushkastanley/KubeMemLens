"""Adversarial access cases against real aggregation, using supplied fixture tokens."""
import json
import re
from datetime import datetime, timezone

from transport import QualificationError, fixture_name, pod_path, resource_path


STATUS_KEYS = {'kind', 'apiVersion', 'metadata', 'status', 'message', 'reason', 'details', 'code'}


def denial(observation, expected, protected=()):
    value = observation.body
    reasons = {403: 'Forbidden', 404: 'NotFound'}
    if (observation.status != expected or expected not in reasons or
            value.get('kind') != 'Status' or value.get('apiVersion') != 'v1' or
            value.get('status') != 'Failure' or type(value.get('code')) is not int or
            value['code'] != expected or value.get('reason') != reasons[expected] or
            set(value)-STATUS_KEYS or value.get('metadata', {}) not in ({}, None)):
        raise QualificationError('unexpected denial contract')
    encoded = json.dumps(value, sort_keys=True)
    if any(item and item in encoded for item in protected):
        raise QualificationError('denial exposed protected fixture data')
    details = value.get('details')
    if details is not None and (not isinstance(details, dict) or details.get('uid')):
        raise QualificationError('denial exposed an object identity')
    return reasons[expected]


def admission_id(observation, namespace, engine, expected_status=201, expected_state='admitted'):
    value = observation.body
    metadata = value.get('metadata', {})
    if (observation.status != expected_status or value.get('kind') != 'TraceAdmission' or
            value.get('apiVersion') != 'tracing.kubememlens.io/v1alpha1' or
            value.get('state') != expected_state or value.get('engineDigest') != engine or
            not isinstance(metadata, dict) or set(metadata) != {'name', 'namespace'} or
            metadata.get('namespace') != namespace or not isinstance(metadata.get('name'), str) or
            not re.fullmatch(r'[a-f0-9]{32}', metadata['name']) or
            set(value) != {'apiVersion', 'kind', 'metadata', 'state', 'expiresAt', 'engineDigest'}):
        raise QualificationError('unexpected admission contract')
    try:
        expiry = datetime.fromisoformat(value['expiresAt'].replace('Z', '+00:00'))
    except (TypeError, ValueError, AttributeError):
        raise QualificationError('invalid admission expiry') from None
    if expiry.tzinfo is None or expiry == datetime(1, 1, 1, tzinfo=timezone.utc):
        raise QualificationError('invalid admission expiry')
    return metadata['name']


def intent(pod='target'):
    return {'schemaVersion': 1, 'pod': pod, 'container': 'worker', 'kind': 'files',
            'rawPaths': False, 'durationSeconds': 30, 'maxEvents': 10000,
            'maxOutputBytes': 8388608, 'maxMapBytes': 8388608, 'maxPathBytes': 64}


class AccessCases:
    def __init__(self, actors, namespaces, engine, protected, *, pods=None):
        if set(actors) != {'a', 'b', 'colleague', 'admin', 'none'} or set(namespaces) != {'a', 'b'}:
            raise QualificationError('complete actor and tenant fixtures required')
        if namespaces['a'] == namespaces['b']:
            raise QualificationError('two distinct tenant namespaces required')
        if not isinstance(engine, str) or not re.fullmatch(r'sha256:[a-f0-9]{64}', engine):
            raise QualificationError('pinned engine digest required')
        self.actors, self.namespaces, self.engine = actors, namespaces, engine
        self.pods = dict(pods) if pods is not None else {'a': 'target', 'b': 'target'}
        if set(self.pods) != {'a', 'b'} or any(not fixture_name(p) or p == 'absent-fixture' for p in self.pods.values()):
            raise QualificationError('two exact existing fixture Pod names required')
        self.protected = tuple(protected)
        self.checks, self.pending, self.expiries = [], [], {}

    def __repr__(self):
        return '[private qualification access cases]'

    def reject(self, name, actor, method, path, expected, body=None):
        observation = self.actors[actor].call(method, path, body)
        reason = denial(observation, expected, self.protected)
        self.checks.append({'case': name, 'reason': reason, **observation.public()})
        return observation

    def create(self, actor, tenant):
        namespace = self.namespaces[tenant]
        observation = self.actors[actor].call('POST', resource_path(namespace), intent(self.pods[tenant]))
        metadata = observation.body.get('metadata')
        candidate = metadata.get('name') if isinstance(metadata, dict) else None
        # A malformed success may still have created a reservation. Keep the
        # known handle on the requested namespace for compensating cancellation.
        if observation.status == 201 and isinstance(candidate, str) and re.fullmatch(r'[a-f0-9]{32}', candidate):
            self.pending.append((actor, namespace, candidate))
        name = admission_id(observation, namespace, self.engine)
        self.expiries[name] = observation.body['expiresAt']
        self.checks.append({'case': actor+'-own-create', **observation.public()})
        self.owner_read(actor, namespace, name)
        return name

    def owner_read(self, actor, namespace, name):
        observation = self.actors[actor].call('GET', resource_path(namespace, name=name))
        if (admission_id(observation, namespace, self.engine, 200) != name or
                observation.body['expiresAt'] != self.expiries[name]):
            raise QualificationError('owner admission changed during access probes')
        self.checks.append({'case': actor+'-owner-still-admitted', **observation.public()})

    def cancel(self, actor, namespace, name):
        observation = self.actors[actor].call('DELETE', resource_path(namespace, name=name))
        value = observation.body
        if (observation.status != 200 or value.get('status') != 'Success' or
                value.get('kind') != 'Status' or value.get('apiVersion') != 'v1' or
                type(value.get('code')) is not int or value['code'] != 200):
            raise QualificationError('owned admission cancellation unconfirmed')
        self.pending.remove((actor, namespace, name))
        self.checks.append({'case': actor+'-own-cancel', **observation.public()})

    def active_probes(self, owner, tenant, name):
        namespace = self.namespaces[tenant]
        path = resource_path(namespace, name=name)
        before = self.actors[owner].call('GET', path)
        if admission_id(before, namespace, self.engine, 200, 'active') != name:
            raise QualificationError('active owner handle changed')
        rows = []
        for actor, expected in self.peers(owner, tenant):
            for method, stream, action in [('GET', False, 'read'), ('GET', True, 'watch'), ('DELETE', False, 'cancel')]:
                observation = self.reject('active-'+actor+'-'+action, actor, method,
                                          resource_path(namespace, name=name, stream=stream), expected)
                rows.append({'actor': actor, 'operation': action, **observation.public()})
        after = self.actors[owner].call('GET', path)
        if (admission_id(after, namespace, self.engine, 200, 'active') != name or
                before.body['expiresAt'] != after.body['expiresAt']):
            raise QualificationError('denied caller influenced active owner state')
        return {'checks': rows, 'ownerStillActive': True, 'deadlineUnchanged': True}

    @staticmethod
    def peers(owner, tenant):
        result = [('b' if tenant == 'a' else 'a', 403), ('none', 403)]
        if owner != 'admin':
            result.append(('admin', 404))
        if tenant == 'a':
            result.append(('colleague', 404))
        if owner == 'admin':
            result.append(('a', 404))
        return result

    def verify(self):
        try:
            for actor, peer in [('a', 'b'), ('b', 'a')]:
                namespace = self.namespaces[peer]
                for pod in [self.pods[peer], 'absent-fixture']:
                    self.reject(actor+'-peer-pod-'+pod, actor, 'GET', pod_path(namespace, pod), 403)
                    for resource in ['tracepreflights', 'traces']:
                        self.reject(actor+'-peer-'+resource+'-'+pod, actor, 'POST', resource_path(namespace, resource), 403, intent(pod))
            for resource in ['tracepreflights', 'traces']:
                self.reject('unbound-'+resource, 'none', 'POST', resource_path(self.namespaces['a'], resource), 403, intent(self.pods['a']))
            for owner, tenant in [('a', 'a'), ('b', 'b'), ('admin', 'a')]:
                namespace = self.namespaces[tenant]
                name = self.create(owner, tenant)
                missing = ('0' if name[0] != '0' else '1')+name[1:]
                for actor, expected in self.peers(owner, tenant):
                    for reference, suffix in [(name, 'existing'), (missing, 'absent')]:
                        for method, stream, action in [('GET', False, 'read'), ('GET', True, 'watch'), ('DELETE', False, 'cancel')]:
                            path = resource_path(namespace, name=reference, stream=stream)
                            self.reject(owner+'-'+actor+'-'+action+'-'+suffix, actor, method, path, expected)
                self.owner_read(owner, namespace, name)
                self.cancel(owner, namespace, name)
                self.reject(owner+'-deleted-absent', owner, 'GET', resource_path(namespace, name=name), 404)
            return {'scope': 'trace access controls', 'checks': self.checks,
                    'rawResponsesRetained': False, 'kernelCleanupVerified': False}
        finally:
            failures = []
            for actor, namespace, name in list(self.pending):
                try:
                    self.cancel(actor, namespace, name)
                except QualificationError:
                    failures.append(True)
            if failures:
                raise QualificationError('access case cleanup unconfirmed')
