"""Run the unchanged five-pair idle protocol on an explicitly bound EKS host."""
import argparse
import json
import os
from pathlib import Path
import signal
import stat

from eks_host_runtime import EKSHostRuntime
from idle_campaign import Campaign


class EKSIdleCampaign(Campaign):
    runtime_class = EKSHostRuntime


def private_configuration(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate private configuration field')
            result[key] = value
        return result
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError('configuration must be a private owned regular file')
        data = source.read(32769)
    if len(data) > 32768:
        raise ValueError('configuration exceeds bound')
    value = json.loads(data, object_pairs_hook=unique,
        parse_constant=lambda _: (_ for _ in ()).throw(ValueError('invalid JSON constant')))
    if type(value) is not dict or 'providerExecution' not in value:
        raise ValueError('explicit EKS execution configuration required')
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--acknowledge-owned-eks-host', action='store_true', required=True)
    args = parser.parse_args()
    os.umask(0o077)
    cfg = private_configuration(args.config)
    def interrupted(_signal, _frame):
        raise InterruptedError('provider campaign interrupted; restore owned services')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(12600)
    try:
        EKSIdleCampaign(cfg, args.output).run()
    finally:
        signal.alarm(0)


if __name__ == '__main__':
    main()
