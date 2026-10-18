"""Offline Helm and rendered-contract checks using caller-pinned tools."""
from pathlib import Path
import sys

from manifest import IMAGE_REPOSITORY, require

# This module is part of the trusted consumer checkout, never the bundle.
sys.path.insert(0, str(Path(__file__).resolve().parent.parent/'trace-chart-contract'))
from fixtures import write_fixtures


def verify_chart(package, manifest, helm, checker, private):
    private = Path(private)
    fixtures = private/'chart-fixtures'
    fixtures.mkdir(mode=0o700)
    expected = IMAGE_REPOSITORY+'@'+manifest.image_digest
    write_fixtures(fixtures, expected, True)
    code, _ = helm.run(['lint', '--strict', str(package), '--namespace', 'trace-admin',
                        '-f', str(fixtures/'valid.json')], private)
    require(code == 0, 'packaged chart lint failed')
    code, disabled = helm.run(['template', 'trial', str(package)], private)
    require(code == 0, 'disabled chart rendering failed')
    disabled_path = fixtures/'disabled.yaml'
    disabled_path.write_bytes(disabled)
    for release, namespace, values in [('trial', 'trace-admin', 'valid.json'), ('123', '456', 'numeric.json')]:
        code, enabled = helm.run(['template', release, str(package), '--namespace', namespace,
                                  '-f', str(fixtures/values)], private)
        require(code == 0, 'enabled chart rendering failed')
        enabled_path = fixtures/'enabled.yaml'
        enabled_path.write_bytes(enabled)
        code, _ = checker.run([str(disabled_path), str(enabled_path), namespace, expected], private)
        require(code == 0, 'rendered chart contract failed')
    for invalid in sorted(fixtures.glob('invalid-*.json')):
        code, _ = helm.run(['template', 'trial', str(package), '--namespace', 'trace-admin',
                            '-f', str(invalid)], private)
        require(code == 1, 'invalid chart configuration rendered')
    for namespace in ('default', 'kube-system', 'kube-public', 'kube-node-lease', 'kube-memlens'):
        code, _ = helm.run(['template', 'trial', str(package), '--namespace', namespace,
                            '-f', str(fixtures/'valid.json')], private)
        require(code == 1, 'chart accepted a reserved namespace')
    return {'image': expected, 'enabledProfiles': 2, 'invalidConfigurations': len(list(fixtures.glob('invalid-*.json'))),
            'reservedNamespaces': 5, 'clusterContacted': False}
