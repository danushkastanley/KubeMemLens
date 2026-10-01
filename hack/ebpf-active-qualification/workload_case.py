"""Select only the two frozen local workload contracts and their paired replay."""
from mixed_workload import compare_mixed_workloads
from workload import compare_workloads
from noisy_profile import CASE as NOISY_CASE
from concurrent_profile import CASE as CONCURRENT_CASE
from concurrent_workload import compare_concurrent_workloads, TARGETS


def workload_arguments(profile):
    work = profile['workload']
    schedules = {'normal-confirmed-files': ['series', 'cached'],
                 NOISY_CASE: ['series', 'cached'],
                 CONCURRENT_CASE: ['series', 'cached'],
                 'high-rate-mixed-files': ['mixed-series']}
    return schedules[profile['case']] + [str(work['count']), str(work['periodMilliseconds'])]


def compare_case(control_raw, enabled_raw, profile, witness):
    work = profile['workload']
    kwargs = {'count': work['count'], 'period_ms': work['periodMilliseconds'], 'witness': witness,
              'seconds': profile['windowSeconds'], 'expected_objects': profile['trace']['objects']}
    if profile['case'] == CONCURRENT_CASE:
        result = compare_concurrent_workloads(control_raw, enabled_raw, mode=work['mode'], **kwargs,
                                             minimum_active_operations=profile['minimumActiveOperations'])
        return result, result['concurrentSelectedLatencyBudgetPassed']
    if profile['case'] == 'high-rate-mixed-files':
        result = compare_mixed_workloads(control_raw, enabled_raw, **kwargs,
                                        minimum_active_operations_per_mode=profile['minimumActiveOperationsPerMode'])
        return result, result['mixedSelectedLatencyBudgetPassed']
    if profile['case'] not in ('normal-confirmed-files', NOISY_CASE):
        raise ValueError('unknown local workload case')
    result = compare_workloads(control_raw, enabled_raw, mode=work['mode'], **kwargs,
                               minimum_active_operations=profile['minimumActiveOperations'])
    return result, result['normalSelectedLatencyBudgetPassed']


def read_workloads(directory, profile):
    if profile['case'] == CONCURRENT_CASE:
        return {target: (directory / ('workload-' + target + '.jsonl')).read_text() for target in TARGETS}
    return (directory / 'workload.jsonl').read_text()
