"""Short native producer checks; not the long paired pressure qualification."""
import subprocess

from pressure_workload import PressureWorkload
from verify_pressure import validate_pressure_session


def verify_pressure(common, image):
    command = common + ['--interactive', '--entrypoint', '/usr/local/bin/kml-io-workload', image]
    workload = PressureWorkload(command + ['pressure', '2'], 2)
    try:
        if workload.process.poll() is not None:
            raise RuntimeError('pressure fixture exited before its command')
        workload.start()
        observation = workload.collect()
        workload.finish()
    finally:
        workload.close()
    result = subprocess.run(command + ['pressure', '1'], input='RQ', capture_output=True,
                            text=True, timeout=15)
    if result.returncode or result.stderr:
        raise RuntimeError('native pressure session failed')
    direct = validate_pressure_session(result.stdout, 1)
    for args in (['pressure'], ['pressure', '0'], ['pressure', '-1'], ['pressure', '1801'],
                 ['pressure', '99999999999999999999'], ['pressure', '1', 'extra']):
        result = subprocess.run(command + args, capture_output=True, text=True, timeout=15)
        if result.returncode != 2 or result.stdout or result.stderr not in (
                'workload failed: pressure arguments\n', 'workload failed: series arguments\n'):
            raise RuntimeError('pressure arguments were not bounded')
    result = subprocess.run(command + ['pressure', '1'], input='X', capture_output=True,
                            text=True, timeout=15)
    if (result.returncode != 2 or result.stdout != '{"ready":true}\n'
            or result.stderr != 'workload failed: bounded command\n'):
        raise RuntimeError('invalid pressure command was not rejected')
    return [{'continuousPressure': True, 'observation': observation, 'retainedUntilCompletion': True},
            {'pressureSession': direct}, {'negativeCase': 'pressure arguments and start command', 'passed': True}]


def verify_corrupt_pressure(common, image):
    result = subprocess.run(common + ['--interactive', '--entrypoint', '/usr/local/bin/kml-io-workload',
        image, 'pressure', '1'], input='R', capture_output=True, text=True, timeout=15)
    if (result.returncode != 2 or result.stdout != '{"ready":true}\n'
            or result.stderr != 'workload failed: data integrity\n'):
        raise RuntimeError('pressure accepted corrupt fixture data')
    return {'negativeCase': 'pressure corrupted bytes', 'passed': True}
