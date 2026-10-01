"""Bind verifier observation to the enabled Node or explicit standard control."""
import re
import secrets

from local_case import canonical, digest
from processes import read_document
from samples import exact, integer, require, strict_json, read_samples
from scheduler_capture import cpu_count
from activity import clock
from verifier_probe_lease import ProbeLease, JOURNAL, definitions
from verifier_window import read_verifier_samples, verifier_window

INPUT = 'verifier-input.private.json'
STREAM = 'verifier.jsonl'
BASE_OWNER = {'pid', 'start', 'container', 'sha256', 'podName', 'podUID', 'startedAt',
              'group', 'specSHA256', 'identity'}


def validate_input(value, seconds, boot, owner, phase):
    exact(value, {'owner', 'seconds', 'bootID', 'onlineCPUs', 'btfSHA256', 'anchor', 'group'})
    integer(seconds, 1, 1800)
    require(type(value['seconds']) is int and value['seconds'] == seconds and value['bootID'] == boot,
            'verifier duration or boot differs from window')
    require(type(boot) is str and re.fullmatch('[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', boot),
            'invalid verifier boot binding')
    definitions(value['owner'])
    require(phase in ('enabled', 'control'), 'invalid verifier phase')
    expected_fields = BASE_OWNER | ({'controllerUID', 'controllerSpecSHA256'} if phase == 'control' else set())
    exact(owner, expected_fields)
    require(owner['identity'] == digest(canonical({k: v for k, v in owner.items() if k != 'identity'})),
            'verifier process binding hash differs')
    integer(owner['pid'], 2, 2**31 - 1)
    integer(owner['start'], 1)
    exact(owner['group'], {'role', 'path', 'inode'})
    integer(owner['group']['inode'], 1)
    require(owner['group']['role'] == ('node' if phase == 'enabled' else 'collector'),
            'verifier cgroup role differs from phase')
    for value_hash in (owner['sha256'], value['btfSHA256']):
        require(type(value_hash) is str and re.fullmatch('[a-f0-9]{64}', value_hash), 'invalid verifier source hash')
    exact(value['anchor'], {'pid', 'start', 'sha256'})
    integer(value['anchor']['pid'], 2, 2**31 - 1)
    integer(value['anchor']['start'], 1)
    require(value['anchor'] == {key: owner[key] for key in ('pid', 'start', 'sha256')},
            'verifier anchor differs from owned process')
    exact(value['group'], {'path', 'inode'})
    integer(value['group']['inode'], 1)
    require(value['group'] == {key: owner['group'][key] for key in ('path', 'inode')},
            'verifier cgroup differs from owned process')
    path = value['group']['path']
    require(type(path) is str and len(path) <= 4096 and path.startswith('/sys/fs/cgroup/') and
            all(part not in ('', '.', '..') for part in path.split('/')[1:]), 'invalid verifier cgroup path')
    return cpu_count(value['onlineCPUs'])


def prepare_verifier(case, processes, directory, seconds, boot, owner, phase):
    bound = {key: value for key, value in owner.items() if key != 'flags'}
    runtime = case.runtime
    btf = runtime.exec(['sha256sum', '/sys/kernel/btf/vmlinux']).decode().split()
    require(len(btf) == 2 and btf[1] == '/sys/kernel/btf/vmlinux', 'kernel BTF identity unavailable')
    value = {'owner': secrets.token_hex(16), 'seconds': seconds, 'bootID': boot,
             'onlineCPUs': runtime.exec(['cat', '/sys/devices/system/cpu/online']).decode().strip(),
             'btfSHA256': btf[0], 'anchor': {key: bound[key] for key in ('pid', 'start', 'sha256')},
             'group': {key: bound['group'][key] for key in ('path', 'inode')}}
    validate_input(value, seconds, boot, bound, phase)
    raw = canonical(value)
    require(len(raw) <= 16384, 'verifier input exceeds native bound')
    with (directory / INPUT).open('xb') as stream:
        stream.write(raw)
    lease = ProbeLease(runtime, directory, value['owner'], case.cfg['helpers']['verifier']['path'])
    path = processes.configuration('verifier', value)
    return path, lease, bound


