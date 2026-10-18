"""Validate a finite burst's independent byte/call counters, not tracer output."""


def validate_flood(value, count):
    keys = {'schemaVersion', 'mode', 'fileBytes', 'readCalls', 'bytesPerRead', 'readBytes',
            'operationStartedMonotonicNanos', 'operationEndedMonotonicNanos', 'operationNanos'}
    if type(count) is not int or not 1 <= count <= 262144 or not isinstance(value, dict) or set(value) != keys:
        raise ValueError('invalid flood observation fields or count')
    if any(type(value[key]) is not int or not 0 <= value[key] < 2**64 for key in keys - {'mode'}):
        raise ValueError('invalid flood observation counters')
    if (value['schemaVersion'] != 1 or value['mode'] != 'flood' or value['fileBytes'] != 8 << 20
            or value['readCalls'] != count or value['bytesPerRead'] != 64 or value['readBytes'] != count * 64):
        raise ValueError('flood operation contract changed')
    started, ended = value['operationStartedMonotonicNanos'], value['operationEndedMonotonicNanos']
    if not 0 < started < ended or ended - started != value['operationNanos'] or ended - started > 10000000000:
        raise ValueError('flood timing is invalid or exceeded its bound')
    return dict(value)


def validate_paced_flood(value, count):
    if (not isinstance(value, dict) or type(value.get('schemaVersion')) is not int
            or value['schemaVersion'] != 2 or value.get('mode') != 'paced-flood'):
        raise ValueError('invalid paced flood schema')
    period = value.get('readPeriodNanos')
    if type(period) is not int or period != 500000 or type(count) is not int or not 1 <= count <= 12000:
        raise ValueError('invalid paced flood schedule')
    normalized = {key: item for key, item in value.items() if key != 'readPeriodNanos'}
    normalized.update(schemaVersion=1, mode='flood')
    validate_flood(normalized, count)
    if value['operationNanos'] < (count - 1) * period:
        raise ValueError('paced workload ran faster than its declared schedule')
    return dict(value)
