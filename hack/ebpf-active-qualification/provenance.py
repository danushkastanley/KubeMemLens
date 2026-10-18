"""Bind every numeric stream to one window and its independently frozen inputs."""
import hashlib
import json

from local_case import canonical, digest
from noisy_profile import CASE as NOISY_CASE
from nonselected_workload import ROLES as NOISE_ROLES
from concurrent_profile import CASE as CONCURRENT_CASE
from flood_profile import CASE as FLOOD_CASE, slots as flood_slots, stem as flood_stem
from pressure_profile import CASE as PRESSURE_CASE

MAX_STREAM = 256 << 20


def file_digest(path):
    if path.stat().st_size > MAX_STREAM:
        raise ValueError('observation stream exceeds bound')
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            value.update(chunk)
    return value.hexdigest()


def stream_names(phase, count):
    result = ['resources.jsonl', 'standard.jsonl', 'workload.jsonl']
    if phase == 'enabled':
        result += ['witness.jsonl', *[f'delivery-{i:02}.jsonl' for i in range(count)]]
    elif phase != 'control':
        raise ValueError('invalid window phase')
    return result


def profile_stream_names(phase, profile):
    return [*_workload_stream_names(phase, profile), 'scheduler.jsonl', 'scheduler-input.private.json',
            'verifier.jsonl', 'verifier-input.private.json', 'verifier-probes.private.json']


def _workload_stream_names(phase, profile):
    if profile['case'] == PRESSURE_CASE:
        names = ['resources.jsonl', 'standard.jsonl', 'pressure.jsonl']
        if phase == 'enabled':
            names += ['witness.jsonl', *[f'delivery-pressure-{i:03}.jsonl'
                                       for i in range(profile['trace']['count'])]]
        elif phase != 'control':
            raise ValueError('invalid window phase')
        return names
    if profile['case'] == FLOOD_CASE:
        names = ['resources.jsonl', 'standard.jsonl']
        stems = [flood_stem(index, kind) for index, kind, _ in flood_slots(profile)]
        names += [name + '-workload.json' for name in stems]
        if phase == 'enabled':
            names += ['witness.jsonl', *[name + '.jsonl' for name in stems]]
        elif phase != 'control':
            raise ValueError('invalid window phase')
        return names
    if profile['case'] == CONCURRENT_CASE:
        names = ['resources.jsonl', 'standard.jsonl', 'workload-target-0.jsonl', 'workload-target-1.jsonl']
        if phase == 'enabled':
            names += ['witness.jsonl', *[f'delivery-{i:02}-target-{target}.jsonl'
                       for i in range(profile['trace']['count']) for target in (0, 1)]]
        elif phase != 'control':
            raise ValueError('invalid window phase')
        return names
    names = stream_names(phase, profile['trace']['count'])
    if profile['case'] == NOISY_CASE:
        names += [role + '.jsonl' for role in NOISE_ROLES]
    return names


def write_envelope(directory, phase, pair, profile, cfg, boot):
    value = {'schemaVersion': 1, 'phase': phase, 'pair': pair, 'profileSHA256': digest(canonical(profile)),
             'sourceSHA256': cfg['sourceSHA256'], 'configurationSHA256': digest(canonical(cfg)),
             'bootID': boot, 'streams': {name: file_digest(directory / name) for name in profile_stream_names(phase, profile)},
             'receiptSHA256': file_digest(directory / 'window.private.json')}
    with (directory / 'envelope.json').open('x') as stream:
        json.dump(value, stream, indent=2)
    return value


def verify_envelope(directory, phase, profile):
    value = json.loads((directory / 'envelope.json').read_text())
    if (value['schemaVersion'] != 1 or value['phase'] != phase or
            value['profileSHA256'] != digest(canonical(profile)) or
            set(value['streams']) != set(profile_stream_names(phase, profile))):
        raise ValueError('observation envelope differs from frozen window')
    for name, expected in value['streams'].items():
        if file_digest(directory / name) != expected:
            raise ValueError('observation stream changed')
    if file_digest(directory / 'window.private.json') != value['receiptSHA256']:
        raise ValueError('window receipt changed')
    return {k: value[k] for k in ('pair', 'sourceSHA256', 'configurationSHA256', 'bootID')}
