"""Replay schema-2 scheduler histograms without inventing exact percentiles."""
from pathlib import Path

from samples import exact, integer, require, strict_json

SECOND = 1_000_000_000
LIMIT = 100_000_000
ROW = {'schemaVersion', 'index', 'startedNanos', 'cutoffNanos', 'readStartedNanos',
       'readEndedNanos', 'observerCPUUsec', 'observerPeakRSSBytes', 'startupDiscarded',
       'perf', 'observation'}
OBSERVATION = {'completedWaits', 'events', 'unmatchedSwitchIns', 'replacedEnqueues',
               'exitedPending', 'pending'}
COUNTERS = OBSERVATION - {'completedWaits', 'pending'}


def read_scheduler_samples(path):
    path = Path(path)
    require(path.stat().st_size <= 16 << 20, 'scheduler stream exceeds native byte bound')
    rows = []
    with path.open('rb') as stream:
        while line := stream.readline(8193):
            require(len(line) <= 8192 and line.endswith(b'\n') and len(rows) < 1801,
                    'scheduler record exceeds bounds or is incomplete')
            rows.append(strict_json(line))
    return rows


def bucket_bounds(index):
    integer(index, 0, 64)
    return (0, 0) if index == 0 else (1 << (index - 1), (1 << index) - 1)


def percentile_bounds(buckets, percent):
    integer(percent, 1, 100)
    count = sum(buckets)
    if count == 0:
        return None
    rank, seen = (count * percent + 99) // 100, 0
    for index, value in enumerate(buckets):
        seen += value
        if seen >= rank:
            low, high = bucket_bounds(index)
            return {'lowerNanos': low, 'upperNanos': high}
    raise ValueError('scheduler percentile rank missing')


def sum_bounds(buckets, maximum, observed_maximum):
    low, high = 0, 0
    for index, count in enumerate(buckets):
        lower, upper = bucket_bounds(index)
        low += count * lower
        high += count * min(upper, maximum)
    if observed_maximum:
        low += maximum - bucket_bounds(maximum.bit_length())[0]
    return low, high


def histogram(value, elapsed):
    exact(value, {'buckets', 'count', 'sumNanos', 'maximumNanos'})
    buckets = value['buckets']
    require(type(buckets) is list and len(buckets) == 65, 'scheduler histogram shape changed')
    for count in buckets:
        integer(count, 0, LIMIT)
    count = integer(value['count'], 0, LIMIT)
    require(sum(buckets) == count, 'scheduler histogram count differs')
    total = integer(value['sumNanos'])
    maximum = integer(value['maximumNanos'], 0, elapsed)
    if count == 0:
        require(total == maximum == 0, 'empty scheduler histogram contains waits')
        return
    highest = max(index for index, count in enumerate(buckets) if count)
    require(maximum.bit_length() == highest, 'scheduler maximum differs from occupied buckets')
    low, high = sum_bounds(buckets, maximum, True)
    require(low <= total <= high, 'scheduler sum is impossible for histogram and maximum')


def distribution(buckets, total):
    count = sum(buckets)
    return {'state': 'observed' if count else 'empty', 'count': count, 'sumNanos': total,
            'meanNanos': {'numerator': total, 'denominator': count} if count else None,
            'percentiles': {f'p{p}': percentile_bounds(buckets, p) for p in (50, 95, 99)}}


