"""Owned active fixtures bound to the explicitly verified EKS node host."""
from bound_case import BoundCase
from eks_host_runtime import EKSHostRuntime, require


class EKSCase(BoundCase):
    def __init__(self, configuration):
        runtime = EKSHostRuntime(configuration['trace'])
        super().__init__(configuration, runtime)
        node = self.json(['get', 'node', self.node, '-o', 'json'])
        require(node['metadata']['uid'] == runtime.cfg['nodeUID']
                and node['metadata'].get('labels', {}).get('kubernetes.io/hostname') == self.node,
                'selected Node hostname label or lifetime changed')

    def standard_values(self):
        values = super().standard_values()
        values['collector'] = {'nodeSelector': {'kubernetes.io/hostname': self.node},
                               'service': {'extensionPort': 8443, 'extensionPortName': 'extension'}}
        return values
