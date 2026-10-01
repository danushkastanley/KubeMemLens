"""Explicit EKS node-host execution; never a fallback from the local kind runner."""
import base64
import os
from pathlib import Path
import platform
import re
import stat
import uuid
from urllib.parse import urlsplit

from local_runtime import Runtime, canonical, command, deployment_names, digest, read_api_cluster

PROVIDER_FIELDS = {'schemaVersion', 'accountID', 'region', 'clusterName', 'clusterCreatedAt',
                   'clusterEndpoint', 'clusterCASHA256', 'nodegroup', 'instanceID', 'amiID',
                   'instanceType', 'availabilityZone', 'bootID', 'nodeInfo'}
NODE_FIELDS = {'architecture', 'operatingSystem', 'osImage', 'kernelVersion',
               'containerRuntimeVersion', 'kubeletVersion'}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def dns_name(value):
    return (isinstance(value, str) and len(value) <= 253 and all(
        re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in value.split('.')))


def provider_configuration(value):
    require(type(value) is dict and set(value) == PROVIDER_FIELDS, 'exact EKS execution binding required')
    require(type(value['schemaVersion']) is int and value['schemaVersion'] == 1, 'unsupported EKS binding schema')
    patterns = {'accountID': r'[0-9]{12}', 'region': r'[a-z]{2}-[a-z]+-[1-9]',
                'clusterName': r'[A-Za-z0-9][A-Za-z0-9_-]{0,99}',
                'nodegroup': r'[A-Za-z0-9][A-Za-z0-9_-]{0,99}',
                'instanceID': r'i-[a-f0-9]{17}', 'amiID': r'ami-[a-f0-9]{17}',
                'instanceType': r'[a-z0-9]+\.[a-z0-9]+',
                'clusterCASHA256': r'[a-f0-9]{64}'}
    for key, pattern in patterns.items():
        require(isinstance(value[key], str) and re.fullmatch(pattern, value[key]), 'invalid EKS execution selector')
    require(value['availabilityZone'] in {value['region'] + suffix for suffix in 'abcdef'}, 'invalid EKS availability zone')
    require(isinstance(value['clusterCreatedAt'], str) and 1 <= len(value['clusterCreatedAt']) <= 64,
            'cluster creation lifetime required')
    require(isinstance(value['bootID'], str) and str(uuid.UUID(value['bootID'])) == value['bootID'], 'invalid frozen boot identity')
    endpoint = urlsplit(value['clusterEndpoint'])
    require(endpoint.scheme == 'https' and endpoint.hostname and endpoint.port in (None, 443)
            and endpoint.username is None and endpoint.password is None and not endpoint.path
            and not endpoint.query and not endpoint.fragment, 'exact HTTPS cluster endpoint required')
    info = value['nodeInfo']
    require(type(info) is dict and set(info) == NODE_FIELDS
            and all(isinstance(x, str) and 0 < len(x) <= 256 for x in info.values()), 'exact Node runtime binding required')
    require(info['architecture'] == 'amd64' and info['operatingSystem'] == 'linux'
            and info['osImage'].startswith('Amazon Linux 2023')
            and info['containerRuntimeVersion'].startswith('containerd://'), 'EKS AL2023 amd64/containerd binding required')
    return value


