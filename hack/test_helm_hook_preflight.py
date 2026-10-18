"""Verify hook image validation, owned-node scope and bounded pull failures."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
IMAGE = 'public.ecr.aws/docker/library/busybox@sha256:' + 'a' * 64


def hook(image=IMAGE):
    labels = {'app.kubernetes.io/name': 'kube-memlens-test'}
    spec = {'automountServiceAccountToken': False, 'restartPolicy': 'Never',
            'securityContext': {'runAsNonRoot': True, 'runAsUser': 65532,
                                'runAsGroup': 65532, 'seccompProfile': {'type': 'RuntimeDefault'}},
            'containers': [{'name': 'connection', 'image': image,
                            'resources': {'requests': {'cpu': '1m', 'memory': '4Mi'},
                                          'limits': {'memory': '16Mi'}},
                            'securityContext': {'privileged': False, 'readOnlyRootFilesystem': True,
                                                'allowPrivilegeEscalation': False,
                                                'capabilities': {'drop': ['ALL']}}}]}
    return {'kind': 'Job', 'metadata': {'labels': labels},
            'spec': {'backoffLimit': 0, 'activeDeadlineSeconds': 300,
                     'template': {'metadata': {'labels': dict(labels)}, 'spec': spec}}}


class HookPreflightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        self.bin = self.path / 'bin'
        self.bin.mkdir()
        self.fixture = self.path / 'hook.json'
        self.fixture.write_text(json.dumps(hook()))
        self.log = self.path / 'calls.jsonl'
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                        HOOK_FIXTURE=str(self.fixture), HOOK_CALLS=str(self.log),
                        HOOK_OWNER='owned', HOOK_CACHED='1', HOOK_PULL_FAIL='0')
        self.command('helm', 'from pathlib import Path\nprint(Path(os.environ["HOOK_FIXTURE"]).read_text())')
        self.command('kind', 'print("owned-control-plane\\nowned-worker")')
        self.command('docker', '''
import json,sys
args=sys.argv[1:]
with open(os.environ['HOOK_CALLS'],'a') as f:f.write(json.dumps(args)+'\\n')
if args[0]=='inspect':
 print(os.environ['HOOK_OWNER']);sys.exit(0)
if args[0]!='exec' or args[2]!='timeout':sys.exit(2)
if args[5]=='inspecti':sys.exit(0 if os.environ['HOOK_CACHED']=='1' else 1)
if args[5]!='pull':sys.exit(2)
if os.environ['HOOK_PULL_FAIL']=='1':
 print('unauthorized private-registry-secret',file=sys.stderr);sys.exit(1)
''')

    def command(self, name, source):
        path = self.bin / name
        path.write_text('#!/usr/bin/env python3\nimport os\n' + source + '\n')
        path.chmod(0o755)

    def run_preflight(self):
        result = subprocess.run(['bash', '-c',
            'source hack/lib/helm-hook-preflight.sh; sleep() { :; }; '
            'prefetch_helm_hook_image owned ./charts/kube-memlens "$1"',
            '--', str(self.path)], cwd=ROOT, env=self.env, text=True, capture_output=True, timeout=15)
        calls = [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []
        return result, calls

    def test_cached_image_requires_no_pull_on_either_owned_node(self):
        result, calls = self.run_preflight()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum('inspecti' in c for c in calls), 2)
        self.assertFalse(any('pull' in c for c in calls))
        self.assertTrue(all(c[-1] == IMAGE for c in calls if c[0] == 'exec'))

    def test_missing_image_is_pulled_with_deadline_for_each_owned_node(self):
        self.env['HOOK_CACHED'] = '0'
        result, calls = self.run_preflight()
        self.assertEqual(result.returncode, 0, result.stderr)
        pulls = [c for c in calls if 'pull' in c]
        self.assertEqual(len(pulls), 2)
        self.assertTrue(all(c[2:] == ['timeout', '30s', 'crictl', 'pull', IMAGE] for c in pulls))

    def test_foreign_node_is_rejected_before_any_exec(self):
        self.env['HOOK_OWNER'] = 'another-cluster'
        result, calls = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c[0] == 'exec' for c in calls))

    def test_registry_failure_stops_after_three_attempts_without_raw_output(self):
        self.env.update(HOOK_CACHED='0', HOOK_PULL_FAIL='1')
        result, calls = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sum('pull' in c for c in calls), 3)
        self.assertIn('registry-denied', result.stderr)
        self.assertNotIn('private-registry-secret', result.stdout + result.stderr)

    def test_unpinned_image_is_rejected_before_docker(self):
        self.fixture.write_text(json.dumps(hook('busybox:latest')))
        result, calls = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_multiple_hook_containers_are_rejected_before_docker(self):
        document = hook()
        containers = document['spec']['template']['spec']['containers']
        containers.append(containers[0].copy())
        self.fixture.write_text(json.dumps(document))
        result, calls = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_standalone_pod_is_rejected_before_docker(self):
        document = hook()
        document.update(kind='Pod', spec=document['spec']['template']['spec'])
        self.fixture.write_text(json.dumps(document))
        result, calls = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_job_retries_deadline_and_policy_selector_are_required(self):
        for field, value in [('backoffLimit', 1), ('activeDeadlineSeconds', 0)]:
            with self.subTest(field=field):
                document = hook()
                document['spec'][field] = value
                self.fixture.write_text(json.dumps(document))
                result, calls = self.run_preflight()
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])
        document = hook()
        document['spec']['template']['metadata']['labels'] = {}
        self.fixture.write_text(json.dumps(document))
        result, calls = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])


if __name__ == '__main__':
    unittest.main()
