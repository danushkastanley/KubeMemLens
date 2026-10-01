"""Exercise the finite burst against the test-owned prepared fixture."""
import subprocess

from gated import GatedWorkload
from verify_flood import validate_flood


def verify_flood(common, image):
    results = []
    command = common + ['--interactive', '--entrypoint', '/usr/local/bin/kml-io-workload', image]
    for count in (1, 131072, 262144):
        workload = GatedWorkload(command + ['flood', str(count)])
        try:
            if workload.process.poll() is not None:
                raise RuntimeError('flood fixture exited before its command')
            receipt = validate_flood(workload.run(), count)
            if workload.process.poll() is not None:
                raise RuntimeError('flood fixture did not retain its gated lifetime')
            workload.finish()
            results.append({'flood': True, 'receipt': receipt, 'retainedUntilCompletion': True})
        finally:
            workload.close()
    for args in (['flood'], ['flood', '0'], ['flood', '-1'], ['flood', '262145'],
                 ['flood', '99999999999999999999'], ['flood', '1', 'extra']):
        result = subprocess.run(command + args, capture_output=True, text=True, timeout=15)
        if (result.returncode != 2 or result.stdout or result.stderr not in
                ('workload failed: flood arguments\n', 'workload failed: series arguments\n')):
            raise RuntimeError('invalid flood arguments were not rejected')
    result = subprocess.run(command + ['flood', '1'], input='X', capture_output=True, text=True, timeout=15)
    if (result.returncode != 2 or result.stdout != '{"ready":true}\n'
            or result.stderr != 'workload failed: bounded command\n'):
        raise RuntimeError('invalid flood start command was not rejected')
    results.append({'negativeCase': 'flood arguments and start command', 'passed': True})
    return results


def verify_corrupt_flood(common, image):
    result = subprocess.run(common + ['--interactive', '--entrypoint', '/usr/local/bin/kml-io-workload',
                                     image, 'flood', '1'], input='R', capture_output=True, text=True, timeout=15)
    if (result.returncode != 2 or result.stdout != '{"ready":true}\n'
            or result.stderr != 'workload failed: data integrity\n'):
        raise RuntimeError('flood accepted corrupt fixture data')
    return {'negativeCase': 'flood corrupted bytes', 'passed': True}
