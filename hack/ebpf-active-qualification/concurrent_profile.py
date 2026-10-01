"""Frozen maximum-two-target schedule with unchanged conservative budgets."""
import json
from pathlib import Path

from samples import require, strict_json

CASE = 'maximum-two-selected-traces'
PATH = Path(__file__).with_name('concurrent_profile.json')
EXPECTED = {
    'schemaVersion': 1, 'case': CASE, 'pairs': 5,
    'windowSeconds': 1350, 'warmupSeconds': 60, 'workloadContainers': 32,
    'minimumActiveSeconds': 900, 'minimumActiveOperations': 900,
    'minimumScans': 240, 'minimumActiveScans': 120,
    'selectedTargets': 2,
    'workload': {'mode': 'cached', 'count': 1320, 'periodMilliseconds': 1000, 'fileBytes': 8388608},
    'trace': {'count': 36, 'firstOffsetSeconds': 30, 'periodSeconds': 35,
              'durationSeconds': 30, 'maxEvents': 10000, 'maxOutputBytes': 8388608,
              'maxMapBytes': 8388608, 'maxPathBytes': 64, 'kind': 'files', 'rawPaths': True,
              'objects': {'map': 14, 'prog': 10, 'link': 10}},
}


def load_concurrent_profile(path=PATH):
    value = strict_json(Path(path).read_text())
    require(value == EXPECTED and json.dumps(value, sort_keys=True) == json.dumps(EXPECTED, sort_keys=True),
            'concurrent measurement schedule or bounds changed')
    return value


def maximum_node_arguments(args):
    result, found, index = [], False, 0
    while index < len(args):
        argument = args[index]
        if argument == '--max-node-traces' or argument.startswith('--max-node-traces='):
            require(not found, 'duplicate Node concurrency argument')
            found = True
            if argument == '--max-node-traces':
                index += 1
                require(index < len(args), 'missing Node concurrency value')
                value = args[index]
            else:
                value = argument.split('=', 1)[1]
            require(value == '1', 'concurrent campaign must start from default Node limit')
            result.append('--max-node-traces=2')
        else:
            result.append(argument)
        index += 1
    if not found:
        result.append('--max-node-traces=2')
    return result
