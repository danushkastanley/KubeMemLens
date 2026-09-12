import copy
import json
import tempfile
import unittest
from contextlib import ExitStack
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

from common import ContractError, load
from run_provider import run
import test_provider_record


class ProviderCommandTest(unittest.TestCase):
    def setUp(self):
        self.fixture=test_provider_record.ProviderRecordTest(); self.fixture.setUp()
        temporary=tempfile.TemporaryDirectory(); self.addCleanup(temporary.cleanup)
        self.root=Path(temporary.name); self.private=self.root/'private'; self.public=self.root/'evidence'
        self.private.mkdir();self.public.mkdir()
        self.e=self.fixture.execution
        self.e.bundle.configuration.update(kubeconfigPath="/private/kubeconfig", context="fixture", namespace="owned-namespace")
        self.replacement=copy.deepcopy(self.e.replacement);self.e.replacement=None
        self.events=[]
        self.e.prepare=Mock(side_effect=lambda:self.events.append('prepare'))
        self.e.enable=Mock(side_effect=lambda:self.events.append('enable'))
        def measure(phase):
            self.events.append(phase)
            return {'phase':phase,'profile':self.fixture.example['profile'],'nodes':[
                {'slot':n['slot'],'samples':n['samples'][phase],'rotation':n['rotation']} for n in self.fixture.example['nodes']]}
        self.e.measure=Mock(side_effect=measure)
        self.e.cleanup=Mock(side_effect=lambda:self.events.append('cleanup'))
        self.e.private=self.private
        self.args=SimpleNamespace(proposal='/private/proposal',plan_digest='sha256:'+'a'*64,replacement_slot=0,
                                  replacement_acknowledge='provider-action-approved')
        self.recovery=SimpleNamespace(source_loss=lambda:copy.deepcopy(self.fixture.example['lifecycle']['sourceLoss']),
            restart=lambda component:copy.deepcopy(self.fixture.example['lifecycle'][component+'Restart']))
        def replacement(*args):
            self.events.append('replacement');self.e.replacement=copy.deepcopy(self.replacement)
            return copy.deepcopy(self.fixture.example['lifecycle']['providerNodeReplacement'])
        self.replacer=SimpleNamespace(run=Mock(side_effect=replacement))
        self.network=SimpleNamespace(run=Mock(return_value=self.fixture.network))
        self.helpers=Mock()

    def run_command(self):
        with ExitStack() as stack:
            patches={
                'prepare':Mock(return_value=(self.e.bundle,self.fixture.proof,self.private,self.public)),
                'build_helpers':self.helpers,'validate_bundle':Mock(),
                'collect_inventory':Mock(return_value=(self.fixture.receipt,self.e.binding)),
                'Execution':Mock(return_value=self.e),'PoolRecovery':Mock(return_value=self.recovery),
                'ProviderReplacement':Mock(return_value=self.replacer),'NetworkChecks':Mock(return_value=self.network),
                'verify_cli':Mock(return_value={'doctor':True,'nodeExplain':2,'nodeHistory':2,'rawResponsesRetained':False}),
                'verify_images':Mock(return_value=self.replacement['liveImages']),
                'utc_text':Mock(return_value=self.fixture.example['startedAt'])}
            for name,value in patches.items():stack.enter_context(patch('run_provider.'+name,value))
            stack.enter_context(patch('provider_record.utc_text',return_value=self.fixture.example['completedAt']))
            return run(self.args)

    def test_success_keeps_cleanup_and_review_pending_after_real_record_assembly(self):
        result=self.run_command()
        self.assertEqual(result['state'],'awaiting-independent-provider-cleanup')
        self.assertFalse(result['qualified'])
        self.assertEqual(self.events,['prepare','baseline','enable','enabled','replacement','cleanup'])
        self.e.cleanup.assert_called_once_with()
        original=load(self.public/'qualification-observations.json')
        cleaned=load(self.public/'kubernetes-cleaned-observations.json')
        self.assertFalse(original['cleanup']['workloadsRemoved'])
        self.assertTrue(cleaned['cleanup']['workloadsRemoved'])
        self.assertEqual(cleaned['cleanup']['cloudResources'],'pending')
        evaluation=load(self.public/'qualification-evaluation.pending.json')
        self.assertEqual([c['id'] for c in evaluation['checks'] if not c['passed']],['cleanup'])
        for path in self.public.glob('*.json'):
            self.assertNotIn('private-',path.read_text())
            self.assertEqual(path.stat().st_mode & 0o777,0o600)

    def test_failure_retains_completed_window_and_cleans_up_without_raw_error_text(self):
        self.e.enable.side_effect=ContractError('private-credential-context')
        with self.assertRaises(ContractError) as caught:self.run_command()
        self.e.cleanup.assert_called_once_with()
        self.assertTrue((self.public/'baseline-measurements.json').exists())
        self.assertFalse((self.public/'enabled-measurements.json').exists())
        failure=load(self.public/'failure.json')
        self.assertEqual(failure['stage'],'enabled');self.assertEqual(failure['ownedCleanup'],'passed')
        self.assertNotIn('private-credential',str(caught.exception)+json.dumps(failure))
        self.replacer.run.assert_not_called()

    def test_api_failure_category_is_retained_without_unknown_exception_text(self):
        self.e.enable.side_effect=ContractError('qualification API read failed: rate-limited')
        with self.assertRaises(ContractError):self.run_command()
        self.assertEqual(load(self.public/'failure.json')['reason'],'api-rate-limited')
        self.setUp()
        self.e.enable.side_effect=ContractError('private-credential-context')
        with self.assertRaises(ContractError):self.run_command()
        failure=load(self.public/'failure.json')
        self.assertEqual(failure['reason'],'unclassified')
        self.assertNotIn('private-credential',json.dumps(failure))

    def test_bad_measurements_stop_before_operator_replacement(self):
        self.fixture.example['nodes'][1]['samples']['enabled'][3]['producerCPUMilli']=100000
        with self.assertRaises(ContractError):self.run_command()
        self.replacer.run.assert_not_called();self.e.cleanup.assert_called_once_with()
        self.assertEqual(load(self.public/'failure.json')['stage'],'measurement-validation')

    def test_cleanup_failure_cannot_publish_a_cleaned_record(self):
        self.e.cleanup.side_effect=ContractError('private-resource')
        with self.assertRaises(ContractError):self.run_command()
        failure=load(self.public/'failure.json')
        self.assertEqual(failure['stage'],'cleanup');self.assertEqual(failure['ownedCleanup'],'failed')
        self.assertTrue((self.public/'qualification-observations.json').exists())
        self.assertFalse((self.public/'kubernetes-cleaned-observations.json').exists())

    def test_interruption_runs_cleanup_and_remains_interrupted(self):
        self.e.measure.side_effect=KeyboardInterrupt
        with self.assertRaises(KeyboardInterrupt):self.run_command()
        self.e.cleanup.assert_called_once_with()
        self.assertEqual(load(self.public/'failure.json')['failureType'],'KeyboardInterrupt')

    def test_helper_failure_does_not_start_target_preparation(self):
        self.helpers.side_effect=ContractError('build failed')
        with self.assertRaises(ContractError):self.run_command()
        self.e.prepare.assert_not_called();self.e.cleanup.assert_not_called()
        self.assertEqual(load(self.public/'failure.json')['ownedCleanup'],'not-started')


if __name__=='__main__':unittest.main()
