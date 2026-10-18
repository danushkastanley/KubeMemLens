"""Conservative rates from two private, identity-bound storage snapshots."""
from storage_counters import block_statistics, io_delta, io_statistics, work_mount


def interval(before, after):
    for row in (before, after):
        start, end = row['readStartedNanos'], row['readEndedNanos']
        if type(start) is not int or type(end) is not int or start < 0 or end < start:
            raise ValueError('invalid storage read interval')
    minimum = after['readStartedNanos'] - before['readEndedNanos']
    maximum = after['readEndedNanos'] - before['readStartedNanos']
    if minimum <= 0:
        raise ValueError('storage read intervals overlap or clocks changed')
    return minimum, maximum


def rate_bounds(count, span):
    minimum, maximum = span
    scaled = count * 1000000000
    return {'lower': scaled // maximum, 'upper': (scaled + minimum - 1) // minimum}


def compare_storage(before, after):
    for key in ('schemaVersion', 'private', 'bootID', 'observerID'):
        if before[key] != after[key]:
            raise ValueError('storage observer or host identity changed')
    if before['schemaVersion'] != 1 or before['private'] is not True:
        raise ValueError('private storage snapshot required')
    span = interval(before, after)
    if not 1 <= len(before['groups']) <= 12 or before['groups'].keys() != after['groups'].keys():
        raise ValueError('storage fixture inventory changed')
    groups = {}
    for key, first in before['groups'].items():
        last = after['groups'][key]
        if any(first[field] != last[field] for field in ('identity', 'role', 'cgroupInode', 'mount')):
            raise ValueError('storage fixture or mount changed')
        for row, snapshot in ((first, before), (last, after)):
            if io_statistics(row['ioRaw']) != row['io'] or work_mount(row['mountRaw']) != row['mount']:
                raise ValueError('storage projection differs from raw observation')
            if not snapshot['readStartedNanos'] <= row['readStartedNanos'] <= row['readEndedNanos'] <= snapshot['readEndedNanos']:
                raise ValueError('fixture storage read escaped snapshot interval')
        group_span = interval(first, last)
        change = io_delta(first['io'], last['io'])
        if change['state'] == 'observed':
            change['rates'] = {device: {
                'readWriteBytesPerSecond': rate_bounds(values['rbytes'] + values['wbytes'], group_span),
                'readWriteIOsPerSecond': rate_bounds(values['rios'] + values['wios'], group_span)}
                for device, values in change['devices'].items()}
        groups[key] = change
    devices = {}
    if before['devices'].keys() != after['devices'].keys():
        return {'state': 'unproven', 'private': True, 'reason': 'block device inventory changed', 'groups': groups}
    for device, first in before['devices'].items():
        last = after['devices'][device]
        for row in (first, last):
            if block_statistics(row['raw']) != row['statistics']:
                raise ValueError('block projection differs from raw observation')
        if first['statistics']['state'] != 'observed' or last['statistics']['state'] != 'observed':
            devices[device] = {'state': 'unproven', 'reason': 'block accounting unavailable'}
            continue
        if (any(first[key] != last[key] for key in ('sysfsPath', 'sysfsInode'))
                or first.get('parentDevice') != last.get('parentDevice')):
            raise ValueError('block device identity changed')
        start, end = first['statistics']['counters'], last['statistics']['counters']
        if start.keys() != end.keys() or any(end[key] < value for key, value in start.items() if key != 'inFlight'):
            raise ValueError('block counters changed or reset')
        delta = {key: end[key] - value for key, value in start.items() if key != 'inFlight'}
        devices[device] = {'state': 'observed', 'counters': delta,
                           'readWriteBytesPerSecond': rate_bounds(512 * (delta['readSectors'] + delta['writeSectors']), span),
                           'readWriteIOsPerSecond': rate_bounds(delta['readIOs'] + delta['writeIOs'], span),
                           'parentDevice': first.get('parentDevice')}
    return {'state': 'observations-compared', 'private': True, 'groups': groups, 'devices': devices,
            'qualification': 'none; do not sum partition and parent counters or infer capacity from missing accounting'}
