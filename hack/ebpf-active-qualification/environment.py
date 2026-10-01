"""Read-only kernel accounting evidence; no boot settings or rlimits are changed."""
import re
import shlex
import hashlib

from samples import require


def memory_configuration(config, command_line, release):
    version = re.match(r'^(\d+)\.(\d+)\.', release)
    require(version is not None and tuple(map(int, version.groups())) >= (5, 11),
            'kernel predates BPF memory-cgroup accounting')
    settings = {}
    for line in config.splitlines():
        if line.startswith('CONFIG_') and '=' in line:
            key, value = line.split('=', 1)
            require(key not in settings, 'duplicate kernel setting')
            settings[key] = value
        elif line.startswith('# CONFIG_') and line.endswith(' is not set'):
            key = line[2:-11]
            require(key not in settings, 'duplicate kernel setting')
            settings[key] = 'n'
    require(all(settings.get(key) == 'y' for key in ('CONFIG_MEMCG', 'CONFIG_CGROUP_BPF', 'CONFIG_BPF_SYSCALL')),
            'required cgroup/BPF kernel configuration unavailable')
    require(settings.get('CONFIG_MEMCG_KMEM') != 'n', 'kernel-memory accounting disabled in build')
    try:
        arguments = shlex.split(command_line)
    except ValueError:
        raise ValueError('ambiguous kernel command line') from None
    for argument in arguments:
        if argument.startswith('cgroup.memory='):
            require(not {'nobpf', 'nokmem', 'nosocket'} & set(argument.partition('=')[2].split(',')),
                    'boot options disable memory accounting')
        if argument.startswith('cgroup_disable='):
            require('memory' not in argument.partition('=')[2].split(','), 'memory controller disabled at boot')
    return {'kernelVersion': release, 'configSHA256': hashlib.sha256(config.encode()).hexdigest(),
            'commandLineSHA256': hashlib.sha256(command_line.encode()).hexdigest(),
            'settings': {key: settings.get(key, 'not-present') for key in
                         ('CONFIG_MEMCG', 'CONFIG_MEMCG_KMEM', 'CONFIG_CGROUP_BPF', 'CONFIG_BPF_SYSCALL')},
            'disablingBootOptionsAbsent': True,
            'basis': 'Linux5.11+ memory-cgroup BPF accounting; require observed worker placement in tracer cgroup'}


def kernel_accounting(runtime):
    config = runtime.kernel_configuration()
    release = runtime.exec(['uname', '-r']).decode().strip()
    arguments = runtime.exec(['cat', '/proc/cmdline']).decode().strip()
    require(runtime.exec(['stat', '-f', '-c', '%T', '/sys/fs/cgroup']).decode().strip() == 'cgroup2fs',
            'unified memory hierarchy required')
    return memory_configuration(config, arguments, release)
