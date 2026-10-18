"""One full untraced noisy control window for owned storage sizing, not qualification."""
import argparse
import os
from pathlib import Path
import signal
import time

from campaign import Campaign, canonical, digest, write
from eks_case import EKSCase
from fixture_roster import noise_targets
from fixtures import Fixtures
from noisy_profile import load_noisy_profile
from preflight import read_configuration
from processes import wait_until
from provenance import file_digest
from storage_probe import StorageProbe
from storage_sizing_replay import replay_sizing
from window import Window


class StorageSizing(Campaign):
    execution_scope = 'owned local untraced storage sizing; one control window, no paired qualification'

    def __init__(self, cfg, directory):
        super().__init__(cfg, directory, load_noisy_profile())
        self.label = 'storage-sizing'

    def run(self):
        result = None
        try:
            self.invariant()
            self.fixtures = Fixtures(self.case, self.record)
            self.progress('preparing-owned-fixtures', 1)
            self.fixtures.prepare(self.profile)
            runtime = self.case.runtime
            previous = {role: runtime.service(role) for role in ('node', 'api')}
            runtime.scale('api', 0)
            runtime.scale('node', 0)
            runtime.wait_absent()
            runtime.stopped(previous)
            self.record('untraced-sizing-start', {'optionalServicesStopped': True})
            wait_until(time.monotonic() + self.profile['warmupSeconds'])
            roster = [{'namespace': self.case.namespaces[0], 'name': 'target'}]
            roster += noise_targets(self.profile, self.case.namespaces)
            keys = [item['namespace'] + '/' + self.fixtures.pod_name(item['namespace'], item['name']) for item in roster]
            probe = StorageProbe(self.case, {key: self.fixtures.bindings[key] for key in keys})
            write(self.directory / 'storage-bindings.private.json', probe.bindings)
            before = probe.snapshot()
            write(self.directory / 'storage-before.private.json', before)
            self.progress('full-untraced-window', 1)
            directory = self.directory / 'control'
            Window(self.case, self.fixtures, self.profile, directory, 'control', 1).run()
            after = probe.snapshot()
            write(self.directory / 'storage-after.private.json', after)
            runtime.absent()
            self.invariant()
            scope = {'pair': 1, 'sourceSHA256': self.cfg['sourceSHA256'],
                     'configurationSHA256': digest(canonical(self.cfg)), 'bootID': probe.boot}
            result = replay_sizing(directory, self.profile, before, after, scope, probe.bindings)
            result['storageSnapshotSHA256'] = {name: file_digest(self.directory / f'storage-{name}.private.json')
                                               for name in ('before', 'after', 'bindings')}
            write(self.directory / 'storage-comparison.private.json', result)
        except BaseException:
            self.progress('sizing-incomplete; evidence retained', 1)
            raise
        finally:
            try:
                self.restore()
            finally:
                if self.fixtures:
                    self.fixtures.cleanup()
        write(self.directory / 'storage-sizing-result.private.json',
              {**result, 'ownedServicesRestored': True, 'ownedFixtureCleanupComplete': True})
        self.progress('sizing-observations-complete', 1)


class EKSStorageSizing(StorageSizing):
    execution_scope = 'owned EKS untraced storage sizing; one control window, no paired qualification'

    def make_case(self, cfg):
        return EKSCase(cfg)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument('--acknowledge-local-storage-sizing', dest='runner', action='store_const', const=StorageSizing)
    modes.add_argument('--acknowledge-owned-eks-storage-sizing', dest='runner', action='store_const', const=EKSStorageSizing)
    args = parser.parse_args()
    os.umask(0o077)
    cfg = read_configuration(args.config)
    def interrupted(_signal, _frame):
        raise InterruptedError('sizing interrupted; restore owned resources')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(3000)
    try:
        args.runner(cfg, args.output).run()
    finally:
        signal.alarm(0)


if __name__ == '__main__':
    main()
