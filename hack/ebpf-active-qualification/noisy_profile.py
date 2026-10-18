"""Frozen selected trace plus ten non-selected read/write neighbours."""
import json
from pathlib import Path

from samples import require, strict_json

PATH = Path(__file__).with_name('noisy_profile.json')
CASE = 'normal-with-ten-noisy-neighbours'
EXPECTED = {
    'schemaVersion': 1, 'case': CASE, 'pairs': 5,
    'windowSeconds': 1350, 'warmupSeconds': 60, 'workloadContainers': 32,
    'minimumActiveSeconds': 900, 'minimumActiveOperations': 900,
    'minimumScans': 240, 'minimumActiveScans': 120,
    'workload': {'mode': 'cached', 'count': 1320, 'periodMilliseconds': 1000, 'fileBytes': 8388608},
    'noise': {'count': 10, 'sameNamespace': 5, 'otherNamespace': 5, 'minimumActiveOperations': 900,
              'workload': {'mode': 'noise', 'count': 1320, 'periodMilliseconds': 1000, 'fileBytes': 8388608}},
    'trace': {'count': 36, 'firstOffsetSeconds': 30, 'periodSeconds': 35,
              'durationSeconds': 30, 'maxEvents': 10000, 'maxOutputBytes': 8388608,
              'maxMapBytes': 8388608, 'maxPathBytes': 64, 'kind': 'files', 'rawPaths': True,
              'objects': {'map': 7, 'prog': 5, 'link': 5}},
}


def load_noisy_profile(path=PATH):
    value = strict_json(Path(path).read_text())
    require(value == EXPECTED and json.dumps(value, sort_keys=True) == json.dumps(EXPECTED, sort_keys=True),
            'noisy measurement schedule or bounds changed')
    return value
