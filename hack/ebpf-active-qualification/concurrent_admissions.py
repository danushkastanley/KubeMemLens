"""Track two selected admissions and a separately authorised capacity probe."""
from admissions import Admissions
from local_case import TraceClient


class ConcurrentAdmissions(Admissions):
    def __init__(self, runtime, namespaces):
        if len(namespaces) != 2:
            raise ValueError('two selected namespaces required')
        super().__init__(runtime, namespaces)
        self.clients.append(TraceClient(runtime, namespaces[0], service_account='limit-probe'))

    def active_pair(self):
        if set(self.pending) != {0, 1}:
            raise ValueError('exactly two owned admissions required before capacity probe')
        for index in (0, 1):
            status, admission = self.get(index)
            if (status != 200 or not isinstance(admission, dict)
                    or admission.get('state') != 'active'
                    or not isinstance(admission.get('metadata'), dict)
                    or admission['metadata'].get('name') != self.pending[index][1:]
                    or admission.get('engineDigest') != self.engine):
                raise ValueError('both owned admission lifetimes must remain active')

    def probe_capacity(self, intent):
        self.active_pair()
        status, response = self.client(2).call('POST', value=intent)
        if status == 201:
            if not isinstance(response, dict):
                raise ValueError('unexpected admission has no identity; source teardown required')
            # Even an invalid candidate response may contain an owned session.
            # Keep it so cancellation attempts include all three clients.
            self.retain(2, response)
            raise ValueError('third admission unexpectedly accepted; source teardown required')
        if (status != 429 or not isinstance(response, dict)
                or response.get('apiVersion') != 'v1' or response.get('kind') != 'Status'
                or response.get('status') != 'Failure' or response.get('reason') != 'TooManyRequests'
                or type(response.get('code')) is not int or response['code'] != 429):
            raise ValueError('third request did not return the capacity error contract')
        self.active_pair()
        return {'thirdRequestCapacityDenied': True, 'bothAdmissionsActiveBeforeAndAfter': True,
                'scope': 'API response only; frozen policy and two-target attachment evidence required'}


def probe_resources(namespace, owner):
    """Grant the probe the existing fixture role without widening that role."""
    labels = {'kube-memlens.io/fixture': owner}
    metadata = {'name': 'limit-probe', 'namespace': namespace, 'labels': labels}
    return [
        {'apiVersion': 'v1', 'kind': 'ServiceAccount', 'metadata': metadata,
         'automountServiceAccountToken': False},
        {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'RoleBinding', 'metadata': metadata,
         'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'Role', 'name': 'trace-fixture'},
         'subjects': [{'kind': 'ServiceAccount', 'name': 'limit-probe', 'namespace': namespace}]},
    ]
