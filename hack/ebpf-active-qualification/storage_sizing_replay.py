"""Require a complete untraced noisy window before reporting sizing observations."""
from nonselected_workload import ROLES
from processes import read_document
from provenance import file_digest, verify_envelope
from samples import read_samples, require, validate_window
from scheduler_observations import scheduler_observations
from scheduler_capture import replay_scheduler
from standard_window import standard_window
from storage_pair import compare_storage
from workload import latency_summary
from verify_series import validate_series


def application_observations(directory, profile):
    streams = {'selected': ('workload', profile['workload'])}
    streams.update({role: (role, profile['noise']['workload']) for role in ROLES})
    result = {}
    for role, (name, work) in streams.items():
        path = directory / (name + '.jsonl')
        require(path.stat().st_size <= 2 << 20, 'sizing workload stream exceeds bound')
        rows = validate_series(path.read_text(), work['mode'], work['count'], work['periodMilliseconds'])[1:]
        result[role] = {'latency': latency_summary(rows),
                        'readBytes': sum(row['observation']['readBytes'] for row in rows),
                        'writeBytes': sum(row['observation']['writeBytes'] for row in rows),
                        'sourceSHA256': file_digest(path)}
    return result


def replay_sizing(directory, profile, before, after, expected_scope, bindings):
    require(verify_envelope(directory, 'control', profile) == expected_scope,
            'sizing window source/configuration/boot changed')
    receipt = read_document(directory / 'window.private.json')
    require(receipt['completed'] is True and not receipt['cleanupFailures']
            and receipt['phase'] == 'control' and receipt['pair'] == 1 and receipt['sessions'] == [],
            'sizing window incomplete or traced')
    require(sum(row['mappedContainers'] for row in receipt['fixtureMapping']) == profile['workloadContainers'],
            'sizing fixture density changed')
    require(len(bindings) == 11 and {row['group']['role'] for row in bindings.values()} == {'selected', *ROLES},
            'selected plus ten noisy storage bindings required')
    for snapshot in (before, after):
        require(snapshot['bootID'] == expected_scope['bootID'], 'storage and window boot identities differ')
        require(snapshot['groups'].keys() == bindings.keys(), 'storage fixture inventory differs from frozen bindings')
        for key, value in snapshot['groups'].items():
            bound = bindings[key]
            require(value['identity'] == bound['identity'] and value['cgroupInode'] == bound['group']['inode']
                    and value['role'] == bound['group']['role'], 'storage role or cgroup binding changed')
        require(all(receipt['fixtureIdentities'].get(key) == value['identity']
                    for key, value in snapshot['groups'].items()), 'storage and workload fixture identities differ')
    resources = read_samples(directory / 'resources.jsonl')
    roles = {'agent', 'collector', 'selected', *ROLES}
    validate_window(resources, profile['windowSeconds'], roles)
    standard_window(read_samples(directory / 'standard.jsonl'), profile['windowSeconds'])
    cpu = {role: {key: resources[-1]['groups'][role]['cpu'][key] - resources[0]['groups'][role]['cpu'][key]
                  for key in ('usage_usec', 'nr_periods', 'nr_throttled', 'throttled_usec')} for role in sorted(roles)}
    return {'schemaVersion': 1, 'private': True, 'storage': compare_storage(before, after),
            'applications': application_observations(directory, profile), 'cpuAccountingDeltas': cpu,
            'nodeSchedulerCounters': scheduler_observations(resources),
            'nodeSchedulerLatency': replay_scheduler(directory, profile['windowSeconds'], expected_scope),
            'resourceSourceSHA256': file_digest(directory / 'resources.jsonl'),
            'standardSourceSHA256': file_digest(directory / 'standard.jsonl'),
            'qualification': 'none; untraced sizing observations only; full paired cases remain required'}
