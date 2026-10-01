"""Replay physical cleanup inventory against a captured attachment witness."""
from activity import snapshot
from samples import exact, integer, require
from trace_observation import empty

OBJECTS = {'map', 'prog', 'link'}


def verify_cleanup(record, witness):
    integer(record['cancelCleanupElapsedNanos'])
    cleanup_witness = record['cleanupWitness']
    exact(cleanup_witness, {'snapshot', 'capturedObjects', 'remaining'})
    snapshot(cleanup_witness['snapshot'])
    require(empty(cleanup_witness['snapshot']), 'nonempty cleanup snapshot')
    exact(cleanup_witness['capturedObjects'], OBJECTS)
    exact(cleanup_witness['remaining'], OBJECTS)
    for key, ids in cleanup_witness['capturedObjects'].items():
        require(type(ids) is list and len(ids) <= 512, 'invalid cleanup object inventory')
        for number in ids:
            integer(number, 1, 2**32 - 1)
        require(ids == sorted(set(ids)), 'unordered cleanup object inventory')
        require(set(witness['objects'][key]) <= set(ids), 'attachment omitted from cleanup census')
        # The native check mode enumerates the captured IDs and returns counts,
        # not a second ID inventory. Zero is required for every object kind.
        remaining = cleanup_witness['remaining'][key]
        integer(remaining, 0, len(ids))
        require(remaining == 0, 'residual captured object remains')
    require(record['zeroOwnedState'] is True, 'owned flood cleanup unconfirmed')
    cleanup = record['cancelCleanupElapsedNanos'] < 2000000000
    require(record['cancelCleanupBudgetPassed'] is cleanup, 'cleanup budget verdict changed')
    return cleanup
