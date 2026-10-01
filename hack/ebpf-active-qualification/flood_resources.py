"""Replay flood containment observations separately from normal performance gates."""
from copy import deepcopy

from resources import working_set
from samples import exact, integer, require, validate_window

CAP_FIELDS = {'cpu', 'cpuBurstUsec', 'memoryMaxBytes', 'memoryPeakBytes', 'pidsMax'}


def observed(value, *, unlimited=False):
    require(type(value) is dict, 'missing controller observation')
    state = value.get('state')
    if state == 'observed':
        exact(value, {'state', 'value'})
        integer(value['value'])
        return value['value']
    require(state in ({'unavailable', 'unlimited'} if unlimited else {'unavailable'}),
            'invalid controller availability')
    exact(value, {'state'})
    return None


def containment(value):
    exact(value, CAP_FIELDS)
    cpu = value['cpu']
    exact(cpu, {'maximum', 'periodUsec'})
    maximum = observed(cpu['maximum'], unlimited=True)
    require(cpu['maximum']['state'] != 'unavailable', 'CPU bandwidth source missing')
    require(maximum is None or maximum > 0, 'invalid CPU bandwidth')
    integer(cpu['periodUsec'], 1)
    observed(value['cpuBurstUsec'])
    observed(value['memoryMaxBytes'], unlimited=True)
    observed(value['memoryPeakBytes'])
    observed(value['pidsMax'], unlimited=True)


def validate_containment_window(rows, seconds, roles):
    """Keep schema 2 validation strict; only this entry point accepts schema 3."""
    require(type(rows) is list, 'missing containment window')
    ordinary = []
    for row in rows:
        require(type(row) is dict and type(row.get('schemaVersion')) is int
                and row['schemaVersion'] == 3, 'containment schema required')
        value = deepcopy(row)
        value['schemaVersion'] = 2
        require(type(value.get('groups')) is dict, 'missing group observations')
        for group in value['groups'].values():
            require(type(group) is dict and 'containment' in group, 'missing containment fields')
            containment(group.pop('containment'))
        ordinary.append(value)
    validate_window(ordinary, seconds, roles)
    return ordinary


def flood_resources(rows, seconds, roles, expected):
    """Expected CPU/memory caps must come from the independently frozen profile.

    memory.peak is a lifetime high-water mark, read without resetting it. It is
    deliberately not relabelled as a window-local peak. CPU quota is configured
    bandwidth, not a measured instantaneous limit or proof of scheduler class.
    """
    ordinary = validate_containment_window(rows, seconds, roles)
    exact(expected, {'node', 'api'})
    results = {}
    for role, caps in expected.items():
        exact(caps, {'cpuQuotaUsec', 'cpuPeriodUsec', 'cpuBurstUsec', 'memoryMaxBytes'})
        for key, number in caps.items():
            integer(number, 0 if key == 'cpuBurstUsec' else 1)
        require(role in roles, 'installation role missing')
        series = [row['groups'][role] for row in rows]
        limits = [group['containment'] for group in series]
        initial = {k: v for k, v in limits[0].items() if k != 'memoryPeakBytes'}
        require(all({k: v for k, v in value.items() if k != 'memoryPeakBytes'} == initial
                    for value in limits), 'cgroup caps or availability changed during window')
        expected_observations = {
            'cpu': {'maximum': {'state': 'observed', 'value': caps['cpuQuotaUsec']},
                    'periodUsec': caps['cpuPeriodUsec']},
            'cpuBurstUsec': {'state': 'observed', 'value': caps['cpuBurstUsec']},
            'memoryMaxBytes': {'state': 'observed', 'value': caps['memoryMaxBytes']},
        }
        matched = all(initial[key] == value for key, value in expected_observations.items())
        peaks = [observed(value['memoryPeakBytes']) for value in limits]
        complete = all(peak is not None for peak in peaks)
        if complete:
            require(all(b >= a for a, b in zip(peaks, peaks[1:])), 'memory lifetime peak reset')
            require(all(peak >= group['memoryCurrent'] for peak, group in zip(peaks, series)),
                    'lifetime peak below prior current reading')
        pids_max = observed(initial['pidsMax'], unlimited=True)
        pids_known = initial['pidsMax']['state'] != 'unavailable'
        pids_within = pids_known and (pids_max is None or all(group['pids'] <= pids_max for group in series))
        results[role] = {
            'configuredCaps': initial, 'configuredCPUMemoryCapsMatch': matched,
            'memoryLifetimePeakAvailable': complete,
            'memoryLifetimePeakBytes': max(peaks) if complete else None,
            'sampledPeakWorkingSetBytes': max(working_set(group) for group in series),
            'sampledPeakPIDs': max(group['pids'] for group in series),
            'pidsWithinObservedSetting': pids_within,
            'cpuUsec': series[-1]['cpu']['usage_usec'] - series[0]['cpu']['usage_usec'],
            'throttledUsec': series[-1]['cpu']['throttled_usec'] - series[0]['cpu']['throttled_usec'],
            'memoryEventsDelta': {key: series[-1]['memoryEvents'][key] - series[0]['memoryEvents'][key]
                                  for key in ('max', 'high', 'oom', 'oom_kill')},
            'observedContainmentPassed': matched and complete and pids_within
                                         and max(peaks) <= caps['memoryMaxBytes'],
        }
    return {'scope': 'cgroup observations only; no paired flood qualification', 'services': results,
            'windowNanos': ordinary[-1]['elapsedNanos'],
            'observedContainmentPassed': all(value['observedContainmentPassed'] for value in results.values()),
            'limitations': ['CPU bandwidth setting is not scheduler-class or instantaneous-rate proof',
                            'memory peak includes the cgroup lifetime before the measured window',
                            'one-second reads cannot exclude cap changes between observations']}