def replay_verifier(directory, seconds, scope, phase):
    path = directory / INPUT
    require(path.stat().st_size <= 16384, 'verifier input exceeds native bound')
    value = strict_json(path.read_bytes())
    receipt = read_document(directory / 'window.private.json')
    require(receipt.get('completed') is True and receipt.get('cleanupFailures') == [] and
            receipt.get('verifierCleanup') is True and receipt.get('phase') == phase,
            'verifier window, phase or cleanup incomplete')
    owner = receipt.get('verifierOwner')
    count = validate_input(value, seconds, scope['bootID'], owner, phase)
    if phase == 'control':
        require(owner['identity'] == receipt.get('standardIdentities', {}).get('collector'),
                'verifier control differs from standard collector')
    else:
        require(owner['identity'] == receipt.get('traceNodeOwnerIdentity'), 'verifier enabled scope differs from trace Node')
    require((directory / JOURNAL).stat().st_size <= 16384, 'verifier journal exceeds bound')
    journal = strict_json((directory / JOURNAL).read_bytes())
    exact(journal, {'schemaVersion', 'owner', 'helper', 'expectedDefinitions', 'initialOwnedRegistryEmpty'})
    require(type(journal['schemaVersion']) is int and journal['schemaVersion'] == 1 and
            journal['owner'] == value['owner'] and journal['expectedDefinitions'] == definitions(value['owner']) and
            journal['initialOwnedRegistryEmpty'] is True and type(journal['helper']) is str and
            re.fullmatch(r'/usr/local/bin/kml-[a-z0-9-]{1,120}', journal['helper']), 'verifier probe journal differs')
    rows = read_verifier_samples(directory / STREAM)
    require(len(rows) == seconds + 1, 'verifier stream incomplete')
    binding = rows[0].get('binding')
    require(type(binding) is dict and all(binding.get(key) == expected for key, expected in {
        'owner': value['owner'], 'btfSHA256': value['btfSHA256'], 'anchorSHA256': value['anchor']['sha256'],
        'cgroupInode': value['group']['inode']}.items()), 'verifier stream differs from frozen input')
    result = verifier_window(rows, seconds, count, binding)
    standard = read_samples(directory / 'standard.jsonl')
    require(len(standard) == seconds + 1, 'verifier standard clock reference incomplete')
    for observed, reference in ((rows[0], standard[0]), (rows[-1], standard[-1])):
        clock(reference['clock'])
        integer(reference['readNanos'], 1, 100_000_000)
        start = reference['clock']['monotonicNanos'] - reference['readNanos']
        require(abs(observed['cutoffNanos'] - start) <= 1_000_000_000,
                'verifier capture is outside the standard observation window')
    return {'observations': result, 'inputSHA256': digest(path.read_bytes()),
            'platform': {key: value[key] for key in ('bootID', 'onlineCPUs', 'btfSHA256')}}


def paired_verifier(control, enabled, seconds, scope):
    before = replay_verifier(control, seconds, scope, 'control')
    after = replay_verifier(enabled, seconds, scope, 'enabled')
    require(before['platform'] == after['platform'], 'paired verifier kernel/CPU binding changed')
    require(before['observations']['totals']['completedCalls'] == 0,
            'capability-free collector control unexpectedly executed verifier calls')
    require(after['observations']['totals']['completedCalls'] > 0,
            'enabled verifier window contains no completed calls')
    return {'control': before['observations'], 'enabled': after['observations'],
            'inputSHA256': {'control': before['inputSHA256'], 'enabled': after['inputSHA256']},
            'scope': 'enabled trace Node cgroup and descendants; standard collector is a distinct negative control; no cross-scope subtraction'}
