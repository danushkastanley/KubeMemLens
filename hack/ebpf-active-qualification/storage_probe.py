"""Read-only private before/after storage evidence for owned workload sizing."""
from copy import deepcopy
from pathlib import PurePosixPath
import time
import uuid

from storage_counters import block_statistics, device_number, io_statistics, work_mount, unsigned

# Fixed script and positional path: no interpolation of paths into shell code.
READ_OPTIONAL = 'if [ -e "$1" ]; then printf "present\\n"; head -c 65537 -- "$1"; else printf "absent\\n"; fi'


def read_optional(runtime, path):
    raw = runtime.exec(['sh', '-ec', READ_OPTIONAL, '--', path])
    state, separator, body = raw.partition(b'\n')
    if not separator or state not in (b'present', b'absent') or len(body) > 65536:
        raise ValueError('invalid or oversized storage source')
    if state == b'absent':
        if body:
            raise ValueError('absent storage source has data')
        return None
    return body.decode()


class StorageProbe:
    def __init__(self, case, bindings):
        if not 1 <= len(bindings) <= 12:
            raise ValueError('bounded selected/noisy fixture inventory required')
        self.case, self.runtime = case, case.runtime
        self.bindings = deepcopy(bindings)
        roles, inodes = set(), set()
        for key, binding in self.bindings.items():
            namespace, name = key.split('/')
            role = binding['group']['role']
            if (namespace not in case.namespaces or name != binding['podName']
                    or role not in {'selected', 'selected-peer', *[f'nonselected-{i}' for i in range(10)]}
                    or role in roles or binding['group']['inode'] in inodes):
                raise ValueError('unowned or duplicate storage fixture')
            roles.add(role)
            inodes.add(binding['group']['inode'])
        self.verify()
        self.boot = self.boot_id()
        self.observer = str(uuid.uuid4())

    def boot_id(self):
        value = self.runtime.exec(['cat', '/proc/sys/kernel/random/boot_id']).decode().strip()
        if str(uuid.UUID(value)) != value:
            raise ValueError('invalid storage host boot identity')
        return value

    def verify(self):
        for key, binding in self.bindings.items():
            namespace, name = key.split('/')
            if self.case.fixture(namespace, name, binding['group']['role']) != binding:
                raise ValueError('storage fixture lifetime or specification changed')

    def device(self, device):
        device_number(device)
        root = '/sys/dev/block/' + device
        raw = read_optional(self.runtime, root + '/stat')
        result = {'raw': raw, 'statistics': block_statistics(raw)}
        if raw is None:
            return result
        inode = unsigned(self.runtime.exec(['stat', '-L', '-c', '%i', '--', root]).decode().strip())
        raw = read_optional(self.runtime, root + '/stat')
        if raw is None:
            raise ValueError('block device disappeared during read')
        result = {'raw': raw, 'statistics': block_statistics(raw), 'sysfsInode': inode}
        resolved = self.runtime.exec(['readlink', '-f', '--', root]).decode().strip()
        if not resolved.startswith('/sys/devices/') or str(PurePosixPath(resolved)) != resolved or '..' in PurePosixPath(resolved).parts:
            raise ValueError('block device resolved outside sysfs')
        result['sysfsPath'] = resolved
        partition = read_optional(self.runtime, root + '/partition')
        if partition is not None:
            if unsigned(partition.strip()) == 0:
                raise ValueError('invalid partition identity')
            parent = read_optional(self.runtime, str(PurePosixPath(resolved).parent / 'dev'))
            if parent is None:
                raise ValueError('partition parent device unavailable')
            result['parentDevice'] = device_number(parent.strip())
            if result['parentDevice'] == device:
                raise ValueError('partition refers to itself')
        if unsigned(self.runtime.exec(['stat', '-L', '-c', '%i', '--', root]).decode().strip()) != inode:
            raise ValueError('block device replaced during read')
        return result

    def snapshot(self):
        self.verify()
        if self.boot_id() != self.boot:
            raise ValueError('storage host rebooted')
        started = time.monotonic_ns()
        groups, devices = {}, {}
        for key, binding in self.bindings.items():
            group = binding['group']
            # fixture() checks PID/start/executable/CRI/cgroup ownership around
            # the whole read. Keep mount and IO records private for replay.
            first = time.monotonic_ns()
            raw = read_optional(self.runtime, group['path'] + '/io.stat')
            mount_raw = self.runtime.exec(['cat', f'/proc/{binding["pid"]}/mountinfo']).decode()
            if len(mount_raw.encode()) > 65536:
                raise ValueError('mount inventory exceeds bound')
            mount = work_mount(mount_raw)
            counters = io_statistics(raw)
            groups[key] = {'identity': binding['identity'], 'role': group['role'], 'cgroupInode': group['inode'],
                           'readStartedNanos': first,
                           'ioRaw': raw, 'io': counters, 'mountRaw': mount_raw, 'mount': mount,
                           'cpuRaw': read_optional(self.runtime, group['path'] + '/cpu.stat'),
                           'pressureRaw': read_optional(self.runtime, group['path'] + '/io.pressure')}
            groups[key]['readEndedNanos'] = time.monotonic_ns()
            for device in sorted({mount['device'], *counters.get('devices', {})}):
                if device not in devices:
                    if len(devices) >= 16:
                        raise ValueError('block device inventory exceeds bound')
                    devices[device] = self.device(device)
        parents = {value['parentDevice'] for value in devices.values() if 'parentDevice' in value}
        for parent in sorted(parents - devices.keys()):
            if len(devices) >= 16:
                raise ValueError('block device inventory exceeds bound')
            devices[parent] = self.device(parent)
        self.verify()
        if self.boot_id() != self.boot:
            raise ValueError('storage host rebooted during read')
        return {'schemaVersion': 1, 'private': True, 'bootID': self.boot, 'observerID': self.observer,
                'readStartedNanos': started, 'readEndedNanos': time.monotonic_ns(),
                'groups': groups, 'devices': devices,
                'scope': 'before/after storage sizing observations; no workload or qualification verdict'}
