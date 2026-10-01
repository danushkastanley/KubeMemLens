"""Validate continuous owned-file read intervals; never infer trace coverage."""
import json

MAX_READS = 20000000000
BATCH_READS = 4096
SECOND = 1000000000


def require(condition, message):
    if not condition:
        raise ValueError(message)


def integer(value, minimum=0, maximum=2**63 - 1):
    require(type(value) is int and minimum <= value <= maximum, 'invalid pressure counter')


def document(raw):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'duplicate pressure field')
            value[key] = item
        return value
    return json.loads(raw, object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(ValueError('invalid pressure JSON')))


def validate_pressure(raw, seconds):
    integer(seconds, 1, 1800)
    require(type(raw) is str and len(raw.encode()) <= 1 << 20 and raw.endswith('\n'),
            'pressure output absent, truncated or oversized')
    lines = raw.splitlines()
    require(len(lines) == seconds + 1 and all(len(line.encode()) <= 4096 for line in lines),
            'incomplete pressure interval stream')
    header = document(lines[0])
    require(type(header) is dict and set(header) == {'type', 'schemaVersion', 'seconds', 'fileBytes',
            'bytesPerRead', 'batchReadCalls', 'maximumReadCalls', 'monotonicBeforeNanos',
            'wallNanos', 'monotonicAfterNanos'}, 'invalid pressure header')
    for key, value in header.items():
        if key != 'type':
            integer(value)
    require(header['type'] == 'pressure-start' and header['schemaVersion'] == 1
            and header['seconds'] == seconds and header['fileBytes'] == 8 << 20
            and header['bytesPerRead'] == 64 and header['batchReadCalls'] == BATCH_READS
            and header['maximumReadCalls'] == MAX_READS, 'pressure contract changed')
    before, started = header['monotonicBeforeNanos'], header['monotonicAfterNanos']
    require(0 < before <= started <= before + 1000000 and header['wallNanos'] > 0,
            'pressure clock alignment invalid')
    previous, total, rows = started, 0, []
    for index, line in enumerate(lines[1:]):
        row = document(line)
        require(type(row) is dict and set(row) == {'sequence', 'startedMonotonicNanos',
                'endedMonotonicNanos', 'readCalls', 'readBytes', 'wallNanos',
                'clockAfterMonotonicNanos'}, 'invalid pressure interval')
        for value in row.values():
            integer(value)
        due = started + (index + 1) * SECOND
        require(row['sequence'] == index and row['startedMonotonicNanos'] == previous
                and due <= row['endedMonotonicNanos'] <= due + 100000000,
                'pressure interval ordering or deadline changed')
        require(row['readCalls'] > 0 and row['readCalls'] % BATCH_READS == 0
                and row['readBytes'] == row['readCalls'] * 64, 'pressure I/O accounting invalid')
        require(row['endedMonotonicNanos'] <= row['clockAfterMonotonicNanos']
                <= row['endedMonotonicNanos'] + 5000000 and row['wallNanos'] > 0,
                'pressure interval clock alignment invalid')
        first_low, first_high = header['wallNanos'] - started, header['wallNanos'] - before
        low = row['wallNanos'] - row['clockAfterMonotonicNanos']
        high = row['wallNanos'] - row['endedMonotonicNanos']
        require(low <= first_high + 5000000 and high >= first_low - 5000000,
                'pressure wall and monotonic clocks drifted')
        total += row['readCalls']
        require(total <= MAX_READS, 'pressure read ceiling exceeded')
        previous = row['endedMonotonicNanos']
        rows.append(row)
    return {'header': header, 'intervals': rows, 'totalReadCalls': total,
            'totalReadBytes': total * 64, 'operationNanos': previous - started,
            'scope': 'continuous producer receipts only; attachment and resource evidence required'}


def validate_pressure_session(raw, seconds):
    require(type(raw) is str and len(raw.encode()) <= 1 << 20 and '\n' in raw,
            'missing bounded pressure readiness')
    line, rest = raw.split('\n', 1)
    ready = document(line)
    require(type(ready) is dict and set(ready) == {'ready'} and ready['ready'] is True,
            'pressure readiness invalid')
    return validate_pressure(rest, seconds)
