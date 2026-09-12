import copy
import hashlib
import subprocess
import sys
import tempfile
import unittest
import uuid
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from common import ContractError, load
from prepare_provider import REPOSITORY
from provider_bundle import Bundle
from provider_inputs import ACKNOWLEDGEMENT, build_helpers, prepare
from test_provider_plan import ProviderFixture


class ProviderInputsTest(ProviderFixture, unittest.TestCase):
    def setUp(self):
        temporary=tempfile.TemporaryDirectory();self.addCleanup(temporary.cleanup)
        self.work=Path(temporary.name)
        self.profile=load(REPOSITORY/'hack/node-qualification/profiles/gke-standard.json')
        configuration=self.config('gke-standard','gke-cos-containerd-amd64')
        for key,name in (('cliBinary','input-cli'),('chartArchive','input-chart.tgz')):
            target=self.work/name;target.write_bytes(Path(configuration[key]).read_bytes());configuration[key]=str(target)
        self.bundle=Bundle(self.work,self.profile,configuration,{'qualificationToolCommit':self.commit})
        self.args=SimpleNamespace(acknowledge=ACKNOWLEDGEMENT,replacement_acknowledge='provider-action-approved',
            profile=str(REPOSITORY/'hack/node-qualification/profiles/gke-standard.json'),proposal=str(self.work),
            plan_digest='sha256:'+'a'*64,replacement_slot=0,output_dir=str(self.work/'new-output'),architecture='amd64',
            candidate_bundle=str(self.work),candidate_tag='v1.0.0-rc.3',image_archive=str(self.work/'image.tar'))
        self.proof={'candidate':{'sourceCommit':self.commit},'image':{'architecture':'amd64'}}

    def prepare(self):
        with patch('provider_inputs.validate_bundle',return_value=self.bundle),patch('provider_inputs.verify',return_value=self.proof):
            return prepare(self.args)

    def test_frozen_consumer_bytes_are_private_and_original_proposal_is_unchanged(self):
        before=copy.deepcopy(self.bundle.configuration)
        frozen,proof,private,public=self.prepare()
        self.assertEqual(self.bundle.configuration,before)
        for key,name,mode in (('chartArchive','chart.tgz',0o400),('cliBinary','kubectl-memlens',0o500)):
            path=private/name
            self.assertEqual(path.read_bytes(),Path(before[key]).read_bytes())
            self.assertEqual(path.stat().st_mode & 0o777,mode)
            self.assertEqual(frozen.configuration[key],str(path))
        self.assertEqual(public.stat().st_mode & 0o777,0o700)
        self.assertFalse(load(public/'artefacts.json')['qualified'])

    def test_approval_is_checked_before_loading_or_verifying_inputs(self):
        self.args.acknowledge='not-approved'
        with patch('provider_inputs.validate_bundle') as validate,patch('provider_inputs.verify') as verify:
            with self.assertRaisesRegex(ContractError,'approval'):prepare(self.args)
        validate.assert_not_called();verify.assert_not_called()
        self.assertFalse(Path(self.args.output_dir).exists())

    def test_unsupported_architecture_or_unignored_output_fails_before_verification(self):
        for scenario in ('architecture','output'):
            self.setUp()
            if scenario=='architecture':self.args.architecture='arm64'
            else:self.args.output_dir=str(REPOSITORY/('provider-output-'+uuid.uuid4().hex))
            with patch('provider_inputs.validate_bundle',return_value=self.bundle),patch('provider_inputs.verify') as verify:
                with self.assertRaises(ContractError):prepare(self.args)
            verify.assert_not_called();self.assertFalse(Path(self.args.output_dir).exists())

    def test_input_changed_after_authority_check_is_never_made_executable(self):
        Path(self.bundle.configuration['cliBinary']).write_bytes(b'changed unsigned input')
        with self.assertRaisesRegex(ContractError,'changed'):self.prepare()
        path=Path(self.args.output_dir)/'private/kubectl-memlens'
        self.assertEqual(path.stat().st_mode & 0o111,0)

    def test_helper_build_cannot_inherit_overlay_or_cross_compile_settings(self):
        private=self.work/'helpers';private.mkdir()
        calls=[]
        def build(args,**kwargs):
            calls.append((args,kwargs));Path(args[args.index('-o')+1]).write_bytes(b'test helper output')
            return ''
        with patch('provider_inputs.host_platform',return_value='linux_amd64'):
            result=build_helpers(private,build)
        self.assertEqual(set(result),{'api-bridge','chart-inventory'})
        for args,options in calls:
            env=options['environment']
            self.assertEqual((env['GOFLAGS'],env['GOWORK'],env['CGO_ENABLED']),('','off','0'))
            self.assertEqual((env['GOOS'],env['GOARCH']),('linux','amd64'))
            self.assertEqual(args[:3],['go','-C',str(REPOSITORY)])
            self.assertTrue(args[-1].startswith('./hack/node-qualification/'))
        self.assertEqual(load(private/'helper-digests.json'),result)

    def test_real_entrypoint_refuses_unapproved_run_without_executing_inputs(self):
        self.args.acknowledge='not-approved'
        command=[sys.executable,str(REPOSITORY/'hack/node-qualification/run_provider.py')]
        for key,value in vars(self.args).items():command += ['--'+key.replace('_','-'),str(value)]
        result=subprocess.run(command,capture_output=True,text=True)
        self.assertEqual(result.returncode,2)
        self.assertIn('approval',result.stderr)
        self.assertFalse(Path(self.args.output_dir).exists())
        self.assertFalse((self.root/'credential-executed').exists())


if __name__=='__main__':unittest.main()
