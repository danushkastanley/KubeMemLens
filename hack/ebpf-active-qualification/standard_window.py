"""Validate every poll and retain one observation per completed operation."""
from activity import clock
from samples import exact, integer, require
from standard_metrics import AGENT_FIELDS, RESULTS

AGENT_KEYS = set(AGENT_FIELDS.values())
AGENT_COUNTERS = {'scanSuccess', 'scanFailure', 'postSuccess', 'postFailure'}
SCAN_FIELDS = AGENT_KEYS - {'postSuccess', 'postFailure'}
ROW_KEYS = {'schemaVersion', 'index', 'elapsedNanos', 'readNanos', 'clock', 'agent',
            'collector', 'observerCPUUsec', 'observerPeakRSSBytes'}
SECOND = 1000000000


def bounds(row):
    c = row['clock']
    return c['wallNanos'] - row['readNanos'] - c['uncertaintyNanos'], c['wallNanos'] + c['uncertaintyNanos']


def standard_window(rows, seconds):
    integer(seconds, 1, 1800)
    require(type(rows) is list and len(rows) == seconds + 1, 'incomplete standard metrics window')
    scans, ingestions = [], []
    for index, row in enumerate(rows):
        exact(row, ROW_KEYS)
        require(type(row['schemaVersion']) is int and row['schemaVersion'] == 1 and
                type(row['index']) is int and row['index'] == index, 'standard metrics ordering/version changed')
        integer(row['elapsedNanos'])
        integer(row['readNanos'], 1, 100000000)
        integer(row['observerCPUUsec'])
        integer(row['observerPeakRSSBytes'], 1)
        clock(row['clock'])
        exact(row['agent'], AGENT_KEYS)
        exact(row['collector'], {'durationNanos', 'results'})
        exact(row['collector']['results'], RESULTS)
        for name, value in row['agent'].items():
            integer(value, 0, 1800 * SECOND if name == 'scanDurationNanos' else 2**53 - 1)
        integer(row['agent']['scanDurationNanos'], 1)
        integer(row['agent']['scanCompletedUnixSeconds'], 1)
        integer(row['collector']['durationNanos'], 1, 1800 * SECOND)
        for value in row['collector']['results'].values():
            integer(value, 0, 2**53 - 1)
        require(0 < sum(row['collector']['results'].values()) <= 2**53 - 1, 'invalid ingestion total')
        begin, end = bounds(row)
        require(abs(row['elapsedNanos'] - index * SECOND) <= 100000000, 'standard metrics sampling drift')
        origin = bounds(rows[0])[0]
        require(abs(begin - origin - row['elapsedNanos']) <= 105000000, 'standard metrics wall clock changed')
        mono_origin = rows[0]['clock']['monotonicNanos'] - rows[0]['readNanos']
        require(abs(row['clock']['monotonicNanos'] - row['readNanos'] - mono_origin - row['elapsedNanos']) <= 105000000,
                'standard metrics monotonic clock changed')
        require(row['agent']['scanCompletedUnixSeconds'] * SECOND <= end, 'scan completed in the future')
        if index == 0:
            require(row['elapsedNanos'] == 0, 'missing initial standard metrics observation')
            continue
        prior = rows[index - 1]
        require(900000000 <= row['elapsedNanos'] - prior['elapsedNanos'] <= 1100000000,
                'standard metrics cadence gap')
        require(begin > bounds(prior)[1] and row['observerCPUUsec'] >= prior['observerCPUUsec'],
                'overlapping reads or observer reset')
        a, b = prior['agent'], row['agent']
        require(all(b[k] >= a[k] for k in AGENT_COUNTERS), 'agent counter reset')
        require(b['scanFailure'] == a['scanFailure'] and b['postFailure'] == a['postFailure'],
                'agent failed during normal observation')
        count = b['scanSuccess'] - a['scanSuccess']
        require(count <= 1, 'scan durations missed between polls')
        if count == 0:
            require(all(b[k] == a[k] for k in SCAN_FIELDS), 'scan gauge changed without a completed attempt')
        else:
            lower = b['scanCompletedUnixSeconds'] * SECOND
            upper = lower + SECOND  # Production completion timestamp has whole-second precision.
            require(upper > bounds(prior)[0], 'new scan has an old completion timestamp')
            require(lower >= b['scanDurationNanos'], 'invalid scan duration/completion')
            scans.append({'pollIndex': index, 'durationNanos': b['scanDurationNanos'],
                          'earliestStartWallNanos': lower - b['scanDurationNanos'],
                          'latestEndWallNanos': upper})
        old, new = prior['collector'], row['collector']
        require(all(new['results'][k] >= old['results'][k] for k in RESULTS), 'collector counter reset')
        require(all(new['results'][k] == old['results'][k] for k in RESULTS - {'accepted', 'duplicate'}),
                'collector rejected ingestion during normal observation')
        count = sum(new['results'].values()) - sum(old['results'].values())
        if count == 0:
            require(new['durationNanos'] == old['durationNanos'], 'ingestion gauge changed without a request')
        else:
            ingestions.append({'pollIndex': index, 'newRequests': count,
                               'knownLatestDurationNanos': new['durationNanos'],
                               'missingDurations': count - 1})
    require(rows[-1]['elapsedNanos'] >= seconds * SECOND, 'short standard metrics window')
    return {'scans': scans, 'ingestions': ingestions,
            'collectorMissingDurations': sum(r['missingDurations'] for r in ingestions),
            'scope': 'validated polling observations; latest collector durations are not a complete distribution'}
