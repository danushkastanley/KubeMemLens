"""Independently replay bounded numeric verifier windows; no log contents."""
from pathlib import Path
import re

from samples import exact, integer, require, strict_json

SECOND = 1_000_000_000
ROW = {'schemaVersion', 'index', 'startedNanos', 'cutoffNanos', 'readStartedNanos',
       'readEndedNanos', 'observerCPUUsec', 'observerPeakRSSBytes', 'startupDiscarded',
       'perf', 'observation', 'probeCounts', 'binding', 'captureClosed',
       'closeStartedNanos', 'closeEndedNanos'}
TOTALS = {'completedCalls', 'rejectedCalls', 'logFailures', 'durationNanos',
          'maximumNanos', 'finalizedLogBytes', 'maximumLogBytes', 'pendingCalls', 'unassociatedLogs'}
PERF = {'events', 'enabledNanos', 'runningNanos', 'lostSamples', 'minimumEnabledNanos', 'descriptors'}
ROLES = {'enter', 'return', 'log'}


def read_verifier_samples(path):
    path = Path(path)
    require(path.stat().st_size <= 16 << 20, 'verifier stream exceeds byte bound')
    rows = []
    with path.open('rb') as stream:
        while line := stream.readline(8193):
            require(len(line) <= 8192 and line.endswith(b'\n') and len(rows) < 1801,
                    'verifier record exceeds bounds or is incomplete')
            rows.append(strict_json(line))
    return rows


def validate_binding(value):
    exact(value, {'owner', 'btfSHA256', 'anchorSHA256', 'cgroupInode', 'formatSHA256'})
    require(type(value['owner']) is str and re.fullmatch('[0-9a-f]{32}', value['owner']),
            'invalid verifier owner nonce')
    exact(value['formatSHA256'], ROLES)
    for digest in [value['btfSHA256'], value['anchorSHA256'], *value['formatSHA256'].values()]:
        require(type(digest) is str and re.fullmatch('[0-9a-f]{64}', digest), 'invalid verifier binding hash')
    integer(value['cgroupInode'], 1)


def totals(value, elapsed):
    exact(value, TOTALS)
    for item in value.values():
        integer(item)
    calls = integer(value['completedCalls'], 0, 4096)
    integer(value['pendingCalls'], 0, 64)
    require(calls + value['pendingCalls'] <= 4096, 'verifier call capacity exceeded')
    integer(value['unassociatedLogs'], 0, 32768)
    for key in ('rejectedCalls', 'logFailures'):
        require(value[key] <= calls, 'verifier outcomes exceed completed calls')
    for sum_key, max_key, bound in (('durationNanos', 'maximumNanos', min(elapsed, 30 * SECOND)),
                                    ('finalizedLogBytes', 'maximumLogBytes', 2**32 - 1)):
        maximum = integer(value[max_key], 0, bound)
        require(maximum <= value[sum_key] <= calls * maximum, 'verifier sum or maximum impossible')
    require(calls == 0 or value['durationNanos'] >= calls * 2,
            'completed verifier calls require ordered enter, log and return timestamps')


def monotonic(previous, current, fields, label):
    require(all(current[key] >= previous[key] for key in fields), label + ' counters reset')


def interval(previous, current):
    old, new = previous['observation'], current['observation']
    monotonic(old, new, TOTALS - {'pendingCalls'}, 'verifier')
    delta = {key: new[key] - old[key] for key in TOTALS - {'pendingCalls', 'maximumNanos', 'maximumLogBytes'}}
    calls = delta['completedCalls']
    require(delta['rejectedCalls'] <= calls and delta['logFailures'] <= calls,
            'verifier interval outcomes exceed calls')
    require(old['pendingCalls'] - new['pendingCalls'] <= calls, 'pending verifier call disappeared')
    for sum_key, max_key in (('durationNanos', 'maximumNanos'), ('finalizedLogBytes', 'maximumLogBytes')):
        require(delta[sum_key] <= calls * new[max_key], 'verifier interval sum exceeds maximum')
        if new[max_key] > old[max_key]:
            require(calls > 0 and delta[sum_key] >= new[max_key], 'verifier maximum changed without a matching call')
    require(delta['durationNanos'] >= calls * 2, 'verifier interval has impossible duration')
    return delta


