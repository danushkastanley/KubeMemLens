"""Synthetic complete paired streams for replay tests; never runtime evidence."""
from copy import deepcopy
import json

from ceiling_result import evaluate_ceiling_session
from local_case import digest
from paused_reader_result import evaluate_paused_reader_session
from pressure_profile import load_pressure_profile, pressure_slots
from pressure_session import saturation
from provenance import write_envelope
from test_activity import witness
from test_flood_resources import contained
from test_flood_session import Fixture
from test_pressure_observation import records, stream
from test_samples import fixture
from test_standard_window import rows as standard_rows
from scheduler_test_fixture import BOOT, write_scheduler
from verifier_test_fixture import write_verifier

BASE = 1700000000000000000
SECOND = 1000000000


def clock_at(seconds):
    return {'wallNanos': BASE + int(seconds * SECOND),
            'monotonicNanos': 11 * SECOND + int(seconds * SECOND), 'uncertaintyNanos': 1000}


def empty_snapshot(seconds):
    return {'workers': 0, 'activeControls': 0, 'excludedWorkers': 0, 'objects': {'map': [], 'prog': [], 'link': []},
            'kernelMapBytes': 0, 'userMapBytes': 0, 'clock': clock_at(seconds)}


def attached(index, seconds):
    value = empty_snapshot(seconds)
    value.update(workers=1, activeControls=1, kernelMapBytes=1000000, userMapBytes=540672,
        objects={key: list(range(index * 100 + 1, index * 100 + count + 1))
                 for key, count in {'map': 7, 'prog': 5, 'link': 5}.items()})
    return value


def resources(enabled):
    rows = fixture(900)
    limits = contained()[0]['groups']['node']['containment']
    for row in rows:
        row['schemaVersion'] = 3
        for group in row['groups'].values():
            group['containment'] = deepcopy(limits)
        base = deepcopy(row['groups']['node'])
        if not enabled:
            row['groups'] = {}
        row['groups'].update({role: deepcopy(base) for role in ('agent', 'collector', 'selected')})
    return rows


def producer():
    rows = records(910)
    for row in rows:
        row['wallNanos'] -= SECOND
        for key in list(row):
            if key != 'wallNanos' and ('Monotonic' in key or key.startswith('monotonic')):
                row[key] += 9 * SECOND
        if 'readCalls' in row:
            row.update(readCalls=4096, readBytes=4096 * 64)
    return stream(rows)


def make_pair(root):
    profile = load_pressure_profile()
    cfg = {'sourceSHA256': 'a' * 64}
    for phase in ('control', 'enabled'):
        directory = root / phase
        directory.mkdir()
        (directory / 'resources.jsonl').write_text(stream(resources(phase == 'enabled')))
        (directory / 'standard.jsonl').write_text(stream(standard_rows(900)))
        (directory / 'pressure.jsonl').write_text(producer())
        window = {'completed': True, 'cleanupFailures': [], 'sessions': [],
                  'fixtureIdentities': {'selected': 'same-fixture'}, 'standardIdentities': {'agent': 'same-agent'},
                  'fixtureMapping': [{'mappedContainers': 32}], 'targetBindings': [{'identity': 'same-target'}]}
        if phase == 'enabled':
            activity = witness(900)
            for row in activity:
                row['clock']['monotonicNanos'] += 10 * SECOND
                row['snapshot'] = empty_snapshot(row['index'])
            for index, kind, offset in pressure_slots(profile):
                first = attached(index, offset + 1)
                for i in range(offset + 1, offset + 30):
                    activity[i]['snapshot'] = attached(index, i)
                client = Fixture(directory, 'paused-reader' if kind == 'paused-pressure' else 'ring-flood')
                mode = 'paused-reader' if kind == 'paused-pressure' else 'ceiling'
                client.native(f'delivery-pressure-{index:03}', 'delivery', '--observation', mode)
                client.burst()
                lines = client.path.read_text().splitlines()
                value = json.loads(lines[1])
                value['observation']['counts'].update(produced=122880, sampled=112880)
                if kind == 'paused-pressure':
                    value['pause'].update(startedUnixNano=BASE + int((offset + .5) * SECOND),
                                          endedUnixNano=BASE + int((offset + 5.5) * SECOND))
                raw = lines[0] + '\n' + json.dumps(value) + '\n'
                client.path.write_text(raw)
                evaluate = evaluate_paused_reader_session if kind == 'paused-pressure' else evaluate_ceiling_session
                observed = evaluate(raw, expected_reason='event_limit', max_events=10000,
                                    max_output_bytes=8 << 20, exit_code=1)
                record = {'index': index, 'case': kind, 'completed': True, 'zeroOwnedState': True,
                    'requestedClock': clock_at(offset), 'attachmentWitness': first,
                    'activeExpiresAtNanos': BASE + (offset + 30) * SECOND,
                    'deliverySHA256': digest(raw.encode()), 'deliveryExitCode': 1, 'observation': observed,
                    'saturation': saturation(observed, 10000), 'sessionChecksPassed': True,
                    'cleanupWitness': {'snapshot': empty_snapshot(offset + 30.1),
                                       'capturedObjects': first['objects'], 'remaining': {'map': 0, 'prog': 0, 'link': 0}},
                    'cancelCleanupElapsedNanos': 100000000, 'cancelCleanupBudgetPassed': True,
                    'deadlineCleanupUpperNanos': 100001000, 'deadlineCleanupBudgetPassed': True,
                    'scheduledOffsetSeconds': offset, 'slotLatenessNanos': 0}
                window['sessions'].append(record)
            (directory / 'witness.jsonl').write_text(stream(activity))
        write_scheduler(directory, profile['windowSeconds'], window)
        write_verifier(directory, profile['windowSeconds'], window, phase)
        (directory / 'window.private.json').write_text(json.dumps(window))
        write_envelope(directory, phase, 1, profile, cfg, BOOT)
    return profile, cfg
