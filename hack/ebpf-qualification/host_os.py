"""Read the two host-distribution fields using AL2023's system Python 3.9."""
from pathlib import Path
import shlex


def read_release(path=Path('/etc/os-release')):
    with path.open('rb') as stream:
        raw = stream.read(16385)
    if len(raw) > 16384:
        raise ValueError('host OS release exceeds bound')
    fields = {}
    for line in raw.decode('utf-8').splitlines():
        key, separator, value = line.partition('=')
        if key not in ('ID', 'VERSION_ID'):
            continue
        if not separator or key in fields:
            raise ValueError('ambiguous host distribution field')
        tokens = shlex.split(value, comments=True, posix=True)
        if len(tokens) != 1:
            raise ValueError('invalid host distribution value')
        fields[key] = tokens[0]
    if set(fields) != {'ID', 'VERSION_ID'}:
        raise ValueError('host distribution fields missing')
    return fields
