"""Distinguish kernel candidate budgeting and ring loss from writer termination."""
from samples import integer, require


def kernel_budget(observation, workload, maximum):
    integer(maximum, 1, 10000)
    require(observation['transportComplete'] is True and workload['mode'] == 'flood',
            'complete transport and an independent burst required')
    counts = observation['engineCounts']
    for key in ('produced', 'sampled', 'lost', 'rejected'):
        integer(counts[key])
    require(counts['produced'] >= counts['sampled'], 'kernel count ordering invalid')
    admitted = counts['produced'] - counts['sampled']
    accounted = observation['events'] + counts['lost'] + counts['rejected']
    result = {'kernelCandidatesWithinBudget': admitted <= maximum,
              'kernelBudgetExhausted': admitted == maximum,
              'ringReservationLossObserved': counts['lost'] > 0,
              'allBurstCallsObserved': counts['produced'] == workload['readCalls'],
              'admittedCandidatesAccounted': accounted == admitted,
              'admittedCandidates': admitted,
              'scope': 'known kernel counters and fixed read workload; no resource or latency qualification'}
    result['kernelBudgetAndLossPassed'] = (observation['termination'] == 'expired'
        and all(result[key] for key in ('kernelCandidatesWithinBudget', 'kernelBudgetExhausted',
                'ringReservationLossObserved', 'allBurstCallsObserved', 'admittedCandidatesAccounted')))
    return result
