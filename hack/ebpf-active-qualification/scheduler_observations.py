"""Summarise already-validated scheduler counters without inventing quantiles."""
from collections import Counter

from samples import scheduling_deltas


def scheduler_observations(rows, role=None):
    """Call after the full resource-window validator; preserve every gap category."""
    changes = scheduling_deltas(rows, role)
    states = Counter(row['state'] for row in changes)
    source_states = Counter()
    for row in rows:
        value = (row['node']['scheduling'] if role is None else
                 row['groups'][role]['process'].get('scheduling', {'state': 'unavailable'}))
        source_states[value['state']] += 1
    observed = [row for row in changes if row['state'] == 'observed']
    covered = sum(rows[row['interval'] + 1]['elapsedNanos'] - rows[row['interval']]['elapsedNanos']
                  for row in observed)
    totals = None
    mean = None
    if observed:
        totals = {key: sum(row[key] for row in observed)
                  for key in ('runtimeNanos', 'runqueueWaitNanos', 'timeslices')}
        if totals['timeslices']:
            # This ratio is a counter-derived mean over observed intervals only.
            # It contains no information about the individual wait distribution.
            mean = {'numeratorNanos': totals['runqueueWaitNanos'], 'denominatorTimeslices': totals['timeslices']}
    return {'scope': 'node-cpu-scheduler-counters' if role is None else 'stable-observed-task-cohorts',
            'sampleStates': dict(sorted(source_states.items())), 'intervalStates': dict(sorted(states.items())),
            'intervals': len(changes), 'observedNanos': covered,
            'completeCounterCoverage': bool(changes) and len(observed) == len(changes),
            'observedTotals': totals, 'meanRunqueueWaitPerTimeslice': mean,
            'latencyPercentiles': {'state': 'unavailable', 'reason': 'cumulative counters contain no latency distribution'},
            'qualification': 'no scheduler latency percentile or regression verdict'}
