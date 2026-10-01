"""Cancel known admissions and physically enumerate captured kernel objects."""
import time

from samples import require
from trace_observation import empty


def clear_owned_session(window, result):
    cancel_started = time.monotonic()
    window.admissions.cancel_all()
    deadline = time.monotonic() + 5
    while True:
        current = window.snapshot()
        left = window.case.remaining({key: sorted(ids) for key, ids in window.owned.items()})
        if empty(current) and not any(left['remaining'].values()):
            result['cleanupWitness'] = {'snapshot': current,
                'capturedObjects': {key: sorted(ids) for key, ids in window.owned.items()},
                'remaining': left['remaining']}
            result['zeroOwnedState'] = True
            result['cancelCleanupElapsedNanos'] = int((time.monotonic() - cancel_started) * 1000000000)
            result['cancelCleanupBudgetPassed'] = result['cancelCleanupElapsedNanos'] < 2000000000
            if result.get('completed'):
                result['sessionChecksPassed'] = result['sessionChecksPassed'] and result['cancelCleanupBudgetPassed']
            break
        require(time.monotonic() < deadline, 'owned flood state did not clear')
        time.sleep(.1)
