"""Deliberately pause only the owned workload to verify retained deadline failure."""
import json
import select
import subprocess
import time

from verify_series import MIXED_MODES, validate_mixed_series
from verify_workload import validate_timed_observation


def verify_deadline(common, image):
    args = common + ['--entrypoint', '/usr/local/bin/kml-io-workload', image, 'mixed-series', '30', '100']
    process = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    paused = None
    try:
        if not select.select([process.stdout], [], [], 10)[0]:
            raise RuntimeError('deadline fixture did not report its schedule')
        header = process.stdout.readline(4097)
        if len(header) > 4096 or not header.endswith('\n'):
            raise RuntimeError('deadline fixture clock record exceeds bound')
        start = json.loads(header)
        name = common[common.index('--name') + 1]
        obj = json.loads(subprocess.check_output(['docker', 'inspect', name], text=True, timeout=10))[0]
        if obj['Image'] != image or obj['Name'] != '/' + name or not obj['State']['Running']:
            raise RuntimeError('deadline fixture identity changed')
        paused = obj['Id']
        subprocess.run(['docker', 'pause', paused], check=True, capture_output=True, timeout=10)
        # The stop is the test stimulus: hold this owned process beyond its first
        # absolute deadline, rather than changing or relaxing the deadline.
        due_wall = start['wallNanos'] + start['firstDueNanos'] - start['monotonicAfterNanos'] + start['periodNanos']
        delay = (due_wall - time.time_ns()) / 1000000000 + .2
        if not 0 < delay < 7:
            raise RuntimeError('deadline fixture clock is outside test bounds')
        time.sleep(delay)
        subprocess.run(['docker', 'unpause', paused], check=True, capture_output=True, timeout=10)
        paused = None
        remaining, errors = process.communicate(timeout=10)
        if process.returncode != 2 or errors != 'workload failed: series deadline missed\n':
            raise RuntimeError('deliberate missed deadline did not fail explicitly')
        raw = header + remaining
        rows = [json.loads(line) for line in remaining.splitlines()]
        if not 1 <= len(rows) < 30:
            raise RuntimeError('deadline failure lost its partial operations')
        for index, row in enumerate(rows):
            if row['sequence'] != index or row['dueMonotonicNanos'] != start['firstDueNanos'] + index * start['periodNanos']:
                raise RuntimeError('deadline failure changed its operation schedule')
            validate_timed_observation(row['observation'], MIXED_MODES[index % 3])
        last = rows[-1]
        if last['observation']['operationEndedMonotonicNanos'] < last['dueMonotonicNanos'] + start['periodNanos']:
            raise RuntimeError('deadline failure did not retain the late operation')
        try:
            validate_mixed_series(raw, 30, 100)
        except ValueError:
            return {'negativeCase': 'missed deadline retains partial operations and still fails validation',
                    'retainedOperations': len(rows), 'schedule': start, 'lastOperation': last}
        raise RuntimeError('incomplete deadline failure passed series validation')
    finally:
        if paused is not None:
            subprocess.run(['docker', 'unpause', paused], capture_output=True, check=True, timeout=10)
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=10)
        process.stdout.close()
        process.stderr.close()
