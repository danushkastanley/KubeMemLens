"""Synthetic verifier streams for controller tests only, never runtime evidence."""
import json

from local_case import canonical, digest
from scheduler_test_fixture import BOOT, owner_binding
from test_verifier_window import samples
from verifier_probe_lease import definitions


def write_verifier(directory, seconds, receipt, phase, boot=BOOT):
    owner = owner_binding()
    if phase == 'enabled':
        for key in ('controllerUID', 'controllerSpecSHA256'):
            del owner[key]
        owner['group']['role'] = 'node'
        owner['group']['path'] = '/sys/fs/cgroup/owned-node'
    owner['identity'] = digest(canonical({key: value for key, value in owner.items() if key != 'identity'}))
    receipt.update(phase=phase, verifierOwner=owner, verifierCleanup=True,
                   traceNodeOwnerIdentity=owner['identity'] if phase == 'enabled' else None)
    config = {'owner': ('a' if phase == 'control' else 'b') * 32, 'seconds': seconds,
              'bootID': boot, 'onlineCPUs': '0-1', 'btfSHA256': 'd' * 64,
              'anchor': {key: owner[key] for key in ('pid', 'start', 'sha256')},
              'group': {key: owner['group'][key] for key in ('path', 'inode')}}
    journal = {'schemaVersion': 1, 'owner': config['owner'], 'helper': '/usr/local/bin/kml-test-verifier',
               'expectedDefinitions': definitions(config['owner']), 'initialOwnedRegistryEmpty': True}
    (directory / 'verifier-input.private.json').write_bytes(canonical(config))
    (directory / 'verifier-probes.private.json').write_bytes(canonical(journal))
    with (directory / 'standard.jsonl').open() as stream:
        reference = json.loads(stream.readline())
    rows = samples(seconds)
    offset = reference['clock']['monotonicNanos'] - reference['readNanos'] - rows[0]['startedNanos']
    for row in rows:
        row['binding'].update(owner=config['owner'], btfSHA256=config['btfSHA256'],
                              anchorSHA256=owner['sha256'], cgroupInode=owner['group']['inode'])
        for key in ('startedNanos', 'cutoffNanos', 'readStartedNanos', 'readEndedNanos',
                    'closeStartedNanos', 'closeEndedNanos'):
            if row[key]:
                row[key] += offset
        if phase == 'control':
            row['observation'] = {key: 0 for key in row['observation']}
            row['perf']['events'] = 0
            for count in row['probeCounts'].values():
                count['hits'] = 0
    (directory / 'verifier.jsonl').write_bytes(b''.join(canonical(row) + b'\n' for row in rows))
