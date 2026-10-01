"""Decode Linux storage counters without converting unavailable data to zero."""
import re

IO_FIELDS = {'rbytes', 'wbytes', 'rios', 'wios', 'dbytes', 'dios'}
BLOCK_FIELDS = ('readIOs', 'readMerges', 'readSectors', 'readMilliseconds',
                'writeIOs', 'writeMerges', 'writeSectors', 'writeMilliseconds',
                'inFlight', 'ioMilliseconds', 'weightedIOMilliseconds',
                'discardIOs', 'discardMerges', 'discardSectors', 'discardMilliseconds',
                'flushIOs', 'flushMilliseconds')


def unsigned(value):
    if not re.fullmatch(r'[0-9]{1,20}', value) or int(value) > (1 << 64) - 1:
        raise ValueError('invalid storage counter')
    return int(value)


def device_number(value):
    if not re.fullmatch(r'(?:0|[1-9][0-9]{0,9}):(?:0|[1-9][0-9]{0,9})', value):
        raise ValueError('invalid block device number')
    if any(int(part) > (1 << 32) - 1 for part in value.split(':')):
        raise ValueError('block device number exceeds bound')
    return value


def io_statistics(raw):
    if raw is None:
        return {'state': 'unavailable'}
    if not raw.strip():
        return {'state': 'empty'}
    devices = {}
    for line in raw.splitlines():
        fields = line.split()
        if not fields or len(devices) >= 64:
            raise ValueError('invalid I/O device inventory')
        device = device_number(fields[0])
        values = {}
        for field in fields[1:]:
            key, separator, value = field.partition('=')
            if not separator or not re.fullmatch('[a-z_]{1,32}', key) or key in values:
                raise ValueError('invalid or duplicate I/O field')
            values[key] = unsigned(value)
        if device in devices or not IO_FIELDS <= values.keys():
            raise ValueError('duplicate device or missing I/O counters')
        devices[device] = values
    return {'state': 'observed', 'devices': devices}


def block_statistics(raw):
    if raw is None:
        return {'state': 'unavailable'}
    fields = raw.split()
    if len(fields) not in (11, 15, 17):
        raise ValueError('unsupported block counter field inventory')
    return {'state': 'observed', 'counters': dict(zip(BLOCK_FIELDS, map(unsigned, fields))),
            'sectorBytes': 512}


def work_mount(raw):
    matches = []
    for line in raw.splitlines():
        fields = line.split()
        if len(fields) < 10 or '-' not in fields:
            raise ValueError('invalid mount inventory')
        separator = fields.index('-')
        if separator < 6 or len(fields) != separator + 4:
            raise ValueError('invalid mount record')
        if fields[4] == '/work':
            matches.append({'mountID': unsigned(fields[0]), 'parentID': unsigned(fields[1]),
                            'device': device_number(fields[2]), 'root': fields[3], 'mountPoint': fields[4],
                            'filesystem': fields[separator + 1], 'source': fields[separator + 2]})
    if len(matches) != 1:
        raise ValueError('one explicit fixture work mount required')
    return matches[0]


def io_delta(before, after):
    if before['state'] != 'observed' or after['state'] != 'observed':
        return {'state': 'unproven', 'beforeState': before['state'], 'afterState': after['state']}
    if before['devices'].keys() != after['devices'].keys():
        return {'state': 'unproven', 'reason': 'device inventory changed'}
    result = {}
    for device, values in before['devices'].items():
        final = after['devices'][device]
        if any(final[key] < values[key] for key in IO_FIELDS):
            raise ValueError('I/O counter reset')
        # Extra latency/depth fields may be gauges, so never subtract them as IO.
        result[device] = {key: final[key] - values[key] for key in sorted(IO_FIELDS)}
    return {'state': 'observed', 'devices': result}
