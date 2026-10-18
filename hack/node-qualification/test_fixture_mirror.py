"""Only equivalent pinned public fixtures may recover from registry rate limits."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
DIGEST = 'sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028'
ECR = 'public.ecr.aws/docker/library/busybox@' + DIGEST
HUB = 'docker.io/library/busybox@' + DIGEST
DOCKER = '''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
root=Path(os.environ['FIXTURE_DIRECTORY'])
with (root/'calls').open('a') as output:output.write(json.dumps(args)+'\\n')
if args[:4] not in (['exec','owned-node','timeout','10s'],['exec','owned-node','timeout','30s']):sys.exit(90)
requested=os.environ['FIXTURE_REQUESTED']
registered=root/'registered'
if args[4:6]==['crictl','inspecti']:
 if args[-1]==requested:
  sys.exit(0 if registered.exists() and os.environ['FIXTURE_VERIFY']=='yes' else 1)
 sys.exit(0 if os.environ['FIXTURE_CACHED']=='yes' else 1)
if args[4:6]==['crictl','pull']:
 if args[-1]==requested:
  print(os.environ['FIXTURE_ERROR']+' private-registry-secret',file=sys.stderr);sys.exit(1)
 if os.environ['FIXTURE_PULL']=='fail':
  print('429 private-mirror-secret',file=sys.stderr);sys.exit(1)
 (root/'pulled').touch();sys.exit(0)
if args[4:9]==['ctr','-n','k8s.io','images','tag']:
 if os.environ['FIXTURE_TAG']=='fail':sys.exit(1)
 if os.environ['FIXTURE_CACHED']!='yes' and not (root/'pulled').exists():sys.exit(92)
 registered.touch();sys.exit(0)
sys.exit(91)
'''


class FixtureMirrorTests(unittest.TestCase):
    def exercise(self, requested=ECR, error='429 Too Many Requests', cached='no', pull='ok', tag='ok', verify='yes'):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            executable = root/'docker'
            executable.write_text(DOCKER)
            executable.chmod(0o755)
            env = dict(os.environ, PATH=name+os.pathsep+os.environ['PATH'],
                       FIXTURE_DIRECTORY=name, FIXTURE_REQUESTED=requested,
                       FIXTURE_ERROR=error, FIXTURE_CACHED=cached,
                       FIXTURE_PULL=pull, FIXTURE_TAG=tag, FIXTURE_VERIFY=verify)
            result = subprocess.run(['bash', '-c',
                'set -Eeuo pipefail; source hack/lib/node-context-fixture.sh; '
                'sleep() { :; }; node_context_prefetch_image "$1" owned-node "$2"',
                '--', name, requested], cwd=ROOT, env=env, text=True, capture_output=True, timeout=10)
            calls = [json.loads(line) for line in (root/'calls').read_text().splitlines()]
        self.assertNotIn('private-registry-secret', result.stdout+result.stderr)
        self.assertNotIn('private-mirror-secret', result.stdout+result.stderr)
        return result, calls

    def test_rate_limited_ecr_reuses_cached_identical_hub_digest(self):
        result, calls = self.exercise(cached='yes')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([c[-1] for c in calls if c[4:6] == ['crictl', 'pull']], [ECR]*3)
        self.assertEqual(calls[-2], ['exec', 'owned-node', 'timeout', '10s', 'ctr', '-n', 'k8s.io', 'images', 'tag', HUB, ECR])
        self.assertEqual(calls[-1][-3:], ['crictl', 'inspecti', ECR])
        self.assertIn('identical-digest', result.stderr)

    def test_rate_limited_hub_has_one_bounded_ecr_pull(self):
        result, calls = self.exercise(requested=HUB)
        self.assertEqual(result.returncode, 0, result.stderr)
        pulls = [c for c in calls if c[4:6] == ['crictl', 'pull']]
        self.assertEqual([c[-1] for c in pulls], [HUB]*3+[ECR])
        self.assertTrue(all(c[2:4] == ['timeout', '30s'] for c in pulls))
        self.assertEqual(calls[-2][-2:], [ECR, HUB])

    def test_other_failure_classes_never_change_registry(self):
        for reason in ['unauthorized', 'manifest unknown', 'certificate invalid', 'unclassified error']:
            with self.subTest(reason=reason):
                result, calls = self.exercise(error=reason)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(calls), 4)
                self.assertTrue(all(c[-1] == ECR for c in calls))

    def test_arbitrary_repositories_and_other_digests_never_use_mirror(self):
        for requested in [ECR.replace(DIGEST, 'sha256:'+'a'*64), ECR.replace('busybox', 'other'), 'busybox:latest']:
            with self.subTest(requested=requested):
                result, calls = self.exercise(requested=requested)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(calls), 4)
                self.assertTrue(all(c[-1] == requested for c in calls))

    def test_failed_mirror_pull_never_registers_an_alias(self):
        result, calls = self.exercise(pull='fail')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sum(c[4:6] == ['crictl', 'pull'] for c in calls), 4)
        self.assertFalse(any(c[4] == 'ctr' for c in calls))

    def test_alias_collision_is_not_overwritten_or_reported_ready(self):
        result, calls = self.exercise(cached='yes', tag='fail')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls[-1][4], 'ctr')
        self.assertNotIn('--force', calls[-1])
        self.assertNotIn('available through', result.stderr)

    def test_final_runtime_inspection_is_required(self):
        result, calls = self.exercise(cached='yes', verify='no')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls[-1][-3:], ['crictl', 'inspecti', ECR])
        self.assertNotIn('available through', result.stderr)


if __name__ == '__main__':
    unittest.main()
