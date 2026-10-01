"""Run a frozen five-pair local normal or mixed case; no provider/support claim."""
import argparse
import copy
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import signal
import time

from environment import kernel_accounting
from evaluate import evaluate_pair
from fixtures import Fixtures
from local_case import LocalCase, canonical, command, digest
from local_runtime import spec_digest
from processes import wait_until
from profile import load_profile
from high_rate_profile import load_high_rate_profile
from preflight import read_configuration, certificate_lifetimes
from chart_inventory import verify_inventory
from window import Window

ROOT = Path(__file__).resolve().parents[2]
SOURCE_DIRS = ['hack/ebpf-active-qualification', 'hack/ebpf-qualification',
               'prototype/trace/qualification/active-measure', 'prototype/trace/qualification/lifecycle',
               'prototype/trace/qualification/filecache', 'prototype/trace/qualification/delivery',
               'prototype/trace/qualification/delivery-client', 'hack/node-qualification/chart-inventory']


def sources():
    paths = []
    for name in SOURCE_DIRS:
        paths += [p for p in (ROOT / name).rglob('*') if p.is_file() and '__pycache__' not in p.parts
                  and (p.suffix in {'.go', '.py', '.json', '.md', '.c'} or p.name == 'Dockerfile')]
    paths += [ROOT / p for p in ('go.mod', 'go.sum', 'prototype/trace/go.mod', 'prototype/trace/go.sum')]
    paths += [p for p in (ROOT / 'charts/kube-memlens').rglob('*') if p.is_file()]
    return {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in sorted(paths)}


