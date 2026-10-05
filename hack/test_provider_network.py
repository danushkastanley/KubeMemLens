"""Exercise provider network contracts without contacting a Kubernetes cluster."""
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
IMAGE = json.loads((ROOT / 'hack/provider-probe-image.json').read_text())['image']


class ProviderNetworkTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        self.values = ROOT / 'hack/provider-values/eks-al2023-containerd-amd64.yaml'
        self.service = self.render('service.yaml')
        self.write('service.json', self.service)
        self.write('networkpolicy.json', self.render('networkpolicy.yaml'))

    def write(self, name, value):
        (self.path / name).write_text(json.dumps(value))

    def render(self, template):
        raw = subprocess.check_output(['helm', 'template', 'fixture', 'charts/kube-memlens',
                                       '--values', str(self.values), '--set', 'namespace.name=fixture',
                                       '--show-only', 'templates/' + template], cwd=ROOT)
        return json.loads(subprocess.check_output(['ruby', '-ryaml', '-rjson', '-e',
            'puts JSON.generate(YAML.load_stream(STDIN.read).compact.first)'], input=raw))

    def run_shell(self, body):
        prelude = '''set -euo pipefail
source hack/lib/provider-qualification-live.sh
namespace=fixture
release=fixture
chart_archive=charts/kube-memlens
work_dir=$KML_CASE
values_path=$KML_VALUES
probe_image=$KML_IMAGE
policy_node=node-a
fail() { echo "$*" >&2; exit 1; }
k() { cat "$work_dir/$2.json"; }
'''
        return subprocess.run(['bash', '-c', prelude + body], cwd=ROOT, capture_output=True, text=True,
            timeout=20, env=dict(os.environ, KML_CASE=str(self.path), KML_VALUES=str(self.values), KML_IMAGE=IMAGE))

    def test_eks_and_default_service_ports_follow_rendered_chart(self):
        self.assertEqual(self.service['spec']['ports'][0]['port'], 8443)
        self.assertEqual(self.run_shell('assert_live_network_resources').returncode, 0)
        self.values = self.path / 'default-values.yaml'
        self.values.write_text('{}\n')
        service = self.render('service.yaml')
        self.assertEqual(service['spec']['ports'][0]['port'], 443)
        self.write('service.json', service)
        self.assertEqual(self.run_shell('assert_live_network_resources').returncode, 0)

    def test_unexpected_port_name_target_or_exposure_is_rejected(self):
        for field, value in [('port', 443), ('name', 'wrong'), ('targetPort', 'http')]:
            with self.subTest(field=field):
                service = copy.deepcopy(self.service)
                service['spec']['ports'][0][field] = value
                self.write('service.json', service)
                self.assertNotEqual(self.run_shell('assert_live_network_resources').returncode, 0)
        for change in ('extra-port', 'NodePort'):
            with self.subTest(change=change):
                service = copy.deepcopy(self.service)
                if change == 'extra-port':
                    service['spec']['ports'].append({'port': 8080, 'name': 'http', 'targetPort': 'http'})
                else:
                    service['spec']['type'] = change
                self.write('service.json', service)
                self.assertNotEqual(self.run_shell('assert_live_network_resources').returncode, 0)

    def test_probe_is_one_bounded_controller_owned_attempt(self):
        for access in ('allowed', 'denied'):
            with self.subTest(access=access):
                result = self.run_shell('k() { cat > "$work_dir/probe.json"; }\napply_policy_probe probe ' + access)
                self.assertEqual(result.returncode, 0, result.stderr)
                job = json.loads((self.path / 'probe.json').read_text())
                self.assertEqual(job['kind'], 'Job')
                self.assertEqual((job['spec']['backoffLimit'], job['spec']['activeDeadlineSeconds']), (0, 60))
                template = job['spec']['template']
                self.assertEqual(template['metadata']['labels']['qualification.kubememlens.io/access'], access)
                self.assertFalse(template['spec']['automountServiceAccountToken'])
                self.assertEqual(template['spec']['nodeName'], 'node-a')

    def pod(self, phase):
        return {'metadata': {'ownerReferences': [{'uid': 'job-uid', 'kind': 'Job', 'controller': True}]},
                'status': {'phase': phase, 'containerStatuses': [{'imageID': IMAGE,
                    'state': {'terminated': {'reason': 'Completed' if phase == 'Succeeded' else 'Error',
                             'exitCode': 0 if phase == 'Succeeded' else 1,
                             'startedAt': '2026-01-01T00:00:00Z', 'finishedAt': '2026-01-01T00:00:01Z'}}}]}}

    def probe_result(self, pods, expected):
        self.write('pods.json', {'items': pods})
        return self.run_shell('''k() {
  if [ "$2" = job ]; then echo job-uid; else cat "$work_dir/pods.json"; fi
}
sleep() { :; }
wait_for_probe_phase probe ''' + expected)

    def test_owned_executed_success_and_denial_are_accepted(self):
        for phase in ('Succeeded', 'Failed'):
            with self.subTest(phase=phase):
                result = self.probe_result([self.pod(phase)], phase)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_unowned_duplicate_unstarted_or_oom_pod_is_not_a_denial(self):
        wrong = self.pod('Failed')
        wrong['metadata']['ownerReferences'][0]['uid'] = 'another-job'
        oom = self.pod('Failed')
        oom['status']['containerStatuses'][0]['state']['terminated']['reason'] = 'OOMKilled'
        killed = self.pod('Failed')
        killed['status']['containerStatuses'][0]['state']['terminated'].update(exitCode=143, signal=15)
        waiting = self.pod('Pending')
        waiting['status']['containerStatuses'][0]['state'] = {'waiting': {'reason': 'ImagePullBackOff'}}
        for pods in ([wrong], [oom], [killed], [waiting], [self.pod('Failed'), self.pod('Failed')]):
            with self.subTest(pods=pods):
                self.assertNotEqual(self.probe_result(pods, 'Failed').returncode, 0)

    def test_full_fixture_selects_controller_pods_without_granting_server_ingress(self):
        result = self.run_shell('''assert_live_network_resources() { :; }
wait_for_probe_phase() { :; }
sleep() { :; }
k() {
  case "$1" in
    apply) local raw name; raw=$(cat); name=$(jq -r '.kind + "-" + .metadata.name' <<< "$raw")
      printf '%s' "$raw" > "$work_dir/$name.json" ;;
    get) echo '{"items":[{"spec":{"nodeName":"node-a"}}]}' ;;
    rollout|delete) : ;;
    *) exit 1 ;;
  esac
}
verify_network_policy_enforcement
''')
        self.assertEqual(result.returncode, 0, result.stderr)
        server = json.loads((self.path / 'Deployment-qualification-policy-target.json').read_text())
        self.assertEqual(server['spec']['replicas'], 1)
        self.assertEqual(server['spec']['selector']['matchLabels'], server['spec']['template']['metadata']['labels'])
        clients = json.loads((self.path / 'NetworkPolicy-qualification-policy-clients.json').read_text())
        self.assertEqual(clients['spec']['policyTypes'], ['Ingress'])
        self.assertEqual(clients['spec']['ingress'], [])
        target = json.loads((self.path / 'NetworkPolicy-qualification-policy-target.json').read_text())
        self.assertEqual(target['spec']['ingress'][0]['from'][0]['podSelector']['matchLabels'],
                         {'qualification.kubememlens.io/access': 'allowed'})


if __name__ == '__main__':
    unittest.main()
