"""Consume development CI artefacts using identities transferred by the build job."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess

from consumer import verify_bundle
from manifest import canonical, require
from signatures import LocalTrust


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    temporary = Path(os.environ['RUNNER_TEMP']).resolve()
    inputs, tools = temporary/'trace-input', temporary/'trace-consumer-tools'
    commit = os.environ['EXPECTED_SOURCE_COMMIT']
    require(subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() == commit,
            'consumer source differs from build')
    require(sha(inputs/'bundle/trace-release.json') == os.environ['EXPECTED_MANIFEST_SHA256'],
            'downloaded manifest differs from build output')
    cosign = shutil.which('cosign')
    require(cosign is not None, 'Cosign unavailable')
    paths = {'cosign': Path(cosign).resolve(), 'inspector': tools/'inspector',
             'helm': tools/'helm', 'chart-checker': tools/'chart-checker'}
    receipt = verify_bundle(inputs/'bundle', temporary/'trace-verified',
                            {name: (path, sha(path)) for name, path in paths.items()},
                            LocalTrust(inputs/'local.pub', os.environ['EXPECTED_PUBLIC_KEY_SHA256'], '0.1.0-dev.1', commit))
    # The development key proves this dry run, never a public release identity.
    require(receipt['authority'] == 'local-development' and not receipt['installationPerformed'],
            'unexpected development verification authority')
    with (temporary/'trace-consumer-receipt.json').open('xb') as output:
        output.write(canonical(receipt))
    print('Clean consumer verified the development bundle; no installation or release authority granted.')


if __name__ == '__main__':
    main()
