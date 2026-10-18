"""Bind each private event receiver to its own admission and pinned workload."""


def target_ordinals(targets):
    if len(targets) != 2:
        raise ValueError('two pinned receiver targets required')
    inodes = [target['group']['inode'] for target in targets]
    if any(type(inode) is not int or inode <= 0 for inode in inodes):
        raise ValueError('invalid target cgroup identity')
    inventories = (inodes, [target['group']['path'] for target in targets],
                   [target['container'] for target in targets], [target['podUID'] for target in targets])
    if any(len(set(values)) != 2 for values in inventories):
        raise ValueError('receiver targets are not distinct workloads')
    # The native watch-targets output uses ascending cgroup inode order. Preserve
    # that explicit mapping even when namespace order differs from inode order.
    ordered = sorted(inodes)
    return [ordered.index(inode) for inode in inodes]


def receiver_configuration(case, admissions, targets, target_index, admission, trace, boot):
    if type(target_index) is not int or not 0 <= target_index < len(targets) or target_index > 1:
        raise ValueError('receiver target outside pinned inventory')
    target, namespace = targets[target_index], case.namespaces[target_index]
    client = admissions.client(target_index)
    name = admission['metadata']['name']
    engine = 'sha256:' + case.runtime.cfg['engineSHA256']
    if (client.namespace != namespace or admissions.pending.get(target_index) != '/' + name
            or admission.get('engineDigest') != engine):
        raise ValueError('receiver admission or namespace differs from owned target')
    return {
        'server': case.runtime.observer_server(),
        'networkScope': case.runtime.observer_network_scope(),
        'token': client.token, 'caPEM': client.ca,
        'workerBootID': boot, 'sessionID': name, 'engineDigest': engine,
        'programmeDigest': 'sha256:' + case.runtime.cfg['programmeIndexSHA256'],
        'target': {'Namespace': namespace, 'PodName': target['podName'], 'PodUID': target['podUID'],
                   'ContainerName': 'worker', 'ContainerID': target['container'],
                   'ContainerStartedAt': target['startedAt'], 'NodeUID': case.runtime.cfg['nodeUID'],
                   'CgroupID': target['group']['inode']},
        **{key: trace[key] for key in ('durationSeconds', 'maxEvents', 'maxOutputBytes', 'maxMapBytes', 'maxPathBytes')},
    }
