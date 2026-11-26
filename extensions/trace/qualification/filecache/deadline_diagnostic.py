"""Interpret a failed operation's timing without promoting it to valid evidence."""

KEYS = {'type', 'schemaVersion', 'sequence', 'waitStartedMonotonicNanos', 'wokeMonotonicNanos',
        'cpuBeforeWaitNanos', 'cpuAfterWakeNanos', 'cpuAfterOperationNanos'}


def validate_deadline_diagnostic(value, last, period_nanos):
    if not isinstance(value, dict) or set(value) != KEYS:
        raise ValueError('invalid deadline diagnostic fields')
    if any(type(value[key]) is not int or not 0 <= value[key] < 2**64 for key in KEYS - {'type'}):
        raise ValueError('invalid deadline diagnostic counters')
    if (value['type'] != 'series-deadline-failure' or value['schemaVersion'] != 1
            or value['sequence'] != last['sequence'] or type(period_nanos) is not int or period_nanos <= 0):
        raise ValueError('deadline diagnostic does not match its operation')
    observation = last['observation']
    wait, wake = value['waitStartedMonotonicNanos'], value['wokeMonotonicNanos']
    due, start, end = last['dueMonotonicNanos'], observation['operationStartedMonotonicNanos'], observation['operationEndedMonotonicNanos']
    before, woke, completed = (value[key] for key in ('cpuBeforeWaitNanos', 'cpuAfterWakeNanos', 'cpuAfterOperationNanos'))
    if not 0 < wait <= wake <= start < end or not before <= woke <= completed:
        raise ValueError('deadline diagnostic clock or CPU counters reset')
    if due <= start and end < due + period_nanos:
        raise ValueError('deadline diagnostic refers to an on-time operation')
    return {'sequence': value['sequence'], 'startLatenessNanos': start - due,
            'wakeLatenessNanos': wake - due, 'setupAfterWakeNanos': start - wake,
            'operationNanos': end - start, 'processCPUAcrossWaitNanos': woke - before,
            'processCPUAfterWakeNanos': completed - woke,
            'scope': 'failed operation only; scheduler, quota and host cause remain unproven'}
