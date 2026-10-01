"""Fixed, restricted fixtures for owned isolation campaigns."""
import re

from transport import QualificationError, fixture_name

LABEL = 'kube-memlens.io/trace-isolation'


def labels(run_id):
    if not isinstance(run_id, str) or not re.fullmatch(r'[a-f0-9]{32}', run_id):
        raise QualificationError('explicit isolation run identity required')
    return {LABEL: run_id}


def namespace(name, run_id):
    if not fixture_name(name) or not name.startswith('kml-isolation-'):
        raise QualificationError('owned isolation namespace required')
    return {'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': name,
            'labels': {**labels(run_id), 'pod-security.kubernetes.io/enforce': 'restricted'}}}


def service_account(namespace_name, name, run_id):
    namespace(namespace_name, run_id)
    if not fixture_name(name):
        raise QualificationError('invalid fixture account')
    return {'apiVersion': 'v1', 'kind': 'ServiceAccount', 'metadata': {
            'name': name, 'namespace': namespace_name, 'labels': labels(run_id)},
            'automountServiceAccountToken': False}


def role_binding(namespace_name, name, operator_role, account_namespace, account, run_id):
    namespace(namespace_name, run_id)
    namespace(account_namespace, run_id)
    if not all(fixture_name(value) for value in [name, operator_role, account]):
        raise QualificationError('invalid fixture grant')
    return {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'RoleBinding',
            'metadata': {'name': name, 'namespace': namespace_name, 'labels': labels(run_id)},
            'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'ClusterRole', 'name': operator_role},
            'subjects': [{'kind': 'ServiceAccount', 'namespace': account_namespace, 'name': account}]}


def pod(namespace_name, name, node, image, run_id):
    namespace(namespace_name, run_id)
    if (not fixture_name(name) or not node_name(node) or not isinstance(image, str) or
            not re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}', image)):
        raise QualificationError('pinned isolation fixture required')
    return {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': name, 'namespace': namespace_name,
            'labels': labels(run_id)}, 'spec': {'nodeName': node, 'automountServiceAccountToken': False,
            'restartPolicy': 'Never', 'terminationGracePeriodSeconds': 1, 'activeDeadlineSeconds': 1800,
            'securityContext': {'runAsNonRoot': True, 'runAsUser': 65532, 'runAsGroup': 65532,
                                'fsGroup': 65532, 'seccompProfile': {'type': 'RuntimeDefault'}},
            'volumes': [{'name': 'work', 'emptyDir': {'sizeLimit': '128Mi'}}],
            'containers': [{'name': 'worker', 'image': image, 'imagePullPolicy': 'Never',
                            'command': ['/usr/local/bin/kml-io-workload', 'idle'],
                            'volumeMounts': [{'name': 'work', 'mountPath': '/work'}],
                            'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                                                'capabilities': {'drop': ['ALL']}},
                            'resources': {'requests': {'cpu': '5m', 'memory': '16Mi'},
                                          'limits': {'cpu': '500m', 'memory': '64Mi'}}}]}}


def node_name(value):
    return isinstance(value, str) and len(value) <= 253 and all(fixture_name(part) for part in value.split('.'))


def job(namespace_name, name, node, image, run_id):
    template = pod(namespace_name, name, node, image, run_id)
    # Provider nodes pull an explicitly pinned private fixture image. Local Pod
    # fixtures retain their existing preloaded-image-only behaviour.
    template['spec']['containers'][0]['imagePullPolicy'] = 'IfNotPresent'
    return {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': template['metadata'],
            'spec': {'parallelism': 1, 'completions': 1, 'backoffLimit': 0, 'activeDeadlineSeconds': 1800,
                     'template': {'metadata': {'labels': labels(run_id)}, 'spec': template['spec']}}}
