"""Select only the two frozen local workload contracts and their paired replay."""
from mixed_workload import compare_mixed_workloads
from workload import compare_workloads


def workload_arguments(profile):
    work = profile['workload']
    schedules = {'normal-confirmed-files': ['series', 'cached'],
                 'high-rate-mixed-files': ['mixed-series']}
    return schedules[profile['case']] + [str(work['count']), str(work['periodMilliseconds'])]


def compare_case(control_raw, enabled_raw, profile, witness):
    work = profile['workload']
    kwargs = {'count': work['count'], 'period_ms': work['periodMilliseconds'], 'witness': witness,
              'seconds': profile['windowSeconds'], 'expected_objects': profile['trace']['objects']}
    if profile['case'] == 'high-rate-mixed-files':
        result = compare_mixed_workloads(control_raw, enabled_raw, **kwargs,
                                        minimum_active_operations_per_mode=profile['minimumActiveOperationsPerMode'])
        return result, result['mixedSelectedLatencyBudgetPassed']
    if profile['case'] != 'normal-confirmed-files':
        raise ValueError('unknown local workload case')
    result = compare_workloads(control_raw, enabled_raw, mode=work['mode'], **kwargs,
                               minimum_active_operations=profile['minimumActiveOperations'])
    return result, result['normalSelectedLatencyBudgetPassed']
