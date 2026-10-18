"""Prove the collector mapped each bound fixture, rather than a total count."""
from local_case import canonical, digest


def mapped_fixtures(document, namespace, bindings, node):
    if document.get('kind') != 'ContainerMemoryList' or document.get('metadata', {}).get('continue'):
        raise ValueError('complete container resource inventory required')
    expected = {key: bound for key, bound in bindings.items() if key.startswith(namespace + '/')}
    actual = {}
    for item in document['items']:
        row = item['snapshot']
        key = row['namespace'] + '/' + row['podName']
        if key not in expected or key in actual:
            raise ValueError('unexpected or duplicate fixture mapping')
        bound = expected[key]
        if (row['containerName'] != 'worker' or row['podUID'] != bound['podUID'] or
                row['containerID'].removeprefix('containerd://') != bound['container'] or
                row['nodeName'] != node or row['freshness'] != 'fresh' or row['completeness'] != 'complete' or
                '/sys/fs/cgroup/' + row['cgroupPath'].lstrip('/') != bound['group']['path']):
            raise ValueError('collector mapping differs from bound fixture')
        actual[key] = bound['identity']
    if actual.keys() != expected.keys():
        raise ValueError('collector has not mapped every fixture')
    return {'mappedContainers': len(actual), 'identitiesSHA256': digest(canonical(actual))}