def scheduler_window(rows, seconds, cpu_count):
    """Validate numeric observations; enclosing receipt must prove source/exit/cleanup."""
    integer(seconds, 1, 1800)
    integer(cpu_count, 1, 64)
    require(type(rows) is list and len(rows) == seconds + 1, 'incomplete scheduler window')
    intervals = []
    for index, row in enumerate(rows):
        exact(row, ROW)
        require(type(row['schemaVersion']) is int and row['schemaVersion'] == 2,
                'scheduler coverage counters require schema 2')
        require(type(row['index']) is int and row['index'] == index, 'scheduler sample ordering changed')
        for key in ROW - {'schemaVersion', 'index', 'perf', 'observation'}:
            integer(row[key], 0 if key in {'observerCPUUsec', 'startupDiscarded'} else 1)
        origin = rows[0]['startedNanos']
        require(row['startedNanos'] == origin and row['cutoffNanos'] == origin + index * SECOND,
                'scheduler origin or cutoff changed')
        delay = row['readStartedNanos'] - row['cutoffNanos']
        require(100_000_000 <= delay <= 350_000_000, 'scheduler observation outside fixed reorder/deadline bounds')
        require(0 <= row['readEndedNanos'] - row['readStartedNanos'] <= 50_000_000,
                'scheduler read exceeded bound')
        perf = row['perf']
        exact(perf, {'enabledNanos', 'runningNanos', 'lostSamples', 'minimumEnabledNanos'})
        for key, value in perf.items():
            integer(value, 0 if key == 'lostSamples' else 1)
        require(perf['lostSamples'] == 0 and perf['enabledNanos'] == perf['runningNanos'],
                'scheduler perf loss or inactive capture')
        require(perf['enabledNanos'] >= cpu_count * 4 * perf['minimumEnabledNanos'],
                'scheduler descriptor coverage cannot match CPU roster')
        observation = row['observation']
        exact(observation, OBSERVATION)
        for key in COUNTERS:
            integer(observation[key], 0, LIMIT)
        integer(observation['pending'], 0, 32768)
        histogram(observation['completedWaits'], index * SECOND)
        require(all(observation[key] <= observation['events'] for key in COUNTERS | {'pending'})
                and observation['completedWaits']['count'] + observation['unmatchedSwitchIns'] <= observation['events'],
                'scheduler coverage counters exceed observed events')
        if index == 0:
            continue
        prior = rows[index - 1]
        require(row['readStartedNanos'] > prior['readEndedNanos'], 'overlapping scheduler observations')
        require(all(row[key] >= prior[key] for key in ('observerCPUUsec', 'observerPeakRSSBytes', 'startupDiscarded')),
                'scheduler observer counters reset')
        require(all(perf[key] >= prior['perf'][key] for key in perf), 'scheduler perf counters reset')
        old, new = prior['observation'], observation
        delta = {key: new[key] - old[key] for key in COUNTERS}
        require(all(0 <= value <= delta['events'] for value in delta.values()), 'scheduler coverage counters reset')
        a, b = old['completedWaits'], new['completedWaits']
        buckets = [right - left for left, right in zip(a['buckets'], b['buckets'])]
        count, total = b['count'] - a['count'], b['sumNanos'] - a['sumNanos']
        require(all(value >= 0 for value in buckets) and 0 <= count <= delta['events']
                and count + delta['unmatchedSwitchIns'] <= delta['events']
                and sum(buckets) == count and total >= 0 and b['maximumNanos'] >= a['maximumNanos'],
                'scheduler histogram counters reset')
        maximum_changed = b['maximumNanos'] > a['maximumNanos']
        if maximum_changed:
            require(buckets[b['maximumNanos'].bit_length()] > 0, 'scheduler maximum changed without a matching wait')
        low, high = sum_bounds(buckets, b['maximumNanos'], maximum_changed)
        require(low <= total <= high, 'scheduler interval sum differs from its buckets')
        require(old['pending'] - new['pending'] <= count + delta['exitedPending'],
                'scheduler pending waits disappeared without completion or exit')
        intervals.append({'index': index, **distribution(buckets, total), 'coverage': delta,
                          'pendingAtEnd': new['pending']})
    first, last = rows[0], rows[-1]
    a, b = first['observation']['completedWaits'], last['observation']['completedWaits']
    cpu = last['observerCPUUsec'] - first['observerCPUUsec']
    minimum = last['readStartedNanos'] - first['readEndedNanos']
    maximum = last['readEndedNanos'] - first['readStartedNanos']
    return {'schemaVersion': 1, 'intervals': intervals,
            'completedWaits': distribution([y - x for x, y in zip(a['buckets'], b['buckets'])], b['sumNanos'] - a['sumNanos']),
            'coverage': {key: last['observation'][key] - first['observation'][key] for key in COUNTERS},
            'pendingAtStart': first['observation']['pending'], 'pendingAtEnd': last['observation']['pending'],
            'observer': {'cpuUsecDelta': cpu, 'peakRSSBytes': last['observerPeakRSSBytes'],
                         'meanMillicoresBounds': {'lower': {'numerator': cpu * 1_000_000, 'denominator': maximum},
                                                 'upper': {'numerator': cpu * 1_000_000, 'denominator': minimum}}},
            'scope': 'completed waits from latest observed enqueue; unmatched, replaced and pending waits remain explicit; percentile ranges only'}
