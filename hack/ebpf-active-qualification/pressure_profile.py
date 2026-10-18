"""Frozen continuous-producer schedule, separate from finite writer checks."""
import json
from pathlib import Path

from samples import require, strict_json

CASE = 'sustained-file-pressure'
KINDS = ('ring-pressure', 'paused-pressure')
PATH = Path(__file__).with_name('pressure_profile.json')
EXPECTED = {
    'schemaVersion': 1, 'case': CASE, 'pairs': 5, 'windowSeconds': 900,
    'warmupSeconds': 60, 'workloadContainers': 32,
    'workload': {'fileBytes': 8388608},
    'producerSeconds': 910, 'producerLeadSeconds': 1, 'minimumCommonIntervals': 890,
    'minimumObservedSessionIntervals': 20,
    'trace': {'count': 20, 'firstOffsetSeconds': 30, 'periodSeconds': 40,
              'durationSeconds': 30, 'maxEvents': 10000, 'maxOutputBytes': 8388608,
              'maxMapBytes': 8388608, 'maxPathBytes': 64, 'kind': 'files', 'rawPaths': True,
              'objects': {'map': 7, 'prog': 5, 'link': 5}},
    'caseOrder': list(KINDS), 'maximumSlotLatenessNanos': 250000000,
    'minimumScans': 160,
    'resourceCaps': {role: {'cpuQuotaUsec': 200000, 'cpuPeriodUsec': 100000,
                           'cpuBurstUsec': 0, 'memoryMaxBytes': 536870912}
                     for role in ('node', 'api')},
}


def load_pressure_profile(path=PATH):
    value = strict_json(Path(path).read_text())
    require(value == EXPECTED and json.dumps(value, sort_keys=True) == json.dumps(EXPECTED, sort_keys=True),
            'sustained pressure schedule or caps changed')
    return value


def pressure_slots(profile):
    require(profile['case'] == CASE, 'wrong sustained pressure profile')
    trace = profile['trace']
    return [(i, KINDS[i % len(KINDS)], trace['firstOffsetSeconds'] + i * trace['periodSeconds'])
            for i in range(trace['count'])]
