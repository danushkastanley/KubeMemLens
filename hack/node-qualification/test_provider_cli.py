import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

from common import ContractError
from provider_cli import verify


class ProductionCLITest(unittest.TestCase):
    def setUp(self):
        root = tempfile.TemporaryDirectory(); self.addCleanup(root.cleanup)
        binary = Path(root.name)/'cli'; binary.write_bytes(b'verified fixture bytes')
        self.nodes = [{'name':'private-a','uid':'uid-a'},{'name':'private-b','uid':'uid-b'}]
        self.config = {'cliBinary':str(binary),'cliDigest':'sha256:'+hashlib.sha256(binary.read_bytes()).hexdigest(),
                       'kubeconfigPath':'/private/config','context':'approved-context','namespace':'qualification'}
        self.execution = SimpleNamespace(bundle=SimpleNamespace(configuration=self.config,profile={'workload':{'containers':32}}),
            binding={'nodes':self.nodes},verify_binding=Mock())
        self.calls=[]
        self.doctor={'checks':[{'status':'pass'}], 'nodes':[{'nodeName':n['name'],'stale':False} for n in self.nodes],
                     'mapping':{'containers':32,'mapped':32,'unmapped':0}}
        self.stale=False

    def read(self, args, **kwargs):
        self.calls.append(args)
        if 'doctor' in args:
            return json.dumps(self.doctor)
        name=args[args.index('node')+1]; node=next(n for n in self.nodes if n['name']==name)
        if 'explain' in args:
            return json.dumps({'record':{'nodeName':name,'nodeUID':node['uid'], 'freshness':'stale' if self.stale else 'fresh',
                                         'lastGood':{'nodeUID':node['uid']}}})
        return json.dumps({'nodeName':name,'generation':'private-generation','series':[{'nodeUID':node['uid'],'points':[{}]}]})

    def test_production_paths_use_explicit_api_target_and_discard_private_payloads(self):
        result=verify(self.execution,self.read)
        self.assertEqual(result,{'doctor':True,'nodeExplain':2,'nodeHistory':2,'rawResponsesRetained':False})
        self.assertNotIn('private',json.dumps(result))
        self.assertEqual(len(self.calls),5)
        for args in self.calls:
            self.assertEqual(args[args.index('--kubeconfig')+1],'/private/config')
            self.assertEqual(args[args.index('--context')+1],'approved-context')
            self.assertEqual(args[args.index('--connect-mode')+1],'kubernetes-api')
        self.assertEqual(self.execution.verify_binding.call_count,2)

    def test_changed_binary_is_rejected_before_execution(self):
        Path(self.config['cliBinary']).write_bytes(b'changed')
        with self.assertRaises(ContractError):verify(self.execution,self.read)
        self.assertFalse(self.calls)

    def test_warning_mapping_loss_or_stale_node_evidence_fails(self):
        for scenario in ('warning','mapping','stale'):
            self.setUp()
            if scenario=='warning':self.doctor['checks'][0]['status']='warn'
            elif scenario=='mapping':self.doctor['mapping']['mapped']=31
            else:self.stale=True
            with self.subTest(scenario=scenario),self.assertRaises(ContractError):verify(self.execution,self.read)


if __name__=='__main__':unittest.main()
