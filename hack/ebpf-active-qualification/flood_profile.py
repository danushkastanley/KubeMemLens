"""Frozen paired flood schedule; early writer termination is expected evidence."""
import json
from pathlib import Path

from samples import require, strict_json

CASE = 'bounded-file-flood'
KINDS = ('event-limit', 'output-limit', 'ring-flood', 'paused-reader')
PATH = Path(__file__).with_name('flood_profile.json')
EXPECTED = {
    'schemaVersion': 1, 'case': CASE, 'pairs': 5, 'windowSeconds': 900,
    'warmupSeconds': 60, 'workloadContainers': 32,
    'workload': {'fileBytes': 8388608},
    'trace': {'count': 20, 'firstOffsetSeconds': 30, 'periodSeconds': 40,
              'durationSeconds': 30, 'maxEvents': 10000, 'maxOutputBytes': 8388608,
              'maxMapBytes': 8388608, 'maxPathBytes': 64, 'kind': 'files', 'rawPaths': True,
              'objects': {'map': 7, 'prog': 5, 'link': 5}},
    'caseOrder': list(KINDS), 'maximumSlotLatenessNanos': 250000000,
    'minimumScans': 160, 'selectedMetric': 'application-read-throughput',
    'resourceCaps': {role: {'cpuQuotaUsec': 200000, 'cpuPeriodUsec': 100000,
                           'cpuBurstUsec': 0, 'memoryMaxBytes': 536870912}
                     for role in ('node', 'api')},
}


def load_flood_profile(path=PATH):
    value = strict_json(Path(path).read_text())
    require(value == EXPECTED and json.dumps(value, sort_keys=True) == json.dumps(EXPECTED, sort_keys=True),
            'flood schedule or bounds changed')
    return value


def slots(profile):
    require(profile['case'] == CASE, 'wrong flood profile')
    trace = profile['trace']
    return [(index, KINDS[index % len(KINDS)], trace['firstOffsetSeconds'] + index * trace['periodSeconds'])
            for index in range(trace['count'])]


def stem(index, kind):
    require(type(index) is int and 0 <= index < EXPECTED['trace']['count'] and kind == KINDS[index % 4],
            'unexpected flood slot')
    return f'delivery-flood-{index:03}-{kind}'
