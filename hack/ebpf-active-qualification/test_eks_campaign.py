"""Provider routing and restoration tests; synthetic inputs are not EKS evidence."""
from copy import deepcopy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from campaign import PROFILES, canonical, digest
from eks_campaign import EKSActiveCampaign, main
from eks_case import EKSCase
from bound_case import BoundCase
from test_bound_case import configuration


class EKSActiveTests(unittest.TestCase):
    def test_initial_freeze_marks_provider_without_copying_private_binding(self):
        cfg = configuration()
        cfg.update(sourceSHA256=digest(canonical({})), chartInventory={})
        cfg['trace'] = {'image': 'tracer@sha256:' + 'a' * 64, 'policySHA256': 'b' * 64,
                        'providerExecution': {'accountID': '123456789012', 'clusterEndpoint': 'https://private.example'}}
        case = Mock()
        case.runtime.exec.return_value = ('f' * 64 + ' helper').encode()
        fields = {'sharedKindKernel': False, 'provider': 'eks-managed-al2023-amd64',
                  'providerBindingSHA256': digest(canonical(cfg['trace']['providerExecution'])),
                  'privateKubeconfigSHA256': 'c' * 64}
        case.runtime.environment_fields.return_value = fields
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'new'
            with patch('campaign.sources', return_value={}), patch('campaign.verify_inventory'), \
                 patch('campaign.kernel_accounting', return_value={}), patch('campaign.certificate_lifetimes', return_value=[]), \
                 patch('eks_campaign.EKSCase', return_value=case) as provider, patch('campaign.LocalCase') as local:
                EKSActiveCampaign(cfg, output, PROFILES['normal']())
            provider.assert_called_once_with(cfg)
            local.assert_not_called()
            raw = (output / 'freeze.json').read_text()
            freeze = json.loads(raw)
            self.assertEqual(freeze['executionEnvironment'], fields)
            self.assertIn('owned EKS host', freeze['scope'])
            self.assertNotIn('123456789012', raw)
            self.assertNotIn('private.example', raw)

    def test_provider_case_requires_selected_node_label_and_lifetime(self):
        cfg = configuration()
        cfg['trace'] = {'node': 'ip-10-0-0-1.ec2.internal', 'nodeUID': 'owned-node'}
        node = {'metadata': {'uid': 'owned-node', 'labels': {'kubernetes.io/hostname': cfg['trace']['node']}}}
        runtime = SimpleNamespace(cfg=cfg['trace'])
        with patch('eks_case.EKSHostRuntime', return_value=runtime) as constructor, patch.object(EKSCase, 'json', return_value=node):
            case = EKSCase(cfg)
        constructor.assert_called_once_with(cfg['trace'])
        self.assertIs(case.runtime, runtime)
        values = case.standard_values()
        self.assertEqual(values['collector'], {'nodeSelector': {'kubernetes.io/hostname': cfg['trace']['node']},
                                              'service': {'extensionPort': 8443, 'extensionPortName': 'extension'}})
        del values['collector']
        self.assertEqual(values, BoundCase.standard_values(case))
        for change in ('uid', 'hostname'):
            changed = deepcopy(node)
            if change == 'uid':
                changed['metadata']['uid'] = 'replacement'
            else:
                changed['metadata']['labels']['kubernetes.io/hostname'] = 'other'
            with patch('eks_case.EKSHostRuntime', return_value=runtime), patch.object(EKSCase, 'json', return_value=changed):
                with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'hostname label or lifetime'):
                    EKSCase(cfg)

    def test_wrong_host_failure_cannot_fall_back_to_kind(self):
        with patch('eks_case.EKSHostRuntime', side_effect=ValueError('wrong host')), patch('local_case.command') as command:
            with self.assertRaisesRegex(ValueError, 'wrong host'):
                EKSCase({'trace': {}})
        command.assert_not_called()

    def test_profile_change_and_restore_keep_provider_runtime_and_original_binding(self):
        run = EKSActiveCampaign.__new__(EKSActiveCampaign)
        run.cfg = {'trace': {'providerExecution': {'frozen': 'binding'}, 'deploymentSpecSHA256': {'api': 'original'}}}
        run.profile = PROFILES['normal']()
        run.original = {'metadata': {'uid': 'owned-api', 'resourceVersion': '17'},
                        'spec': {'replicas': 1, 'template': {'spec': {'containers': [{'args': ['serve']}]}}}}
        case = Mock()
        run.case = case
        runtime = case.runtime
        runtime.kube = ['kubectl', '--context', 'explicit-eks']
        runtime.cfg = {'namespace': 'owned'}
        runtime.services = {'api': 'owned-api', 'node': 'owned-node'}
        runtime.deployment.return_value = deepcopy(run.original)
        run.confirmed = None
        run.record = Mock()
        with tempfile.TemporaryDirectory() as directory:
            run.directory = Path(directory)
            with patch('campaign.command') as command, patch('eks_campaign.EKSCase', return_value=case) as provider, patch('campaign.LocalCase') as local:
                run.confirmed_profile()
                self.assertEqual(provider.call_args.args[0]['trace']['providerExecution'], {'frozen': 'binding'})
                runtime.get.return_value = deepcopy(run.confirmed)
                run.restore()
                self.assertEqual(provider.call_count, 2)
                provider.assert_called_with(run.cfg)
                local.assert_not_called()
                restoration = json.loads(command.call_args.args[1])
                self.assertEqual(restoration[0], {'op': 'test', 'path': '/metadata/uid', 'value': 'owned-api'})
                self.assertEqual(restoration[2]['value'], run.original['spec'])

    def test_cli_keeps_each_full_profile_and_clears_alarm_on_failure(self):
        for label, loader in PROFILES.items():
            profile = loader()
            self.assertEqual(profile['pairs'], 5)
            argv = ['eks_campaign.py', '--config', '/private/config', '--output', '/private/new',
                    '--case', label, '--acknowledge-owned-eks-host']
            with self.subTest(label=label), patch('sys.argv', argv), patch('eks_campaign.os.umask'), \
                 patch('eks_campaign.read_configuration', return_value={'private': 'binding'}) as read, \
                 patch('eks_campaign.signal.signal'), patch('eks_campaign.signal.alarm') as alarm, \
                 patch('eks_campaign.EKSActiveCampaign') as campaign:
                campaign.return_value.run.side_effect = RuntimeError('measurement failed')
                with self.assertRaisesRegex(RuntimeError, 'measurement failed'):
                    main()
                read.assert_called_once_with(Path('/private/config'))
                campaign.assert_called_once_with({'private': 'binding'}, Path('/private/new'), profile)
                expected = 5 * (2 * (profile['windowSeconds'] + profile['warmupSeconds']) + 600) + 600
                self.assertEqual([call.args[0] for call in alarm.call_args_list], [expected, 0])

    def test_cli_requires_explicit_provider_acknowledgement(self):
        argv = ['eks_campaign.py', '--config', '/private/config', '--output', '/private/new', '--case', 'normal']
        with patch('sys.argv', argv), patch('sys.stderr'), patch('eks_campaign.read_configuration') as read:
            with self.assertRaises(SystemExit):
                main()
        read.assert_not_called()


if __name__ == '__main__':
    unittest.main()
