"""Recover every new scan from bounded atomic history, or reject the window."""
from samples import exact, integer, require

SECOND = 1000000000
LIMIT = 32


def validate_history(row, read_end):
    agent, history = row['agent'], row['agentScans']
    total = agent['scanSuccess'] + agent['scanFailure']
    integer(total, 1, 2**53 - 1)
    require(type(history) is list and len(history) == min(total, LIMIT), 'incomplete bounded scan history')
    completed = 0
    outcomes = {'success': 0, 'failure': 0}
    for index, entry in enumerate(history):
        exact(entry, {'sequence', 'completedUnixNanos', 'durationNanos', 'result'})
        integer(entry['sequence'], 1, 2**53 - 1)
        integer(entry['completedUnixNanos'], 1, 2**63 - 1)
        integer(entry['durationNanos'], 1, 1800 * SECOND)
        require(type(entry['result']) is str and entry['result'] in outcomes, 'invalid retained scan outcome')
        require(entry['sequence'] == total - len(history) + index + 1, 'scan history sequence gap')
        require(completed <= entry['completedUnixNanos'] <= read_end
                and entry['completedUnixNanos'] >= entry['durationNanos'], 'scan history clock differs')
        completed = entry['completedUnixNanos']
        outcomes[entry['result']] += 1
    require(outcomes['success'] <= agent['scanSuccess'] and outcomes['failure'] <= agent['scanFailure'],
            'scan history outcomes differ from counters')
    require(history[-1]['durationNanos'] == agent['scanDurationNanos']
            and history[-1]['completedUnixNanos'] // SECOND == agent['scanCompletedUnixSeconds'],
            'scan history and latest gauges differ')


def history_scans(prior, row, prior_read_begin):
    previous = {entry['sequence']: entry for entry in prior['agentScans']}
    current = {entry['sequence']: entry for entry in row['agentScans']}
    require(all(current[sequence] == previous[sequence] for sequence in current.keys() & previous.keys()),
            'previously observed scan timing changed')
    first = prior['agent']['scanSuccess'] + prior['agent']['scanFailure'] + 1
    last = row['agent']['scanSuccess'] + row['agent']['scanFailure']
    require(last - first + 1 <= LIMIT and all(sequence in current for sequence in range(first, last + 1)),
            'scan history overwritten between polls')
    result = []
    for sequence in range(first, last + 1):
        entry = current[sequence]
        require(entry['result'] == 'success', 'failed scan in measured history')
        require(entry['completedUnixNanos'] > prior_read_begin, 'new scan has an old completion timestamp')
        result.append({'pollIndex': row['index'], 'durationNanos': entry['durationNanos'],
                       'earliestStartWallNanos': entry['completedUnixNanos'] - entry['durationNanos'],
                       'latestEndWallNanos': entry['completedUnixNanos']})
    return result