def verifier_window(rows, seconds, cpu_count, binding):
    """The enclosing receipt must also prove source, scope, process exit and cleanup."""
    integer(seconds, 1, 1800)
    integer(cpu_count, 1, 64)
    validate_binding(binding)
    require(type(rows) is list and len(rows) == seconds + 1, 'incomplete verifier window')
    intervals = []
    for index, row in enumerate(rows):
        exact(row, ROW)
        require(type(row['schemaVersion']) is int and row['schemaVersion'] == 1, 'invalid verifier schema')
        require(type(row['index']) is int and row['index'] == index, 'verifier sample order changed')
        validate_binding(row['binding'])
        require(row['binding'] == binding, 'verifier source or owner binding changed')
        for key in ('startedNanos', 'cutoffNanos', 'readStartedNanos', 'readEndedNanos', 'observerPeakRSSBytes'):
            integer(row[key], 1)
        for key in ('observerCPUUsec', 'startupDiscarded', 'closeStartedNanos', 'closeEndedNanos'):
            integer(row[key])
        require(row['startedNanos'] == rows[0]['startedNanos'] and
                row['cutoffNanos'] == row['startedNanos'] + index * SECOND, 'verifier clock changed')
        require(100_000_000 <= row['readStartedNanos'] - row['cutoffNanos'] <= 350_000_000,
                'verifier sample exceeded reorder or deadline bound')
        require(0 <= row['readEndedNanos'] - row['readStartedNanos'] <= 50_000_000,
                'verifier read exceeded bound')
        require(type(row['captureClosed']) is bool and row['captureClosed'] == (index == seconds),
                'verifier final cleanup absent or premature')
        if index == seconds:
            require(row['closeEndedNanos'] >= row['closeStartedNanos'] >= row['readEndedNanos'],
                    'verifier cleanup clock invalid')
        else:
            require(row['closeStartedNanos'] == row['closeEndedNanos'] == 0, 'unexpected verifier cleanup timestamps')
        perf = row['perf']
        exact(perf, PERF)
        for value in perf.values():
            integer(value)
        require(perf['descriptors'] == cpu_count * 3 and perf['lostSamples'] == 0 and
                perf['enabledNanos'] == perf['runningNanos'], 'verifier perf coverage incomplete')
        require(perf['enabledNanos'] >= perf['descriptors'] * perf['minimumEnabledNanos'] and
                (perf['events'] == 0 or perf['enabledNanos'] > 0), 'invalid verifier cgroup perf time')
        value = row['observation']
        totals(value, index * SECOND)
        accounted = value['completedCalls'] * 3 + value['unassociatedLogs'] + row['startupDiscarded']
        require(perf['events'] >= accounted + value['pendingCalls'], 'verifier samples do not cover calls')
        if index == seconds:
            require(value['pendingCalls'] == 0 and perf['events'] == accounted,
                    'verifier final window has pending or unaccounted records')
        exact(row['probeCounts'], ROLES)
        for role, count in row['probeCounts'].items():
            exact(count, {'hits', 'missed'})
            integer(count['hits'])
            require(type(count['missed']) is int and count['missed'] == 0 and
                    count['hits'] >= value['completedCalls'], 'verifier probe coverage incomplete')
        if index == 0:
            continue
        prior = rows[index - 1]
        require(row['readStartedNanos'] > prior['readEndedNanos'], 'overlapping verifier reads')
        monotonic(prior, row, ('observerCPUUsec', 'observerPeakRSSBytes', 'startupDiscarded'), 'observer')
        monotonic(prior['perf'], perf, PERF, 'perf')
        for role in ROLES:
            monotonic(prior['probeCounts'][role], row['probeCounts'][role], ('hits',), 'probe')
        intervals.append({'index': index, **interval(prior, row), 'pendingAtEnd': value['pendingCalls']})
    first, last = rows[0], rows[-1]
    cpu = last['observerCPUUsec'] - first['observerCPUUsec']
    minimum = last['readStartedNanos'] - first['readEndedNanos']
    maximum = last['readEndedNanos'] - first['readStartedNanos']
    return {'schemaVersion': 1, 'intervals': intervals, 'totals': last['observation'],
            'observer': {'cpuUsecDelta': cpu, 'peakRSSBytes': last['observerPeakRSSBytes'],
                         'meanMillicoresBounds': {'lower': {'numerator': cpu * 1_000_000, 'denominator': maximum},
                                                 'upper': {'numerator': cpu * 1_000_000, 'denominator': minimum}}},
            'scope': 'cgroup-filtered bpf_check elapsed time and finalizer byte counts; includes probe overhead'}
