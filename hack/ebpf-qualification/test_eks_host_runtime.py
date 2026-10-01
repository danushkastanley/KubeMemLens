"""Provider identity negatives; these synthetic API records are not cloud evidence."""
import base64
from copy import deepcopy
import unittest
import tempfile
from pathlib import Path
from unittest.mock import patch

from eks_host_runtime import EKSHostRuntime, provider_configuration
from local_runtime import Runtime, digest


def documents():
    ca = b'unit-test-public-ca'
    info = {'architecture': 'amd64', 'operatingSystem': 'linux', 'osImage': 'Amazon Linux 2023.9',
            'kernelVersion': '6.12.1', 'containerRuntimeVersion': 'containerd://2.1.0', 'kubeletVersion': 'v1.36.4'}
    p = {'schemaVersion': 1, 'accountID': '123456789012', 'region': 'us-east-1', 'clusterName': 'owned-test',
         'clusterCreatedAt': '2026-10-01T00:00:00Z', 'clusterEndpoint': 'https://owned.example.eks.amazonaws.com',
         'clusterCASHA256': digest(ca), 'nodegroup': 'owned-workers', 'instanceID': 'i-' + 'a' * 17,
         'amiID': 'ami-' + 'b' * 17, 'instanceType': 'm6i.large', 'availabilityZone': 'us-east-1a',
         'bootID': '10000000-0000-0000-0000-000000000001', 'nodeInfo': info}
    cfg = {'providerExecution': p, 'node': 'ip-10-0-0-1.ec2.internal', 'nodeUID': 'node-lifetime',
           'context': 'owned-eks-test', 'namespace': 'owned-trace', 'kubeconfig': '/private/config'}
    cluster = {'arn': 'arn:aws:eks:us-east-1:123456789012:cluster/owned-test', 'status': 'ACTIVE',
               'createdAt': p['clusterCreatedAt'], 'endpoint': p['clusterEndpoint'],
               'certificateAuthority': {'data': base64.b64encode(ca).decode()}}
    config = {'clusters': [{'cluster': {'server': p['clusterEndpoint'], 'certificate-authority-data': base64.b64encode(ca).decode()}}]}
    instance = {'InstanceId': p['instanceID'], 'ImageId': p['amiID'], 'InstanceType': p['instanceType'],
                'State': {'Name': 'running'}, 'Architecture': 'x86_64', 'Placement': {'AvailabilityZone': p['availabilityZone']},
                'Tags': [{'Key': 'aws:autoscaling:groupName', 'Value': 'owned-asg'}]}
    group = {'clusterName': p['clusterName'], 'nodegroupName': p['nodegroup'], 'status': 'ACTIVE',
             'amiType': 'AL2023_x86_64_STANDARD', 'resources': {'autoScalingGroups': [{'name': 'owned-asg'}]}}
    node = {'metadata': {'uid': cfg['nodeUID'], 'labels': {'eks.amazonaws.com/nodegroup': p['nodegroup'],
                                                       'topology.kubernetes.io/region': p['region']}},
            'spec': {'providerID': 'aws:///us-east-1a/' + p['instanceID']},
            'status': {'nodeInfo': {**info, 'bootID': p['bootID']}, 'conditions': [{'type': 'Ready', 'status': 'True'}]}}
    return cfg, {'cluster': cluster, 'config': config, 'instance': instance, 'group': group, 'node': node}


def verify(cfg, docs):
    runtime = EKSHostRuntime.__new__(EKSHostRuntime)
    runtime.cfg, runtime.provider = cfg, provider_configuration(cfg['providerExecution'])
    def aws(*args):
        return {'sts': {'Account': '123456789012'},
                'ec2': {'Reservations': [{'OwnerId': '123456789012', 'Instances': [docs['instance']]}]},
                'eks': {'cluster': docs['cluster'], 'nodegroup': docs['group']}}[args[0]]
    runtime.aws = aws
    runtime.json = lambda args: docs['config'] if args[0] == 'config' else docs['node']
    runtime.verify_provider()
    return runtime


