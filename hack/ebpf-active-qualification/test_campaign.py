from pathlib import Path
from copy import deepcopy
import json
import tempfile
import unittest
from unittest.mock import Mock, patch

from campaign import Campaign


class CampaignCleanupTests(unittest.TestCase):
    def profile_case(self):
        run = Campaign.__new__(Campaign)
        run.cfg = {'trace': {'deploymentSpecSHA256': {'api': 'original'},
                            'deploymentNames': {'api': 'reviewed-trace-api', 'node': 'reviewed-trace-node-local'}}}
        run.original = {'metadata': {'uid': 'owned-api', 'resourceVersion': '17'},
                        'spec': {'replicas': 1, 'template': {'spec': {'containers': [{'args': ['serve']}]}}}}
        run.case = Mock()
        runtime = run.case.runtime
        runtime.services = run.cfg['trace']['deploymentNames']
        runtime.cfg = {'namespace': 'owned'}
        runtime.kube = ['kubectl', '--context', 'kind-test']
        runtime.deployment.return_value = deepcopy(run.original)
        run.record = Mock()
        run.confirmed = None
        return run, runtime

    def test_chart_profile_transition_and_restoration_target_same_owned_api(self):
        run, runtime = self.profile_case()
        case = run.case
        with tempfile.TemporaryDirectory() as directory:
            run.directory = Path(directory)
            with patch('campaign.command') as command, patch('campaign.LocalCase', return_value=case) as local_case:
                run.confirmed_profile()
                transition = json.loads(command.call_args.args[1])
                self.assertEqual(transition[0], {'op': 'test', 'path': '/metadata/uid', 'value': 'owned-api'})
                self.assertEqual(transition[1]['value'], '17')
                self.assertEqual(transition[2]['value']['template']['spec']['containers'][0]['args'],
                                 ['serve', '--allow-confirmed-paths'])
                self.assertEqual(local_case.call_args.args[0]['trace']['deploymentNames'], runtime.services)
                runtime.get.return_value = deepcopy(run.confirmed)
                runtime.get.return_value['metadata']['resourceVersion'] = '18'
                run.restore()
                runtime.get.assert_called_once_with('deployment', 'reviewed-trace-api')
                self.assertEqual(command.call_count, 2)
                for call in command.call_args_list:
                    args = call.args[0]
                    self.assertEqual(args[args.index('deployment') + 1], 'reviewed-trace-api')
                restoration = json.loads(command.call_args.args[1])
                self.assertEqual(restoration[1]['value'], '18')
                self.assertEqual(restoration[2]['value'], run.original['spec'])
                local_case.assert_called_with(run.cfg)

    def test_chart_restoration_refuses_replacement_or_concurrent_spec_change(self):
        for change in ('uid', 'spec'):
            with self.subTest(change=change):
                run, runtime = self.profile_case()
                run.confirmed = deepcopy(run.original)
                current = deepcopy(run.original)
                if change == 'uid':
                    current['metadata']['uid'] = 'replacement-api'
                else:
                    current['spec']['template']['spec']['containers'][0]['args'].append('--foreign-change')
                runtime.get.return_value = current
                with patch('campaign.command') as command, patch('campaign.LocalCase') as local_case:
                    with self.assertRaisesRegex(ValueError, 'concurrent profile change'):
                        run.restore()
                    command.assert_not_called()
                    local_case.assert_not_called()

    def test_api_restoration_precedes_namespace_cleanup_after_prepare_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Campaign.__new__(Campaign)
            run.directory = Path(directory)
            run.profile = {'pairs': 1}
            run.case = Mock()
            run.pairs = []
            run.fixtures = None
            run.record = Mock()
            run.progress = Mock()
            run.invariant = Mock()
            run.confirmed_profile = Mock()
            events = []
            run.restore = Mock(side_effect=lambda: events.append('restore-api'))
            fixtures = Mock()
            fixtures.prepare.side_effect = RuntimeError('setup failed')
            fixtures.cleanup.side_effect = lambda: events.append('cleanup-namespaces')
            with patch('campaign.Fixtures', return_value=fixtures):
                with self.assertRaisesRegex(RuntimeError, 'setup failed'):
                    run.run()
            self.assertEqual(events, ['restore-api', 'cleanup-namespaces'])


if __name__ == '__main__':
    unittest.main()
