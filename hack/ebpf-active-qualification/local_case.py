"""Bound owned local identities and trace requests; never print raw responses."""
import base64
import http.client
import json
import re
import ssl
import urllib.parse
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'ebpf-qualification'))
from local_runtime import CENSUS, Runtime, canonical, command, digest


class TraceClient:
    def __init__(self, runtime, namespace):
        if not re.fullmatch(r'kml-active-[a-z0-9-]{1,40}', namespace):
            raise ValueError('unowned trace namespace')
        self.runtime, self.namespace = runtime, namespace
        doc = runtime.json(['config', 'view', '--raw', '--minify', '-o', 'json'])
        cluster = doc['clusters'][0]['cluster']
        self.url = urllib.parse.urlsplit(cluster['server'])
        if self.url.scheme != 'https' or self.url.hostname not in ('127.0.0.1', 'localhost'):
            raise ValueError('local API endpoint required')
        self.ca = base64.b64decode(cluster['certificate-authority-data']).decode()
        self.tls = ssl.create_default_context(cadata=self.ca)
        self.token = command(runtime.kube + ['-n', namespace, 'create', 'token', 'tenant', '--duration=1h']).decode().strip()
        self.path = '/apis/tracing.kubememlens.io/v1alpha1/namespaces/' + namespace + '/traces'

    def __repr__(self):
        return '<private local trace client>'

    def call(self, method, suffix='', value=None):
        if method not in ('GET', 'POST', 'DELETE') or not re.fullmatch(r'(?:/[a-zA-Z0-9-]{1,128})?', suffix):
            raise ValueError('invalid owned trace operation')
        connection = http.client.HTTPSConnection(self.url.hostname, self.url.port, context=self.tls, timeout=5)
        headers = {'Authorization': 'Bearer ' + self.token}
        data = None
        if value is not None:
            headers['Content-Type'] = 'application/json'
            data = canonical(value)
        try:
            connection.request(method, self.path + suffix, data, headers)
            response = connection.getresponse()
            raw = response.read(65537)
            if len(raw) > 65536:
                raise ValueError('trace response exceeds bound')
            return response.status, json.loads(raw) if raw else None
        finally:
            connection.close()