def write(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


class Campaign:
    def __init__(self, cfg, directory, profile):
        self.cfg, self.directory = cfg, directory
        self.profile = profile
        self.label = {'normal-confirmed-files': 'normal', 'high-rate-mixed-files': 'high-rate'}[profile['case']]
        self.source = sources()
        if digest(canonical(self.source)) != cfg['sourceSHA256']:
            raise ValueError('source differs from independently frozen manifest')
        verify_inventory(cfg['chartInventory'])
        self.case = LocalCase(cfg)
        self.case.runtime.policy()
        self.case.runtime.ready()
        for role in ('node', 'api'):
            self.case.runtime.service(role)
        for helper in cfg['helpers'].values():
            if self.case.runtime.exec(['sha256sum', helper['path']]).decode().split()[0] != helper['sha256']:
                raise ValueError('helper differs from frozen build')
        self.kernel = kernel_accounting(self.case.runtime)
        certificates = certificate_lifetimes(self.case.runtime, self.profile)
        self.directory.mkdir(mode=0o700)
        self.sequence = 0
        self.original = self.case.runtime.deployment('api')
        self.original_cfg = copy.deepcopy(cfg['trace'])
        self.confirmed = None
        self.fixtures = None
        self.pairs = []
        for name in self.source:
            target = directory / 'source' / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)
        write(directory / 'freeze.json', {'schemaVersion': 1, 'createdAt': datetime.now(timezone.utc).isoformat(),
              'profile': self.profile, 'source': self.source, 'sourceSHA256': cfg['sourceSHA256'],
              'privateConfigurationSHA256': digest(canonical(cfg)), 'kernelAccounting': self.kernel,
              'certificateLifetimes': certificates,
              'images': {k: cfg[k] for k in ('standardImage', 'fixtureImage')},
              'tracerImage': cfg['trace']['image'], 'policySHA256': cfg['trace']['policySHA256'],
              'helperSHA256': {name: value['sha256'] for name, value in cfg['helpers'].items()},
              'scope': 'owned local ' + self.label + ' workload; no full qualification or cloud execution'})

    def record(self, event, value, private=False):
        self.sequence += 1
        write(self.directory / f'{self.sequence:04}-{event}{".private" if private else ""}.json', value)

    def progress(self, state, pair):
        temporary = self.directory / 'progress.tmp'
        temporary.write_text(json.dumps({'state': state, 'pair': pair, 'updatedAt': datetime.now(timezone.utc).isoformat()}))
        temporary.replace(self.directory / 'progress.json')
        print(f'{self.label} pair {pair}: {state}', flush=True)

    def invariant(self):
        if sources() != self.source:
            raise ValueError('source changed during campaign')
        verify_inventory(self.cfg['chartInventory'])
        self.case.runtime.policy()
        for role in ('node', 'api'):
            self.case.runtime.deployment(role)
        for helper in self.cfg['helpers'].values():
            if self.case.runtime.exec(['sha256sum', helper['path']]).decode().split()[0] != helper['sha256']:
                raise ValueError('helper changed during campaign')
        if kernel_accounting(self.case.runtime) != self.kernel:
            raise ValueError('kernel accounting configuration changed')

    def confirmed_profile(self):
        runtime = self.case.runtime
        current = runtime.deployment('api')
        desired = copy.deepcopy(current)
        args = desired['spec']['template']['spec']['containers'][0]['args']
        if '--allow-confirmed-paths' in args:
            raise ValueError('campaign must start from recorded default profile')
        args.append('--allow-confirmed-paths')
        cfg = copy.deepcopy(self.cfg)
        cfg['trace']['deploymentSpecSHA256']['api'] = spec_digest(desired['spec'])
        write(self.directory / 'profile-transition.private.json', {'before': current, 'afterSpec': desired['spec'],
              'configuration': cfg, 'purpose': 'explicit confirmed paths for owned fixed file; no previous profile verdict reused'})
        patch = [{'op': 'test', 'path': '/metadata/uid', 'value': current['metadata']['uid']},
                 {'op': 'test', 'path': '/metadata/resourceVersion', 'value': current['metadata']['resourceVersion']},
                 {'op': 'replace', 'path': '/spec', 'value': desired['spec']}]
        self.confirmed = desired
        command(runtime.kube + ['-n', runtime.cfg['namespace'], 'patch', 'deployment', runtime.services['api'], '--type=json', '--patch-file=/dev/stdin'], canonical(patch))
        self.case = LocalCase(cfg)
        self.case.runtime.ready()
        self.case.runtime.service('api')

    def restore(self):
        # Restoration is independent from fixture teardown and refuses concurrent
        # specification changes. It never erases a failed campaign's evidence.
        runtime = self.case.runtime
        if self.confirmed:
            current = runtime.get('deployment', runtime.services['api'])
            if (current['metadata']['uid'] != self.original['metadata']['uid'] or
                    spec_digest(current['spec']) not in (spec_digest(self.original['spec']), spec_digest(self.confirmed['spec']))):
                raise ValueError('concurrent profile change; do not overwrite')
            patch = [{'op': 'test', 'path': '/metadata/uid', 'value': current['metadata']['uid']},
                     {'op': 'test', 'path': '/metadata/resourceVersion', 'value': current['metadata']['resourceVersion']},
                     {'op': 'replace', 'path': '/spec', 'value': self.original['spec']}]
            command(runtime.kube + ['-n', runtime.cfg['namespace'], 'patch', 'deployment', runtime.services['api'], '--type=json', '--patch-file=/dev/stdin'], canonical(patch))
        self.case = LocalCase(self.cfg)
        for role in ('node', 'api'):
            if self.case.runtime.deployment(role)['spec']['replicas'] == 0:
                self.case.runtime.scale(role, 1)
        self.case.runtime.ready()
        for role in ('node', 'api'):
            self.case.runtime.service(role)
        self.record('restored', {'originalDefaultProfile': True, 'readyAndIdle': True})

    def run(self):
        try:
            self.confirmed_profile()
            for pair in range(1, self.profile['pairs'] + 1):
                self.invariant()
                pair_dir = self.directory / f'pair-{pair}'
                pair_dir.mkdir(mode=0o700)
                self.fixtures = Fixtures(self.case, self.record)
                self.progress('preparing-owned-fixtures', pair)
                self.fixtures.prepare(self.profile)
                previous = {role: self.case.runtime.service(role) for role in ('node', 'api')}
                self.case.runtime.scale('api', 0)
                self.case.runtime.scale('node', 0)
                self.case.runtime.wait_absent()
                self.case.runtime.stopped(previous)
                self.progress('control-warmup', pair)
                wait_until(time.monotonic() + self.profile['warmupSeconds'])
                self.progress('control-sampling', pair)
                Window(self.case, self.fixtures, self.profile, pair_dir / 'control', 'control', pair).run()
                self.invariant()
                self.case.runtime.scale('node', 1)
                self.case.runtime.scale('api', 1)
                self.case.runtime.ready()
                self.progress('enabled-warmup', pair)
                wait_until(time.monotonic() + self.profile['warmupSeconds'])
                self.progress('enabled-sampling', pair)
                Window(self.case, self.fixtures, self.profile, pair_dir / 'enabled', 'enabled', pair).run()
                self.invariant()
                result = evaluate_pair(pair_dir / 'control', pair_dir / 'enabled', self.profile)
                result['pair'] = pair
                write(pair_dir / 'result.json', result)
                self.pairs.append(result)
                self.fixtures.cleanup()
                self.fixtures = None
                self.progress('paired-measurements-complete', pair)
            write(self.directory / (self.label + '-result.json'), {'schemaVersion': 1, 'pairs': self.pairs,
                  'measuredNormalBudgetsPassed': all(p['measuredNormalBudgetsPassed'] for p in self.pairs),
                  'qualification': 'normal measured budgets applied to this case only; remaining protocol gates are not waived'})
            self.progress(self.label + '-case-complete', self.profile['pairs'])
        except BaseException:
            self.progress('interrupted-or-invalid; evidence retained', len(self.pairs) + 1)
            raise
        finally:
            try:
                # Namespace finalisation needs aggregated API discovery even if
                # an interruption happened during the no-tracer control window.
                self.restore()
            finally:
                if self.fixtures:
                    self.fixtures.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    cases = parser.add_mutually_exclusive_group(required=True)
    cases.add_argument('--acknowledge-local-normal-campaign', action='store_const', const='normal', dest='case')
    cases.add_argument('--acknowledge-local-high-rate-campaign', action='store_const', const='high-rate', dest='case')
    args = parser.parse_args()
    os.umask(0o077)
    cfg = read_configuration(args.config)
    def interrupted(_signal, _frame):
        raise InterruptedError('campaign interrupted; restore owned resources')
    signal.signal(signal.SIGTERM, interrupted)
    profile = {'normal': load_profile, 'high-rate': load_high_rate_profile}[args.case]()
    Campaign(cfg, args.output, profile).run()


if __name__ == '__main__':
    main()
