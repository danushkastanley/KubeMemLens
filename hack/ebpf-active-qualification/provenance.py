"""Bind every numeric stream to one window and its independently frozen inputs."""
import hashlib
import json

from local_case import canonical, digest

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


def write_envelope(directory, phase, pair, profile, cfg, boot):
    value = {'schemaVersion': 1, 'phase': phase, 'pair': pair, 'profileSHA256': digest(canonical(profile)),
             'sourceSHA256': cfg['sourceSHA256'], 'configurationSHA256': digest(canonical(cfg)),
             'bootID': boot, 'streams': {name: file_digest(directory / name) for name in stream_names(phase, profile['trace']['count'])},
             'receiptSHA256': file_digest(directory / 'window.private.json')}
    with (directory / 'envelope.json').open('x') as stream:
        json.dump(value, stream, indent=2)
    return value


def verify_envelope(directory, phase, profile):
    value = json.loads((directory / 'envelope.json').read_text())
    if (value['schemaVersion'] != 1 or value['phase'] != phase or
            value['profileSHA256'] != digest(canonical(profile)) or
            set(value['streams']) != set(stream_names(phase, profile['trace']['count']))):
        raise ValueError('observation envelope differs from frozen window')
    for name, expected in value['streams'].items():
        if file_digest(directory / name) != expected:
            raise ValueError('observation stream changed')
    if file_digest(directory / 'window.private.json') != value['receiptSHA256']:
        raise ValueError('window receipt changed')
    return {k: value[k] for k in ('pair', 'sourceSHA256', 'configurationSHA256', 'bootID')}
