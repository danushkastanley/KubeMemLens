"""Bind benchmark workloads to real Job-created Pods without retry or adoption."""
from copy import deepcopy
import re
import time

from local_case import canonical, digest


def require(condition, message):
    if not condition:
        raise ValueError(message)


def job_from_pod(pod):
    require(pod.get('apiVersion') == 'v1' and pod.get('kind') == 'Pod', 'Pod template required')
    meta, spec = pod['metadata'], pod['spec']
    require(type(spec.get('activeDeadlineSeconds')) is int and 0 < spec['activeDeadlineSeconds'] <= 3600
            and spec.get('restartPolicy') == 'Never', 'bounded non-restarting fixture required')
    require(not meta.get('ownerReferences') and not meta.get('generateName'), 'controller must create its own child')
    return {'apiVersion': 'batch/v1', 'kind': 'Job',
            'metadata': deepcopy(meta), 'spec': {'parallelism': 1, 'completions': 1, 'backoffLimit': 0,
                'activeDeadlineSeconds': spec['activeDeadlineSeconds'],
                'template': {'metadata': {'labels': deepcopy(meta['labels'])}, 'spec': deepcopy(spec)}}}


def submitted_fields(actual, desired):
    if type(desired) is dict:
        return type(actual) is dict and all(key in actual and submitted_fields(actual[key], value)
                                           for key, value in desired.items())
    if type(desired) is list:
        return type(actual) is list and len(actual) == len(desired) and all(
            submitted_fields(a, b) for a, b in zip(actual, desired))
    return type(actual) is type(desired) and actual == desired


def created_specification(parent, requested):
    require(submitted_fields(parent['spec'], requested['spec']), 'admission changed requested fixture bounds')
    actual, expected = (value['spec']['template']['spec'] for value in (parent, requested))
    require(all(not actual.get(key, False) for key in ('hostPID', 'hostIPC', 'hostNetwork'))
            and not actual.get('initContainers') and not actual.get('ephemeralContainers'),
            'admission added host access or fixture containers')
    require(actual['securityContext'] == expected['securityContext'] and actual['volumes'] == expected['volumes'],
            'admission changed fixture security or storage')
    for got, wanted in zip(actual['containers'], expected['containers']):
        require(all(got[key] == wanted[key] for key in ('securityContext', 'resources', 'volumeMounts')),
                'admission changed container privilege, limits or mounts')


def selector(parent):
    value = parent['spec'].get('selector', {})
    labels = value.get('matchLabels')
    require(set(value) == {'matchLabels'} and type(labels) is dict and 1 <= len(labels) <= 4,
            'bounded Job selector required')
    for key, item in labels.items():
        require(type(key) is str and re.fullmatch(r'[A-Za-z0-9./_-]{1,317}', key)
                and type(item) is str and re.fullmatch(r'[A-Za-z0-9_.-]{1,63}', item), 'invalid Job selector')
    require(labels.get('batch.kubernetes.io/controller-uid', labels.get('controller-uid')) == parent['metadata']['uid'],
            'Job selector does not bind its controller UID')
    return ','.join(key + '=' + labels[key] for key in sorted(labels))


def child(parent, candidates, node):
    require(type(candidates) is list and len(candidates) <= 1, 'fixture Job has multiple children or retried')
    if not candidates:
        return None
    pod = candidates[0]
    expected = {'apiVersion': 'batch/v1', 'kind': 'Job', 'controller': True,
                'name': parent['metadata']['name'], 'uid': parent['metadata']['uid']}
    owners = pod['metadata'].get('ownerReferences', [])
    require(len(owners) == 1 and expected.items() <= owners[0].items()
            and pod['metadata']['namespace'] == parent['metadata']['namespace'], 'fixture child controller differs')
    require(not pod['metadata'].get('deletionTimestamp') and pod['spec'].get('nodeName') == node,
            'fixture child placement or lifetime changed')
    require(pod.get('status', {}).get('phase') not in ('Failed', 'Succeeded'), 'fixture child terminated')
    statuses = pod.get('status', {}).get('containerStatuses', [])
    require(len(statuses) <= 1 and all(value.get('restartCount', 0) == 0 for value in statuses),
            'fixture child restarted or has extra containers')
    if not statuses or not statuses[0].get('ready') or 'running' not in statuses[0].get('state', {}):
        return None
    require(statuses[0].get('name') == 'worker', 'fixture child container changed')
    return pod


