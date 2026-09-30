"""Create and remove only the explicit local campaign's standard stack and Pods."""
import json
import hashlib
from pathlib import Path

from local_case import canonical, command
from density import mapped_fixtures

ROOT = Path(__file__).resolve().parents[2]


class Fixtures:
    def __init__(self, case, record):
        self.case, self.cfg, self.record = case, case.cfg, record
        self.namespaces = {}
        self.bindings = {}
        self.helm_started = False
        self.cleaned = False
        self.chart_objects = []
        self.reader_binding = 'kml-active-metrics-' + self.cfg['owner'][-12:]
        self.reader_uid = None

    def create(self, obj):
        return json.loads(self.case.kube(['create', '-f', '-', '-o', 'json'], canonical(obj)))

    def namespace(self, name, restricted):
        if self.case.kube(['get', 'namespace', name, '--ignore-not-found', '-o', 'name']).strip():
            raise ValueError('campaign namespace already exists')
        obj = self.create({'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': name, 'labels': {
            'kube-memlens.io/fixture': self.cfg['owner'], 'pod-security.kubernetes.io/enforce': 'restricted' if restricted else 'privileged'}}})
        self.namespaces[name] = obj['metadata']['uid']
        self.record('created-namespace', {'name': name, 'uid': obj['metadata']['uid']}, private=True)

    def prepare(self, profile):
        c, cfg = self.case, self.cfg
        if c.kube(['get', 'apiservice', 'v1alpha1.memory.kubememlens.io', '--ignore-not-found', '-o', 'name']).strip():
            raise ValueError('standard API already installed')
        if c.kube(['get', 'clusterrolebinding', self.reader_binding, '--ignore-not-found', '-o', 'name']).strip():
            raise ValueError('reader grant already exists')
        for ns in c.namespaces:
            self.namespace(ns, True)
        self.namespace(cfg['standardNamespace'], False)
        values = {'namespace': {'name': cfg['standardNamespace'], 'create': False},
                  'image': {'repository': cfg['standardImage'].split('@')[0],
                            'digest': cfg['standardImage'].split('@')[1], 'pullPolicy': 'Never'}}
        rendered = command(['helm', 'template', cfg['release'], str(ROOT / 'charts/kube-memlens'),
                            '-n', cfg['standardNamespace'], '-f', '-'], canonical(values))
        objects = json.loads(command(['go', '-C', str(ROOT), 'run', './hack/node-qualification/chart-inventory'], rendered, timeout=120))
        self.chart_objects = objects
        for obj in objects:
            metadata = obj['metadata']
            args = ['get', obj['kind'], metadata['name'], '--ignore-not-found', '-o', 'name']
            if metadata.get('namespace'):
                args += ['-n', metadata['namespace']]
            if c.kube(args).strip():
                raise ValueError('rendered chart resource already exists; do not adopt it')
        self.record('rendered-standard-chart', {'sha256': hashlib.sha256(rendered).hexdigest()}, private=True)
        self.helm_started = True
        command(['helm', 'install', cfg['release'], str(ROOT / 'charts/kube-memlens'),
                 '--kubeconfig', c.runtime.cfg['kubeconfig'], '--kube-context', c.runtime.cfg['context'],
                 '-n', cfg['standardNamespace'], '-f', '-', '--wait', '--timeout', '180s'], canonical(values), timeout=195)
        c.kube(['wait', '--for=condition=Available', 'apiservice/v1alpha1.memory.kubememlens.io', '--timeout=90s'], timeout=95)
        role = c.json(['get', 'clusterrole', 'kube-memlens-metrics-reader', '-o', 'json'])
        if role['rules'] != [{'apiGroups': ['memory.kubememlens.io'], 'resources': ['metrics'], 'verbs': ['get']}]:
            raise ValueError('metrics reader permissions changed')
        self.create({'apiVersion': 'v1', 'kind': 'ServiceAccount', 'metadata': {'name': 'observer', 'namespace': cfg['standardNamespace']}, 'automountServiceAccountToken': False})
        obj = self.create({'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRoleBinding',
                           'metadata': {'name': self.reader_binding, 'labels': {'kube-memlens.io/fixture': cfg['owner']}},
                           'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'ClusterRole', 'name': 'kube-memlens-metrics-reader'},
                           'subjects': [{'kind': 'ServiceAccount', 'name': 'observer', 'namespace': cfg['standardNamespace']}]})
        self.reader_uid = obj['metadata']['uid']
        for ns in c.namespaces:
            self.tenant(ns)
        roster = [(c.namespaces[0], 'target'), (c.namespaces[1], 'target')]
        roster += [(c.namespaces[0], f'passive-{i:02}') for i in range(profile['workloadContainers'] - 2)]
        for ns, name in roster:
            self.create(self.pod(ns, name))
        for ns in c.namespaces:
            c.kube(['-n', ns, 'wait', 'pod', '-l', 'kube-memlens.io/fixture=' + cfg['owner'], '--for=condition=Ready', '--timeout=90s'], timeout=95)
        for ns, name in roster:
            value = c.fixture(ns, name)
            c.runtime.exec(['sh', '-ec', 'test "$(stat -c %i "$1")" = "$2"; printf 32 > "$1/pids.max"', '--', value['group']['path'], str(value['group']['inode'])])
            self.bindings[ns + '/' + name] = value
        for ns in c.namespaces:
            data = c.kube(['-n', ns, 'exec', 'target', '-c', 'worker', '--', '/usr/local/bin/kml-io-workload', 'prepare'])
            if json.loads(data)['writeBytes'] != profile['workload']['fileBytes']:
                raise ValueError('fixture byte inventory changed')
        self.record('fixture-bindings', self.bindings, private=True)
        self.verify()

    def tenant(self, ns):
        labels = {'kube-memlens.io/fixture': self.cfg['owner']}
        resources = [
            {'apiVersion': 'v1', 'kind': 'ServiceAccount', 'metadata': {'name': 'tenant', 'namespace': ns, 'labels': labels}, 'automountServiceAccountToken': False},
            {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'Role', 'metadata': {'name': 'trace-fixture', 'namespace': ns, 'labels': labels}, 'rules': [
                {'apiGroups': [''], 'resources': ['pods'], 'resourceNames': ['target'], 'verbs': ['get']},
                {'apiGroups': ['tracing.kubememlens.io'], 'resources': ['traces'], 'verbs': ['create', 'get', 'delete']},
                {'apiGroups': ['tracing.kubememlens.io'], 'resources': ['traces/stream'], 'verbs': ['get']}]},
            {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'RoleBinding', 'metadata': {'name': 'trace-fixture', 'namespace': ns, 'labels': labels},
             'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'Role', 'name': 'trace-fixture'},
             'subjects': [{'kind': 'ServiceAccount', 'name': 'tenant', 'namespace': ns}]}]
        for obj in resources:
            self.create(obj)

    def pod(self, namespace, name):
        return {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': name, 'namespace': namespace,
                'labels': {'kube-memlens.io/fixture': self.cfg['owner']}}, 'spec': {
            'nodeName': self.case.node, 'automountServiceAccountToken': False, 'restartPolicy': 'Never',
            'activeDeadlineSeconds': 3600,
            'securityContext': {'runAsNonRoot': True, 'runAsUser': 65532, 'runAsGroup': 65532, 'fsGroup': 65532, 'seccompProfile': {'type': 'RuntimeDefault'}},
            'containers': [{'name': 'worker', 'image': self.cfg['fixtureImage'], 'imagePullPolicy': 'Never',
                            'command': ['/usr/local/bin/kml-io-workload', 'idle'],
                            'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True, 'capabilities': {'drop': ['ALL']}},
                            'resources': {'requests': {'cpu': '5m', 'memory': '16Mi'}, 'limits': {'cpu': '1', 'memory': '64Mi'}},
                            'volumeMounts': [{'name': 'work', 'mountPath': '/work'}]}],
            'volumes': [{'name': 'work', 'emptyDir': {'sizeLimit': '128Mi'}}]}}

    def verify(self):
        for key, previous in self.bindings.items():
            ns, name = key.split('/')
            if self.case.fixture(ns, name) != previous:
                raise ValueError('fixture lifetime/specification changed')
        for ns, uid in self.namespaces.items():
            if self.case.namespace(ns)['metadata']['uid'] != uid:
                raise ValueError('namespace replaced')

    def mapping(self):
        result = []
        for ns in self.case.namespaces:
            path = '/apis/memory.kubememlens.io/v1alpha1/namespaces/' + ns + '/containers?limit=100'
            document = json.loads(self.case.kube(['get', '--raw', path]))
            result.append(mapped_fixtures(document, ns, self.bindings, self.case.node))
        return result

    def cleanup(self):
        # Independent owned cleanup steps still run if an earlier step fails.
        if self.cleaned:
            return
        failures = []
        if self.reader_uid:
            try:
                raw = self.case.kube(['get', 'clusterrolebinding', self.reader_binding, '--ignore-not-found', '-o', 'json'])
                obj = json.loads(raw) if raw.strip() else None
                if obj is None:
                    self.reader_uid = None
                elif obj['metadata']['uid'] != self.reader_uid or obj['metadata'].get('labels', {}).get('kube-memlens.io/fixture') != self.cfg['owner']:
                    raise ValueError('reader binding ownership changed')
                if obj is not None:
                    body = {'apiVersion': 'v1', 'kind': 'DeleteOptions', 'preconditions': {'uid': self.reader_uid, 'resourceVersion': obj['metadata']['resourceVersion']}}
                    self.case.kube(['delete', '--raw', '/apis/rbac.authorization.k8s.io/v1/clusterrolebindings/' + self.reader_binding, '-f', '-'], canonical(body))
            except Exception:
                failures.append('reader grant')
        if self.helm_started:
            try:
                ns = self.cfg['standardNamespace']
                if self.case.namespace(ns)['metadata']['uid'] != self.namespaces[ns]:
                    raise ValueError('standard namespace replaced')
                releases = json.loads(command(['helm', 'list', '--all', '-o', 'json', '--kubeconfig', self.case.runtime.cfg['kubeconfig'], '--kube-context', self.case.runtime.cfg['context'], '-n', ns]))
                if any(release['name'] == self.cfg['release'] for release in releases):
                    command(['helm', 'uninstall', self.cfg['release'], '--kubeconfig', self.case.runtime.cfg['kubeconfig'], '--kube-context', self.case.runtime.cfg['context'], '-n', ns, '--wait', '--timeout', '90s'], timeout=100)
                for resource in self.chart_objects:
                    metadata = resource['metadata']
                    args = ['get', resource['kind'], metadata['name'], '--ignore-not-found', '-o', 'name']
                    if metadata.get('namespace'):
                        args += ['-n', metadata['namespace']]
                    if self.case.kube(args).strip():
                        raise ValueError('chart resource remains; retain namespace and release state')
                self.helm_started = False
            except Exception:
                failures.append('standard release')
        for ns, uid in self.namespaces.items():
            if ns == self.cfg['standardNamespace'] and self.helm_started:
                continue
            try:
                raw = self.case.kube(['get', 'namespace', ns, '--ignore-not-found', '-o', 'json'])
                if not raw.strip():
                    continue
                obj = self.case.namespace(ns)
                if obj['metadata']['uid'] != uid:
                    raise ValueError('namespace replaced')
                body = {'apiVersion': 'v1', 'kind': 'DeleteOptions', 'preconditions': {'uid': uid, 'resourceVersion': obj['metadata']['resourceVersion']}}
                self.case.kube(['delete', '--raw', '/api/v1/namespaces/' + ns, '-f', '-'], canonical(body))
                self.case.kube(['wait', '--for=delete', 'namespace/' + ns, '--timeout=90s'], timeout=95)
            except Exception:
                failures.append('owned namespace')
        self.record('cleanup', {'complete': not failures, 'failedSteps': failures, 'cloudActions': False})
        if failures:
            raise ValueError('owned campaign cleanup incomplete; retain evidence')
        self.cleaned = True