class LocalCase:
    def __init__(self, configuration):
        self.cfg = configuration
        trace = configuration['trace']
        if not re.fullmatch(r'kind-[a-z0-9-]+', trace['context']):
            raise ValueError('local kind context required')
        exported = command(['kind', 'get', 'kubeconfig', '--name', trace['context'].removeprefix('kind-')])
        view = ['kubectl', 'config', 'view', '--raw', '--minify', '--context', trace['context'], '-o', 'json', '--kubeconfig']
        expected = json.loads(command(view + ['/dev/stdin'], exported))
        actual = json.loads(command(view + [trace['kubeconfig']]))
        if actual != expected:
            raise ValueError('kubeconfig does not match the local kind endpoint and identity')
        self.runtime = Runtime(trace)
        self.node = self.runtime.cfg['node']
        label = command(['docker', 'inspect', '--format', '{{index .Config.Labels "io.x-k8s.kind.cluster"}}', self.node]).decode().strip()
        node = self.runtime.json(['get', 'node', self.node, '-o', 'json'])
        boot = self.runtime.exec(['cat', '/proc/sys/kernel/random/boot_id']).decode().strip()
        if label != trace['context'].removeprefix('kind-') or node['status']['nodeInfo']['bootID'] != boot:
            raise ValueError('Docker and Kubernetes Node identities differ')
        self.namespaces = tuple(configuration['fixtureNamespaces'])
        if len(self.namespaces) != 2 or len(set(self.namespaces)) != 2 or any(
                not re.fullmatch(r'kml-active-[a-z0-9-]{1,40}', name) for name in self.namespaces):
            raise ValueError('exactly two owned fixture namespaces required')
        for field in ('standardNamespace', 'release'):
            if not re.fullmatch(r'kml-active-[a-z0-9-]{1,40}', configuration[field]):
                raise ValueError('invalid owned standard installation name')
        if configuration['standardNamespace'] in self.namespaces:
            raise ValueError('standard and workload namespaces must differ')
        for field in ('standardImage', 'fixtureImage'):
            if not re.fullmatch(r'[a-z0-9./:_-]+@sha256:[a-f0-9]{64}', configuration[field]):
                raise ValueError('immutable local image required')
        hashes = [configuration['fixtureSHA256'], *configuration['standardSHA256'].values()]
        if set(configuration['standardSHA256']) != {'agent', 'collector'} or any(not re.fullmatch(r'[a-f0-9]{64}', h) for h in hashes):
            raise ValueError('exact standard and fixture executable hashes required')
        if set(configuration['helpers']) != {'measure', 'watch', 'delivery', 'standard'}:
            raise ValueError('complete helper inventory required')
        for value in configuration['helpers'].values():
            if not re.fullmatch(r'/usr/local/bin/kml-[a-z0-9-]{1,120}', value['path']) or not re.fullmatch(r'[a-f0-9]{64}', value['sha256']):
                raise ValueError('invalid bound helper')
        self.owner = configuration['owner']
        if not re.fullmatch(r'[a-z0-9-]{1,63}', self.owner):
            raise ValueError('invalid fixture ownership label')

    def kube(self, args, data=None, timeout=15):
        return command(self.runtime.kube + args, data, timeout)

    def json(self, args):
        return json.loads(self.kube(args))

    def namespace(self, name):
        if name not in (*self.namespaces, self.cfg['standardNamespace']):
            raise ValueError('namespace outside campaign')
        obj = self.json(['get', 'namespace', name, '-o', 'json'])
        if obj['metadata'].get('labels', {}).get('kube-memlens.io/fixture') != self.owner:
            raise ValueError('namespace ownership changed')
        return obj

    def bind(self, pod, role, executable_hash):
        if pod['spec']['nodeName'] != self.node or pod['metadata'].get('deletionTimestamp'):
            raise ValueError('Pod placement or lifetime changed')
        statuses = pod['status'].get('containerStatuses', [])
        if len(statuses) != 1 or not statuses[0]['ready'] or statuses[0]['restartCount'] != 0 or 'running' not in statuses[0]['state']:
            raise ValueError('container not uniquely ready')
        cid = statuses[0]['containerID'].removeprefix('containerd://')
        if re.fullmatch('[a-f0-9]{64}', cid) is None:
            raise ValueError('full CRI identity missing')
        cri = json.loads(self.runtime.exec(['crictl', 'inspect', cid]))
        if cri['status']['id'] != cid or cri['status']['labels'].get('io.kubernetes.pod.uid') != pod['metadata']['uid']:
            raise ValueError('CRI ownership differs from Pod')
        pid = cri['info']['pid']
        if type(pid) is not int or pid <= 1:
            raise ValueError('invalid process identity')
        flags = ['--parent-pid', str(pid), '--parent-sha256', executable_hash, '--parent-container', cid]
        started = json.loads(self.runtime.exec([CENSUS, '--mode', 'identify', *flags]))['parentStart']
        path = self.runtime.exec(['cat', f'/proc/{pid}/cgroup']).decode().strip()
        if not path.startswith('0::/') or '\n' in path or '..' in path or not path.endswith('/cri-containerd-' + cid + '.scope'):
            raise ValueError('invalid container cgroup')
        path = '/sys/fs/cgroup' + path[3:]
        inode = int(self.runtime.exec(['stat', '-c', '%i', path]))
        value = {'pid': pid, 'start': started, 'container': cid, 'sha256': executable_hash,
                 'podUID': pod['metadata']['uid'], 'startedAt': statuses[0]['state']['running']['startedAt'],
                 'group': {'role': role, 'path': path, 'inode': inode},
                 'specSHA256': digest(canonical(pod['spec']))}
        value['identity'] = digest(canonical(value))
        return value

    def fixture(self, namespace, name, role='selected'):
        self.namespace(namespace)
        pod = self.json(['-n', namespace, 'get', 'pod', name, '-o', 'json'])
        c = pod['spec']['containers'][0]
        if (pod['metadata'].get('labels', {}).get('kube-memlens.io/fixture') != self.owner or
                c['image'] != self.cfg['fixtureImage'] or c['resources']['limits'] != {'cpu': '1', 'memory': '64Mi'} or
                pod['spec']['automountServiceAccountToken'] is not False):
            raise ValueError('fixture specification changed')
        return self.bind(pod, role, self.cfg['fixtureSHA256'])

    def standard(self, role):
        if role not in ('agent', 'collector'):
            raise ValueError('invalid standard role')
        ns = self.cfg['standardNamespace']
        self.namespace(ns)
        name = 'kube-memlens-' + role
        pods = self.json(['-n', ns, 'get', 'pods', '-l', 'app.kubernetes.io/name=' + name, '-o', 'json'])['items']
        pods = [p for p in pods if p['spec']['nodeName'] == self.node and not p['metadata'].get('deletionTimestamp')]
        if len(pods) != 1 or pods[0]['spec']['containers'][0]['image'] != self.cfg['standardImage']:
            raise ValueError('standard service image or instance changed')
        pod = pods[0]
        owners = [x for x in pod['metadata'].get('ownerReferences', []) if x.get('controller')]
        if len(owners) != 1:
            raise ValueError('standard controller unavailable')
        owner = owners[0]
        if role == 'agent':
            parent = self.json(['-n', ns, 'get', 'daemonset', name, '-o', 'json'])
            if owner['kind'] != 'DaemonSet' or owner['uid'] != parent['metadata']['uid']:
                raise ValueError('agent controller changed')
        else:
            parent = self.json(['-n', ns, 'get', 'deployment', name, '-o', 'json'])
            replica = self.json(['-n', ns, 'get', 'replicaset', owner['name'], '-o', 'json'])
            if owner['kind'] != 'ReplicaSet' or owner['uid'] != replica['metadata']['uid'] or parent['spec']['replicas'] != 1 or not any(
                    o['kind'] == 'Deployment' and o['uid'] == parent['metadata']['uid'] and o.get('controller') for o in replica['metadata']['ownerReferences']):
                raise ValueError('collector controller changed')
        if (parent['metadata'].get('annotations', {}).get('meta.helm.sh/release-name') != self.cfg['release'] or
                parent['metadata'].get('annotations', {}).get('meta.helm.sh/release-namespace') != ns):
            raise ValueError('standard controller not owned by campaign release')
        value = self.bind(pod, role, self.cfg['standardSHA256'][role])
        value['controllerUID'] = parent['metadata']['uid']
        value['controllerSpecSHA256'] = digest(canonical(parent['spec']))
        value['identity'] = digest(canonical({k: v for k, v in value.items() if k != 'identity'}))
        return value

    def node_owner(self):
        deployment = self.runtime.deployment('node')
        pods = self.runtime.pods('node')
        if len(pods) != 1 or deployment['spec']['replicas'] != 1:
            raise ValueError('ambiguous optional Node service')
        bound = self.bind(pods[0], 'node', self.runtime.cfg['nodeSHA256'])
        bound['flags'] = ['--parent-pid', str(bound['pid']), '--parent-sha256', bound['sha256'],
                          '--parent-start', str(bound['start']), '--parent-container', bound['container'],
                          '--worker-sha256', self.runtime.cfg['workerSHA256']]
        return bound

    def snapshot(self, owner, targets):
        return json.loads(self.runtime.exec([CENSUS, '--mode', 'snapshot', *owner['flags'], '--target-cgroups',
                                            ','.join(str(t['group']['inode']) for t in targets)]))

    def remaining(self, objects):
        return json.loads(self.runtime.exec([CENSUS, '--mode', 'check'], canonical(objects)))
