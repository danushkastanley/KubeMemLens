"""Journal and reconcile only a verifier window's exact owned tracefs definitions."""
import os
import re

from local_case import canonical
from samples import require

REGISTRY = '/sys/kernel/tracing/kprobe_events'
JOURNAL = 'verifier-probes.private.json'


def definitions(owner):
    require(type(owner) is str and re.fullmatch('[a-f0-9]{32}', owner), 'invalid verifier probe owner')
    group = 'kml_verifier_' + owner
    return {group + '/' + role + '_' + owner: prefix + group + '/' + role + '_' + owner + ' ' + target
            for role, prefix, target in [('enter', 'p:', 'bpf_check'),
                ('return', 'r128:', 'bpf_check result=$retval:s32'),
                ('log', 'r128:', 'bpf_vlog_finalize result=$retval:s32 bytes=+0($arg2):u32')]}


class ProbeLease:
    def __init__(self, runtime, directory, owner, helper):
        require(type(helper) is str and re.fullmatch(r'/usr/local/bin/kml-[a-z0-9-]{1,120}', helper),
                'invalid verifier helper path')
        self.runtime, self.helper, self.owner = runtime, helper, owner
        self.expected = definitions(owner)
        require(not self.registry(), 'verifier owner already has registered probes')
        # This must be durable before starting the helper; an interrupted helper
        # cannot remove its tracefs definitions after a hard process exit.
        with (directory / JOURNAL).open('xb') as stream:
            stream.write(canonical({'schemaVersion': 1, 'owner': owner, 'helper': helper,
                                    'expectedDefinitions': self.expected, 'initialOwnedRegistryEmpty': True}))
            stream.flush()
            os.fsync(stream.fileno())
        self.closed = False

    def registry(self):
        raw = self.runtime.exec(['cat', REGISTRY])
        require(len(raw) <= 1 << 20, 'verifier registry exceeds bound')
        prefix = 'kml_verifier_' + self.owner + '/'
        found = {}
        for line in raw.decode().splitlines():
            fields = line.split()
            if not fields or ':' not in fields[0]:
                continue
            name = fields[0].split(':', 1)[1]
            if not name.startswith(prefix):
                continue
            require(name in self.expected and name not in found and ' '.join(fields) == self.expected[name],
                    'owned verifier definition changed; refusing reconciliation')
            found[name] = ' '.join(fields)
        return found

    def close(self):
        if self.closed:
            return
        # A stopped Docker/remote client alone does not establish native exit.
        # Refuse mutation while the exact installed helper is still running.
        check = ('for f in /proc/[0-9]*/exe; do x=$(readlink "$f" 2>/dev/null || true); '
                 '[ "$x" != "$1" ] && [ "$x" != "$1 (deleted)" ] || exit 1; done')
        self.runtime.exec(['sh', '-ec', check, '--', self.helper])
        for name in reversed(list(self.expected)):
            if name in self.registry():
                self.runtime.exec(['sh', '-ec', 'printf \'%s\\n\' "$1" >> ' + REGISTRY, '--', '-:' + name])
        require(not self.registry(), 'owned verifier probes remain')
        self.closed = True