class EKSHostRuntime(Runtime):
    """Run on the selected disposable host, with its approved private kubeconfig.

    Installation, credentials, node tools and cloud ownership must be prepared by
    the provider controller. Construction performs only identity/tool checks.
    It neither provisions AWS resources nor grants qualification or publication.
    """
    def __init__(self, cfg):
        self.provider = provider_configuration(cfg['providerExecution'])
        require(dns_name(cfg['node']) and dns_name(cfg['namespace']) and '.' not in cfg['namespace'],
                'invalid owned Kubernetes names')
        require(isinstance(cfg['context'], str) and 0 < len(cfg['context']) <= 256
                and cfg['context'].isascii() and all(32 <= ord(c) < 127 for c in cfg['context'])
                and not cfg['context'].startswith('-'), 'explicit Kubernetes context required')
        self.cfg, self.services = cfg, deployment_names(cfg)
        self.kube = ['kubectl', '--kubeconfig', cfg['kubeconfig'], '--context', cfg['context'], '--request-timeout=15s']
        self.verify_host()
        self.kubeconfig_sha256 = self.verify_kubeconfig_file()
        self.verify_provider()
        self.verify_kubeconfig_binding()
        self.verify_tools()

    def verify_kubeconfig_file(self):
        path = self.cfg['kubeconfig']
        require(isinstance(path, str) and Path(path).is_absolute(), 'absolute private kubeconfig required')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(fd)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid()
                    and not info.st_mode & 0o077 and 0 < info.st_size <= 65536,
                    'kubeconfig must be a bounded private owned regular file')
            data = os.read(fd, 65537)
            require(len(data) == info.st_size, 'kubeconfig changed during binding')
            return digest(data)
        finally:
            os.close(fd)

    def verify_kubeconfig_binding(self):
        require(self.verify_kubeconfig_file() == self.kubeconfig_sha256, 'private kubeconfig changed')

    def verify_host(self):
        require(platform.system() == 'Linux' and platform.machine() == 'x86_64' and os.geteuid() == 0,
                'EKS observer must execute on the selected Linux amd64 node host')
        require(os.path.samefile('/', '/proc/1/root')
                and Path('/proc/1/comm').read_text().strip() == 'systemd', 'node host root and init required')
        release = platform.freedesktop_os_release()
        require(release.get('ID') == 'amzn' and release.get('VERSION_ID') == '2023', 'AL2023 host required')
        require(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self.provider['bootID']
                and platform.release() == self.provider['nodeInfo']['kernelVersion'], 'local Node lifetime or kernel changed')
        require(Path('/sys/fs/cgroup/cgroup.controllers').is_file(), 'unified host cgroup hierarchy required')

    def aws(self, *args):
        import json
        return json.loads(command(['aws', '--no-cli-pager', '--region', self.provider['region'],
                                   '--output', 'json', *args], timeout=30))

    def verify_node(self):
        p = self.provider
        node = self.json(['get', 'node', self.cfg['node'], '-o', 'json'])
        require(node['metadata']['uid'] == self.cfg['nodeUID'] and not node['metadata'].get('deletionTimestamp'),
                'selected Node lifetime changed')
        require(node['spec'].get('providerID') == 'aws:///' + p['availabilityZone'] + '/' + p['instanceID'],
                'selected Node instance changed')
        labels = node['metadata'].get('labels', {})
        require(labels.get('eks.amazonaws.com/nodegroup') == p['nodegroup']
                and labels.get('topology.kubernetes.io/region') == p['region'], 'selected Node pool or region changed')
        require(not node['spec'].get('unschedulable') and any(
            row.get('type') == 'Ready' and row.get('status') == 'True' for row in node['status'].get('conditions', [])),
            'selected Node is not Ready and schedulable')
        info = node['status']['nodeInfo']
        require(all(info.get(k) == v for k, v in p['nodeInfo'].items()) and info.get('bootID') == p['bootID'],
                'selected Node runtime differs from the approved host')

    def verify_provider(self):
        p = self.provider
        require(self.aws('sts', 'get-caller-identity')['Account'] == p['accountID'], 'ambient AWS account differs from binding')
        cluster = self.aws('eks', 'describe-cluster', '--name', p['clusterName'])['cluster']
        arn = 'arn:aws:eks:' + p['region'] + ':' + p['accountID'] + ':cluster/' + p['clusterName']
        require(cluster['arn'] == arn and cluster['status'] == 'ACTIVE'
                and cluster['createdAt'] == p['clusterCreatedAt'] and cluster['endpoint'] == p['clusterEndpoint'],
                'EKS cluster identity or lifetime changed')
        ca = base64.b64decode(cluster['certificateAuthority']['data'], validate=True)
        require(digest(ca) == p['clusterCASHA256'], 'EKS serving CA changed')
        selected = read_api_cluster(self)
        require(selected['server'] == p['clusterEndpoint']
                and base64.b64decode(selected['certificate-authority-data'], validate=True) == ca,
                'kubeconfig endpoint or TLS trust differs from EKS')
        reservations = self.aws('ec2', 'describe-instances', '--instance-ids', p['instanceID'])['Reservations']
        require(len(reservations) == 1 and reservations[0]['OwnerId'] == p['accountID']
                and len(reservations[0]['Instances']) == 1, 'exact EC2 instance ownership required')
        instance = reservations[0]['Instances'][0]
        require(instance['InstanceId'] == p['instanceID'] and instance['ImageId'] == p['amiID']
                and instance['InstanceType'] == p['instanceType'] and instance['State']['Name'] == 'running'
                and instance['Architecture'] == 'x86_64' and instance['Placement']['AvailabilityZone'] == p['availabilityZone'],
                'EC2 image, type, placement or lifetime changed')
        group = self.aws('eks', 'describe-nodegroup', '--cluster-name', p['clusterName'], '--nodegroup-name', p['nodegroup'])['nodegroup']
        require(group['clusterName'] == p['clusterName'] and group['nodegroupName'] == p['nodegroup']
                and group['status'] == 'ACTIVE' and group['amiType'] == 'AL2023_x86_64_STANDARD', 'managed AL2023 nodegroup required')
        groups = {row['name'] for row in group['resources']['autoScalingGroups']}
        tags = {row['Key']: row['Value'] for row in instance.get('Tags', [])}
        require(tags.get('aws:autoscaling:groupName') in groups, 'instance is outside the approved managed nodegroup')
        self.verify_node()

    def api_cluster(self):
        self.verify_kubeconfig_binding()
        cluster = read_api_cluster(self)
        require(cluster['server'] == self.provider['clusterEndpoint']
                and digest(base64.b64decode(cluster['certificate-authority-data'], validate=True)) == self.provider['clusterCASHA256'],
                'API endpoint differs from the verified EKS binding')
        return cluster

    def observer_network_scope(self):
        return 'eks'

    def observer_server(self):
        return self.provider['clusterEndpoint']

    def kernel_configuration(self):
        release = self.provider['nodeInfo']['kernelVersion']
        require(re.fullmatch(r'[A-Za-z0-9._+-]{1,256}', release) is not None,
                'invalid bound kernel release')
        # Read the installed config for the verified running kernel. A missing
        # package file remains an error, never inferred accounting support.
        return self.exec(['cat', '/boot/config-' + release]).decode()

    def environment_fields(self):
        return {'sharedKindKernel': False, 'provider': 'eks-managed-al2023-amd64',
                'providerBindingSHA256': digest(canonical(self.provider)),
                'privateKubeconfigSHA256': self.kubeconfig_sha256}

    def exec(self, args, data=None, timeout=10):
        return command(self.observer_command(args), data, timeout)

    def observer_command(self, args):
        self.verify_host()
        require(type(args) is list and args and all(isinstance(x, str) and '\0' not in x for x in args), 'invalid native command')
        return list(args)

    def observer_input_command(self, args):
        return self.observer_command(args)

    def policy(self):
        self.verify_kubeconfig_binding()
        self.verify_host()
        self.verify_node()
        super().policy()
