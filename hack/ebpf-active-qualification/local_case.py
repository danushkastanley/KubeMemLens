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
from bound_case import BoundCase


class TraceClient:
    def __init__(self, runtime, namespace, *, service_account='tenant'):
        if not re.fullmatch(r'kml-active-[a-z0-9-]{1,40}', namespace):
            raise ValueError('unowned trace namespace')
        if service_account not in ('tenant', 'limit-probe'):
            raise ValueError('unowned trace service account')
        self.runtime, self.namespace = runtime, namespace
        cluster = runtime.api_cluster()
        self.url = urllib.parse.urlsplit(cluster['server'])
        self.ca = base64.b64decode(cluster['certificate-authority-data']).decode()
        self.tls = ssl.create_default_context(cadata=self.ca)
        self.token = command(runtime.kube + ['-n', namespace, 'create', 'token', service_account, '--duration=1h']).decode().strip()
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


class LocalCase(BoundCase):
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
        super().__init__(configuration, self.runtime)
