import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


class RecoveryClockTests(unittest.TestCase):
    def test_lifecycle_preserves_recovery_and_waits_for_rollback_readiness(self):
        script = Path(__file__).resolve().parents[1] / 'lib/volume-stats-lifecycle.sh'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            clock = root / 'clock'
            clock.write_text('1000000000')
            # Control only the clock in child Python processes. Cluster commands
            # are stubbed below; the production timing and JSON writer run intact.
            (root / 'sitecustomize.py').write_text('''import os,time
from pathlib import Path
def tick():
    path=Path(os.environ['KML_RECOVERY_CLOCK_FILE'])
    value=int(path.read_text())
    path.write_text(str(value+125000000))
    return value
time.monotonic_ns=tick
''')
            sample = {'context': {'volumes': [{'usage': {
                'filesystem': {'capturedAt': '2026-10-18T00:00:00Z'}
            }}]}}
            for name in ('volume-producer-restarted.json', 'volume-stale.json'):
                (root / name).write_text(json.dumps(sample))
            environment = dict(os.environ, PYTHONPATH=str(root),
                               KML_RECOVERY_CLOCK_FILE=str(clock))
            subprocess.run(['bash', '-euo', 'pipefail', '-c', '''
source "$1"
work_dir=$2
namespace=fixture
volume_path=/fixture
helm_args=(fixture)
volume_args=(fixture)
rollback_pending=0
kctl() { :; }
helm() {
  # Helm can finish before the aggregation layer serves the new collector.
  if [[ "$*" == *"nodeContext.enabled=true"* ]]; then rollback_pending=1; fi
}
volume_wait() { :; }
node_context_expect_status() {
  if [[ "$2" == admin && "$rollback_pending" == 1 ]]; then
    echo 'rollback aggregation is unavailable, not a route 404' >&2
    return 1
  fi
}
node_context_wait_read() {
  if [[ "$2" == admin && "$3" == /clusterstatus/current ]]; then rollback_pending=0; fi
}
volume_stats_lifecycle
''', 'recovery-clock', str(script), str(root)], env=environment,
                           check=True, capture_output=True, text=True, timeout=10)
            result = json.loads((root / 'volume-lifecycle.json').read_text())
            self.assertEqual(result['recovery'],
                             dict.fromkeys(('collector', 'producer', 'source', 'pod'), 0.125))


if __name__ == '__main__':
    unittest.main()
