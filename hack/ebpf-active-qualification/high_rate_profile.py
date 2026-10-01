"""Frozen mixed-I/O schedule; independent from the historical normal profile."""
import json
from pathlib import Path

from samples import require, strict_json

PATH = Path(__file__).with_name('high_rate_profile.json')
EXPECTED = {
    'schemaVersion': 1, 'case': 'high-rate-mixed-files', 'pairs': 5,
    'windowSeconds': 1350, 'warmupSeconds': 60, 'workloadContainers': 32,
    'minimumActiveSeconds': 900, 'minimumActiveOperationsPerMode': 3000,
    'minimumScans': 240, 'minimumActiveScans': 120,
    'workload': {'mode': 'mixed', 'count': 13200, 'periodMilliseconds': 100, 'fileBytes': 8388608},
    'trace': {'count': 36, 'firstOffsetSeconds': 30, 'periodSeconds': 35,
              'durationSeconds': 30, 'maxEvents': 10000, 'maxOutputBytes': 8388608,
              'maxMapBytes': 8388608, 'maxPathBytes': 64, 'kind': 'files', 'rawPaths': True,
              'objects': {'map': 7, 'prog': 5, 'link': 5}},
}


def load_high_rate_profile(path=PATH):
    value = strict_json(Path(path).read_text())
    require(value == EXPECTED and json.dumps(value, sort_keys=True) == json.dumps(EXPECTED, sort_keys=True),
            'high-rate measurement schedule or bounds changed')
    return value