class EKSHostRuntimeTests(unittest.TestCase):
    def test_exact_live_api_binding_accepts_dotted_node(self):
        cfg, docs = documents()
        verify(cfg, docs)

    def test_api_clients_use_the_same_verified_provider_endpoint_and_ca(self):
        cfg, docs = documents()
        runtime = verify(cfg, docs)
        self.assertEqual(runtime.observer_server(), cfg['providerExecution']['clusterEndpoint'])
        with patch.object(runtime, 'verify_kubeconfig_binding') as binding:
            self.assertEqual(runtime.api_cluster(), docs['config']['clusters'][0]['cluster'])
            binding.assert_called_once()
            docs['config']['clusters'][0]['cluster']['server'] = 'https://other.example'
            with self.assertRaisesRegex(ValueError, 'verified EKS binding'):
                runtime.api_cluster()

    def test_cloud_and_node_lifetime_mismatches_are_rejected(self):
        mutations = [
            lambda d: d['cluster'].update(arn='arn:aws:eks:us-east-1:999999999999:cluster/owned-test'),
            lambda d: d['cluster'].update(createdAt='2026-10-02T00:00:00Z'),
            lambda d: d['cluster'].update(endpoint='https://other.example'),
            lambda d: d['cluster'].update(status='DELETING'),
            lambda d: d['instance'].update(ImageId='ami-' + 'c' * 17),
            lambda d: d['instance'].update(InstanceType='t3.micro'),
            lambda d: d['instance'].update(Tags=[]),
            lambda d: d['instance']['State'].update(Name='stopped'),
            lambda d: d['group'].update(amiType='AL2_x86_64'),
            lambda d: d['node']['metadata'].update(uid='replacement'),
            lambda d: d['node']['spec'].update(providerID='aws:///us-east-1a/i-' + 'c' * 17),
            lambda d: d['node']['status']['nodeInfo'].update(bootID='replacement'),
            lambda d: d['node']['status']['nodeInfo'].update(kernelVersion='different'),
            lambda d: d['node']['metadata']['labels'].update({'eks.amazonaws.com/nodegroup': 'foreign'}),
        ]
        for mutate in mutations:
            cfg, docs = documents()
            mutate(docs)
            with self.subTest(mutation=mutations.index(mutate)), self.assertRaises(ValueError):
                verify(cfg, docs)

    def test_kubeconfig_cannot_override_endpoint_trust_or_proxy(self):
        for changed in ({'insecure-skip-tls-verify': True}, {'proxy-url': 'http://foreign'},
                        {'tls-server-name': 'foreign'}, {'server': 'https://foreign'},
                        {'certificate-authority-data': base64.b64encode(b'foreign').decode()}):
            cfg, docs = documents()
            docs['config']['clusters'][0]['cluster'].update(changed)
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                verify(cfg, docs)

    def test_non_host_execution_fails_before_aws_or_kubernetes(self):
        cfg, _ = documents()
        with patch('eks_host_runtime.platform.system', return_value='Darwin'), patch('eks_host_runtime.command') as command:
            with self.assertRaisesRegex(ValueError, 'node host'):
                EKSHostRuntime(cfg)
            command.assert_not_called()

    def test_host_boot_kernel_root_and_distribution_are_bound(self):
        cfg, _ = documents()
        runtime = EKSHostRuntime.__new__(EKSHostRuntime)
        runtime.provider = cfg['providerExecution']
        defaults = {'system': 'Linux', 'machine': 'x86_64', 'uid': 0, 'root': True,
                    'init': 'systemd', 'boot': runtime.provider['bootID'], 'kernel': '6.12.1',
                    'release': {'ID': 'amzn', 'VERSION_ID': '2023'}, 'cgroup': True}
        variants = [{}] + [{key: value} for key, value in (
            ('system', 'Darwin'), ('machine', 'aarch64'), ('uid', 1000), ('root', False),
            ('init', 'sh'), ('boot', 'other-boot'), ('kernel', 'other-kernel'),
            ('release', {'ID': 'debian', 'VERSION_ID': '13'}), ('cgroup', False))]
        for changed in variants:
            values = {**defaults, **changed}
            with patch('eks_host_runtime.platform.system', return_value=values['system']), \
                 patch('eks_host_runtime.platform.machine', return_value=values['machine']), \
                 patch('eks_host_runtime.platform.release', return_value=values['kernel']), \
                 patch('eks_host_runtime.platform.freedesktop_os_release', return_value=values['release']), \
                 patch('eks_host_runtime.os.geteuid', return_value=values['uid']), \
                 patch('eks_host_runtime.os.path.samefile', return_value=values['root']), \
                 patch('eks_host_runtime.Path.is_file', return_value=values['cgroup']), \
                 patch('eks_host_runtime.Path.read_text', side_effect=[values['init'], values['boot']]):
                if changed:
                    with self.subTest(changed=changed), self.assertRaises(ValueError):
                        runtime.verify_host()
                else:
                    runtime.verify_host()

    def test_private_kubeconfig_is_required_before_provider_requests(self):
        cfg, _ = documents()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'config'
            path.write_text('private configuration'); path.chmod(0o644)
            cfg['kubeconfig'] = str(path)
            with patch.object(EKSHostRuntime, 'verify_host'), \
                 patch.object(EKSHostRuntime, 'verify_provider') as provider, \
                 patch.object(EKSHostRuntime, 'verify_tools'):
                with self.assertRaisesRegex(ValueError, 'private owned'):
                    EKSHostRuntime(cfg)
                provider.assert_not_called()
                path.chmod(0o600)
                runtime = EKSHostRuntime(cfg)
                provider.assert_called_once()
                self.assertFalse(runtime.environment_fields()['sharedKindKernel'])
                self.assertEqual(runtime.environment_fields()['provider'], 'eks-managed-al2023-amd64')
                path.write_text('changed private configuration')
                with self.assertRaisesRegex(ValueError, 'kubeconfig changed'):
                    runtime.verify_kubeconfig_binding()
                link = Path(directory) / 'linked-config'; link.symlink_to(path)
                cfg['kubeconfig'] = str(link)
                with self.assertRaises(OSError):
                    EKSHostRuntime(cfg)

    def test_native_argv_and_stdin_are_not_shell_interpolated(self):
        cfg, docs = documents()
        runtime = verify(cfg, docs)
        args = ['sh', '-ec', 'cat > "$1"', '--', '/tmp/owned input']
        with patch.object(runtime, 'verify_host'), patch('eks_host_runtime.command', return_value=b'ok') as command:
            self.assertEqual(runtime.exec(args, b'private input', timeout=12), b'ok')
            command.assert_called_once_with(args, b'private input', 12)
            self.assertEqual(runtime.observer_command(['/usr/local/bin/kml-observer', '--config', '/tmp/owned']),
                             ['/usr/local/bin/kml-observer', '--config', '/tmp/owned'])

    def test_provider_mode_is_not_a_fallback_from_kind(self):
        cfg, _ = documents()
        with patch('local_runtime.command') as command:
            with self.assertRaises(ValueError):
                Runtime(cfg)
            command.assert_not_called()
        runtime = Runtime.__new__(Runtime)
        runtime.cfg = {'node': 'owned-worker'}
        self.assertEqual(runtime.observer_command(['observer', '--config', '/tmp/input']),
                         ['docker', 'exec', 'owned-worker', 'observer', '--config', '/tmp/input'])

    def test_ambiguous_provider_configuration_fails(self):
        cfg, _ = documents()
        for updates in ({'schemaVersion': True}, {'region': '--profile'}, {'instanceID': 'i-short'},
                        {'clusterEndpoint': 'http://owned'}, {'nodeInfo': {}}, {'extra': True}):
            value = deepcopy(cfg['providerExecution']); value.update(updates)
            with self.subTest(updates=updates), self.assertRaises(ValueError):
                provider_configuration(value)


if __name__ == '__main__':
    unittest.main()