class FixtureJobs:
    def __init__(self, case, create, record):
        self.case, self.create, self.record = case, create, record
        self.parents, self.children = {}, {}

    def add(self, pod):
        job = job_from_pod(pod)
        meta = job['metadata']
        key = (meta['namespace'], meta['name'])
        require(key[0] in self.case.namespaces and key not in self.parents, 'fixture Job outside owned roster')
        require(meta.get('labels', {}).get('kube-memlens.io/fixture') == self.case.owner,
                'fixture Job lacks campaign ownership')
        require(not self.case.kube(['-n', key[0], 'get', 'job', key[1], '--ignore-not-found', '-o', 'name']).strip(),
                'fixture Job already exists; do not adopt it')
        parent = self.create(job)
        require(parent['metadata']['name'] == key[1] and parent['metadata']['namespace'] == key[0]
                and parent['metadata'].get('uid'), 'created Job identity unavailable')
        created_specification(parent, job)
        # API-defaulted selector/template fields belong to this exact Job lifetime.
        selector(parent)
        self.parents[key] = deepcopy(parent)
        self.record('created-fixture-job', {'namespace': key[0], 'name': key[1],
                    'uid': parent['metadata']['uid'], 'specSHA256': digest(canonical(parent['spec']))}, private=True)

    def parent(self, key):
        expected = self.parents[key]
        actual = self.case.json(['-n', key[0], 'get', 'job', key[1], '-o', 'json'])
        require(actual['metadata']['uid'] == expected['metadata']['uid']
                and not actual['metadata'].get('deletionTimestamp') and actual['spec'] == expected['spec']
                and actual['metadata'].get('labels', {}).get('kube-memlens.io/fixture') == self.case.owner,
                'fixture Job identity, ownership or specification changed')
        status = actual.get('status', {})
        require(not status.get('failed', 0) and not status.get('succeeded', 0)
                and not any(row.get('status') == 'True' and row.get('type') in ('Failed', 'Complete')
                            for row in status.get('conditions', [])), 'fixture Job completed or failed')
        return actual

    def observe(self, key):
        parent = self.parent(key)
        pods = self.case.json(['-n', key[0], 'get', 'pods', '-l', selector(parent), '-o', 'json'])['items']
        pod = child(parent, pods, self.case.node)
        if pod is not None and key in self.children:
            saved = self.children[key]
            require(pod['metadata']['uid'] == saved['uid'] and pod['metadata']['name'] == saved['name'],
                    'fixture Job replaced its bound child')
        return pod

    def ready(self):
        deadline = time.monotonic() + 90
        pending = set(self.parents)
        while pending:
            for key in sorted(pending):
                pod = self.observe(key)
                if pod is None:
                    continue
                self.children[key] = {'name': pod['metadata']['name'], 'uid': pod['metadata']['uid']}
                self.record('bound-fixture-child', {'namespace': key[0], 'logicalName': key[1],
                            **self.children[key]}, private=True)
                pending.remove(key)
            require(time.monotonic() < deadline, 'fixture Job readiness deadline exceeded')
            if pending:
                time.sleep(.25)

    def pod_name(self, namespace, logical_name):
        key = (namespace, logical_name)
        require(key in self.children, 'fixture child has not been bound')
        return self.children[key]['name']

    def verify(self):
        require(self.parents.keys() == self.children.keys(), 'fixture roster is not fully bound')
        for key in self.parents:
            require(self.observe(key) is not None, 'bound fixture child is no longer Ready')
