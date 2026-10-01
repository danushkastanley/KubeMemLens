"""Bind scheduler capture and replay to the same owned standard-service lifetime."""
import re

from local_case import canonical, digest
from processes import read_document
from samples import exact, integer, require, strict_json, read_samples
from activity import clock
from scheduler_window import read_scheduler_samples, scheduler_window

TRACEPOINTS = ('sched_wakeup', 'sched_wakeup_new', 'sched_switch', 'sched_process_exit')
INPUT = 'scheduler-input.private.json'
STREAM = 'scheduler.jsonl'
OWNER_FIELDS = {'pid', 'start', 'container', 'sha256', 'podName', 'podUID', 'startedAt',
                'group', 'specSHA256', 'controllerUID', 'controllerSpecSHA256', 'identity'}


def cpu_count(value):
    require(type(value) is str and 0 < len(value) <= 512, 'scheduler CPU roster missing or unbounded')
    cpus = []
    for part in value.split(','):
        require(re.fullmatch(r'[0-9]+(?:-[0-9]+)?', part) is not None, 'invalid scheduler CPU roster')
        bounds = [int(x) for x in part.split('-')]
        first, last = bounds[0], bounds[-1]
        require(0 <= first <= last <= 4095 and (len(bounds) == 1 or first < last)
                and last - first + 1 <= 64 - len(cpus), 'invalid scheduler CPU bounds')
        require(not cpus or first > cpus[-1], 'scheduler CPUs overlap or are unordered')
        cpus.extend(range(first, last + 1))
    return len(cpus)


def validate_input(value, seconds, boot, owner):
    exact(value, {'seconds', 'bootID', 'onlineCPUs', 'tracepointSHA256', 'anchor'})
    require(type(value['seconds']) is int and value['seconds'] == seconds and value['bootID'] == boot,
            'scheduler duration or boot differs from window')
    integer(seconds, 1, 1800)
    require(type(boot) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', boot),
            'invalid scheduler boot binding')
    exact(owner, OWNER_FIELDS)
    require(owner['identity'] == digest(canonical({k: v for k, v in owner.items() if k != 'identity'})),
            'scheduler owner identity hash differs')
    integer(owner['pid'], 2, 2**31 - 1)
    integer(owner['start'], 1)
    exact(owner['group'], {'role', 'path', 'inode'})
    require(owner['group']['role'] == 'collector', 'scheduler anchor must be the validated standard collector')
    exact(value['anchor'], {'pid', 'start'})
    integer(value['anchor']['pid'], 2, 2**31 - 1)
    integer(value['anchor']['start'], 1)
    require(value['anchor'] == {k: owner[k] for k in ('pid', 'start')}, 'scheduler anchor differs from owned process')
    exact(value['tracepointSHA256'], TRACEPOINTS)
    require(all(type(v) is str and re.fullmatch(r'[a-f0-9]{64}', v) for v in value['tracepointSHA256'].values()),
            'invalid scheduler tracepoint hashes')
    return cpu_count(value['onlineCPUs'])


def prepare_scheduler(case, processes, directory, seconds, boot, owner):
    runtime = case.runtime
    value = {'seconds': seconds, 'bootID': boot, 'onlineCPUs': runtime.exec(
        ['cat', '/sys/devices/system/cpu/online']).decode().strip(),
        'anchor': {k: owner[k] for k in ('pid', 'start')}, 'tracepointSHA256': {}}
    for name in TRACEPOINTS:
        raw = runtime.exec(['cat', '/sys/kernel/tracing/events/sched/' + name + '/format'])
        require(0 < len(raw) <= 16384, 'scheduler tracepoint format exceeds bound')
        value['tracepointSHA256'][name] = digest(raw)
    validate_input(value, seconds, boot, owner)
    raw = canonical(value)
    require(len(raw) <= 16384, 'scheduler input exceeds native bound')
    with (directory / INPUT).open('xb') as stream:
        stream.write(raw)
    return processes.configuration('scheduler', value)


def replay_scheduler(directory, seconds, scope):
    path = directory / INPUT
    require(path.stat().st_size <= 16384, 'scheduler input exceeds native bound')
    value = strict_json(path.read_bytes())
    receipt = read_document(directory / 'window.private.json')
    require(receipt.get('completed') is True and receipt.get('cleanupFailures') == [], 'scheduler window/cleanup incomplete')
    owner = receipt.get('schedulerOwner')
    count = validate_input(value, seconds, scope['bootID'], owner)
    exact(receipt.get('standardIdentities'), {'agent', 'collector'})
    require(owner['identity'] == receipt['standardIdentities']['collector'], 'scheduler owner differs from standard service')
    rows = read_scheduler_samples(directory / STREAM)
    observations = scheduler_window(rows, seconds, count)
    standard = read_samples(directory / 'standard.jsonl')
    require(len(standard) == seconds + 1, 'standard clock reference incomplete')
    for observed, reference in ((rows[0], standard[0]), (rows[-1], standard[-1])):
        clock(reference['clock'])
        integer(reference['readNanos'], 1, 100_000_000)
        start = reference['clock']['monotonicNanos'] - reference['readNanos']
        require(abs(observed['cutoffNanos'] - start) <= 1_000_000_000,
                'scheduler capture is outside the standard observation window')
    observations['startSkewFromStandardNanos'] = rows[0]['startedNanos'] - (
        standard[0]['clock']['monotonicNanos'] - standard[0]['readNanos'])
    return {'observations': observations, 'inputSHA256': digest(path.read_bytes()),
            'platform': {k: value[k] for k in ('bootID', 'onlineCPUs', 'tracepointSHA256')}}


def paired_scheduler(control, enabled, seconds, scope):
    before = replay_scheduler(control, seconds, scope)
    after = replay_scheduler(enabled, seconds, scope)
    require(before['platform'] == after['platform'], 'paired scheduler CPU/tracepoint bindings changed')
    return {'control': before['observations'], 'enabled': after['observations'],
            'inputSHA256': {'control': before['inputSHA256'], 'enabled': after['inputSHA256']},
            'scope': 'node-wide completed-wait histogram bounds; observer cost and coverage remain explicit'}
