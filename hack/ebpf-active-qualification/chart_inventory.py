"""Verify the approved host decoder before inspecting a rendered Helm chart."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat

from local_case import command

MAX_BINARY = 128 << 20


def verify_inventory(binding):
    if (type(binding) is not dict or set(binding) != {'path', 'sha256'}
            or not isinstance(binding['path'], str) or not Path(binding['path']).is_absolute()
            or not isinstance(binding['sha256'], str)
            or not re.fullmatch('[a-f0-9]{64}', binding['sha256'])):
        raise ValueError('exact prebuilt chart inventory binding required')
    fd = os.open(binding['path'], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        before = os.fstat(source.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != os.getuid()
                or before.st_mode & 0o022 or not before.st_mode & stat.S_IXUSR
                or not 0 < before.st_size <= MAX_BINARY):
            raise ValueError('chart inventory must be a bounded owned executable without shared write access')
        value = hashlib.sha256()
        size = 0
        while chunk := source.read(1 << 20):
            size += len(chunk)
            if size > MAX_BINARY:
                raise ValueError('chart inventory exceeds bound')
            value.update(chunk)
        after = os.fstat(source.fileno())
    fields = ('st_dev', 'st_ino', 'st_size', 'st_mtime_ns', 'st_ctime_ns', 'st_mode', 'st_uid')
    if (size != before.st_size or any(getattr(before, key) != getattr(after, key) for key in fields)
            or value.hexdigest() != binding['sha256']):
        raise ValueError('chart inventory differs from approved build')
    return binding['path']


def inventory(binding, rendered):
    executable = verify_inventory(binding)
    raw = command([executable], rendered, timeout=15)
    verify_inventory(binding)
    return json.loads(raw)
