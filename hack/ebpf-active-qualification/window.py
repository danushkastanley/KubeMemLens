"""One complete paired-control or active window with actual bounded admissions."""
import base64
from datetime import datetime
import json
import time

from local_case import TraceClient, canonical, command, digest
from processes import Processes, read_document, wait_until
from profile import trace_slots
from provenance import write_envelope
from workload_case import workload_arguments


def clock_ns(case):
    return json.loads(case.runtime.exec(['/usr/local/bin/kml-lifecycle-census', '--mode', 'clock']))


def empty(snapshot):
    return all(snapshot[k] == 0 for k in ('workers', 'excludedWorkers', 'activeControls')) and not any(snapshot['objects'].values())


class Window:
    def __init__(self, case, fixtures, profile, directory, phase, pair):
        self.case, self.fixtures, self.profile, self.directory = case, fixtures, profile, directory
        self.phase, self.pair = phase, pair
        self.processes = Processes(case, directory, f'{pair}-{phase}')
        self.client = None
        self.session = None
        self.owned = {key: set() for key in ('map', 'prog', 'link')}
        self.sessions = []
        self.owner = None

    def snapshot(self):
        result = self.case.snapshot(self.owner, self.targets)
        for key, ids in result['objects'].items():
            self.owned[key].update(ids)
        return result

    def standard_config(self, services):
        c = self.case
        cluster = c.runtime.api_cluster()
        result = {'seconds': self.profile['windowSeconds'],
                  'bootID': c.runtime.exec(['cat', '/proc/sys/kernel/random/boot_id']).decode().strip(),
                  'server': c.runtime.observer_server(),
                  'networkScope': c.runtime.observer_network_scope(),
                  'token': c.kube(['-n', c.cfg['standardNamespace'], 'create', 'token', 'observer', '--duration=1h']).decode().strip(),
                  'caPEM': base64.b64decode(cluster['certificate-authority-data']).decode()}
        for role, binding in services.items():
            for source, suffix in [('pid', 'PID'), ('start', 'Start'), ('container', 'Container'), ('sha256', 'SHA256')]:
                result[role + suffix] = binding[source]
        return result

    def receiver_config(self, admission):
        c, target, t = self.case, self.targets[0], self.profile['trace']
        return {'server': c.runtime.observer_server(),
                  'networkScope': c.runtime.observer_network_scope(),
                'token': self.client.token, 'caPEM': self.client.ca,
                'workerBootID': self.boot, 'sessionID': admission['metadata']['name'],
                'engineDigest': 'sha256:' + c.runtime.cfg['engineSHA256'],
                'programmeDigest': 'sha256:' + c.runtime.cfg['programmeIndexSHA256'],
                'target': {'Namespace': c.namespaces[0], 'PodName': 'target', 'PodUID': target['podUID'],
                           'ContainerName': 'worker', 'ContainerID': target['container'],
                           'ContainerStartedAt': target['startedAt'], 'NodeUID': c.runtime.cfg['nodeUID'],
                           'CgroupID': target['group']['inode']},
                **{key: t[key] for key in ('durationSeconds', 'maxEvents', 'maxOutputBytes', 'maxMapBytes', 'maxPathBytes')}}

    def trace(self, index, due):
        t, c = self.profile['trace'], self.case
        while time.monotonic() < due:
            self.processes.healthy()
            time.sleep(max(0, min(.25, due - time.monotonic())))
        self.processes.healthy()
        lateness = time.monotonic() - due
        if lateness > .25:
            raise ValueError('trace admission slot missed; no retimed retry')
        if not empty(self.snapshot()):
            raise ValueError('previous trace state remains at admission slot')
        intent = {'schemaVersion': 1, 'pod': 'target', 'container': 'worker',
                  **{key: t[key] for key in ('kind', 'rawPaths', 'durationSeconds', 'maxEvents', 'maxOutputBytes', 'maxMapBytes', 'maxPathBytes')}}
        requested = clock_ns(c)
        status, admission = self.client.call('POST', value=intent)
        if status != 201 or admission['engineDigest'] != 'sha256:' + c.runtime.cfg['engineSHA256']:
            raise ValueError('normal admission did not match candidate')
        self.session = '/' + admission['metadata']['name']
        path = self.processes.configuration(f'receiver-{index}', self.receiver_config(admission))
        receiver = self.processes.native(f'delivery-{index:02}', 'delivery', '--config', path)
        deadline = time.monotonic() + 5
        while True:
            snapshot = self.snapshot()
            if (snapshot['workers'] == snapshot['activeControls'] == 1 and snapshot['excludedWorkers'] == 0 and
                    all(len(snapshot['objects'][key]) == count for key, count in t['objects'].items())):
                break
            if receiver.poll() is not None or time.monotonic() >= deadline:
                raise ValueError('complete owned attachment not observed')
            time.sleep(.1)
        attached = snapshot['clock']
        status, active = self.client.call('GET', self.session)
        if status != 200 or active['state'] != 'active' or active['metadata']['name'] != admission['metadata']['name']:
            raise ValueError('active admission lifetime unavailable')
        expiry = datetime.fromisoformat(active['expiresAt'].replace('Z', '+00:00'))
        expiry_ns = int(expiry.timestamp()) * 1000000000 + expiry.microsecond * 1000
        code = receiver.wait(timeout=40)
        delivered = read_document(self.directory / f'delivery-{index:02}.jsonl')
        if not delivered['sameKernelClock'] or not delivered['observation']['transportComplete'] or not delivered['observation']['hookCoverageIncomplete']:
            raise ValueError('normal event transport incomplete')
        if delivered['latency']['receivedEvents'] == 0:
            raise ValueError('normal trace delivered no selected events')
        budget_pass = delivered['latency']['eventDeliveryBudgetPassed'] and delivered['latency']['normalLossBudgetPassed']
        if code not in (0, 1) or (code == 0) != budget_pass:
            raise ValueError('delivery process and complete result disagree')
        if code == 1:
            self.processes.accept_delivery_budget_failure(receiver)
        status, _ = self.client.call('DELETE', self.session)
        if status not in (200, 404, 410):
            raise ValueError('owned admission cancellation unconfirmed')
        self.session = None
        deadline = time.monotonic() + 5
        while True:
            current = self.snapshot()
            left = c.remaining({key: sorted(ids) for key, ids in self.owned.items()})
            if empty(current) and not any(left['remaining'].values()):
                break
            if time.monotonic() >= deadline:
                raise ValueError('owned trace state did not clear')
            time.sleep(.1)
        teardown = max(0, current['clock']['wallNanos'] + current['clock']['uncertaintyNanos'] - expiry_ns)
        self.sessions.append({'index': index, 'scheduledOffsetSeconds': trace_slots(self.profile)[index],
                              'admissionLatenessNanos': int(lateness * 1000000000),
                              'attachUpperNanos': attached['wallNanos'] + attached['uncertaintyNanos'] - requested['wallNanos'] + requested['uncertaintyNanos'],
                              'deadlineTeardownUpperNanos': teardown, 'zeroOwnedState': True,
                              'deliverySHA256': digest((self.directory / f'delivery-{index:02}.jsonl').read_bytes()),
                              'latency': delivered['latency']})

    def run(self):
        c, p = self.case, self.profile
        self.directory.mkdir(mode=0o700)
        self.fixtures.verify()
        mapping = self.fixtures.mapping()
        self.targets = [c.fixture(ns, 'target') for ns in c.namespaces]
        standard = {role: c.standard(role) for role in ('agent', 'collector')}
        groups = [standard[role]['group'] for role in ('agent', 'collector')] + [self.targets[0]['group']]
        self.boot = c.runtime.exec(['cat', '/proc/sys/kernel/random/boot_id']).decode().strip()
        trace_services = {}
        if self.phase == 'enabled':
            trace_services = {role: c.runtime.service(role) for role in ('node', 'api')}
            groups += [trace_services[role]['group'] for role in ('node', 'api')]
            self.owner = c.node_owner()
            if not empty(self.snapshot()):
                raise ValueError('owned state present before window')
            self.client = TraceClient(c.runtime, c.namespaces[0])
        elif self.phase != 'control':
            raise ValueError('invalid window phase')
        else:
            c.runtime.absent()
        failure = None
        try:
            standard_path = self.processes.configuration('standard', self.standard_config(standard))
            measure_path = self.processes.configuration('measure', {'seconds': p['windowSeconds'], 'groups': groups})
            origin = time.monotonic()
            measure = self.processes.native('resources', 'measure', '--config', measure_path)
            metrics = self.processes.native('standard', 'standard', '--config', standard_path)
            if self.phase == 'enabled':
                self.processes.native('witness', 'watch', '--mode', 'watch', '--watch-seconds', str(p['windowSeconds']),
                                      *self.owner['flags'], '--target-cgroups', ','.join(str(x['group']['inode']) for x in self.targets))
            workload = self.processes.start('workload', c.runtime.kube + ['-n', c.namespaces[0], 'exec', 'target', '-c', 'worker', '--',
                                                                       '/usr/local/bin/kml-io-workload', *workload_arguments(p)])
            if self.phase == 'enabled':
                for index, offset in enumerate(trace_slots(p)):
                    self.trace(index, origin + offset)
            self.processes.wait_all(origin + p['windowSeconds'] + 15)
            self.fixtures.verify()
            if self.fixtures.mapping() != mapping:
                raise ValueError('production fixture mapping changed')
            if any(c.standard(role) != standard[role] for role in standard):
                raise ValueError('standard service changed during window')
            if self.phase == 'enabled':
                if any(c.runtime.service(role)['identity'] != trace_services[role]['identity'] for role in trace_services):
                    raise ValueError('tracer service changed during window')
            else:
                c.runtime.absent()
            if c.runtime.exec(['cat', '/proc/sys/kernel/random/boot_id']).decode().strip() != self.boot:
                raise ValueError('kernel lifetime changed')
        except BaseException:
            failure = True
            raise
        finally:
            cleanup_errors = []
            if self.session:
                try:
                    status, _ = self.client.call('DELETE', self.session)
                    if status not in (200, 404, 410):
                        raise ValueError('cancel failed')
                except Exception:
                    cleanup_errors.append('owned admission')
            stopped = False
            if failure:
                try:
                    for role in ('api', 'node'):
                        if c.runtime.deployment(role)['spec']['replicas'] == 1:
                            c.runtime.scale(role, 0)
                    c.runtime.wait_absent()
                    if trace_services:
                        c.runtime.stopped(trace_services)
                        for service in trace_services.values():
                            group = service['group']
                            c.runtime.exec(['sh', '-ec', 'if test -d "$1"; then test "$(stat -c %i "$1")" = "$2"; test "$(cat "$1/pids.current")" = 0; fi', '--', group['path'], str(group['inode'])])
                    stopped = True
                    # Old process/cgroup bindings are gone. Keep aggregated API
                    # discovery available while Kubernetes deletes namespaces.
                    for role in ('node', 'api'):
                        c.runtime.scale(role, 1)
                    c.runtime.ready()
                    for role in ('node', 'api'):
                        c.runtime.service(role)
                except Exception:
                    cleanup_errors.append('owned tracer sources')
                try:
                    self.fixtures.cleanup()
                except Exception:
                    cleanup_errors.append('owned standard/workload sources')
            try:
                self.processes.close()
            except Exception:
                cleanup_errors.append('observers or private inputs')
            if self.owner:
                try:
                    final = None if stopped else self.snapshot()
                    left = c.remaining({key: sorted(ids) for key, ids in self.owned.items()})
                    if (final is not None and not empty(final)) or any(left['remaining'].values()):
                        raise ValueError('owned state remains')
                except Exception:
                    cleanup_errors.append('owned BPF state')
            result = {'phase': self.phase, 'pair': self.pair, 'completed': failure is None,
                      'cleanupFailures': cleanup_errors, 'sessions': self.sessions, 'fixtureMapping': mapping,
                      'fixtureIdentities': {key: value['identity'] for key, value in self.fixtures.bindings.items()},
                      'standardIdentities': {key: value['identity'] for key, value in standard.items()}}
            # Binding keys contain owned fixture names; this receipt remains private.
            with (self.directory / 'window.private.json').open('x') as stream:
                json.dump(result, stream, indent=2)
            if cleanup_errors:
                raise ValueError('window cleanup unconfirmed; no qualification')
        write_envelope(self.directory, self.phase, self.pair, p, c.cfg, self.boot)
        return result
