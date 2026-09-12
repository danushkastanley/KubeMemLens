import copy
import json
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

from common import Invalid, digest, load, privacy
from evidence import NUMBERS, OPTIONAL, WORKFLOW, validate
from evaluate import evaluate
from profile import validate as validate_profile

PROFILE=Path(__file__).parent/'profiles/kind-csi-137.json'

def stamp(value):return value.isoformat().replace('+00:00','Z')

def fixture():
    p=load(PROFILE); start=datetime(2026,9,13,tzinfo=timezone.utc)
    source={'commit':'a'*40,'tree':'sha256:'+'b'*64,'dirty':False,**{k:'sha256:'+'c'*64 for k in ('producer','collector','cli','chart','image','driverImage','driverBinary')}}
    e={'schemaVersion':1,'scope':'local-reference','profile':{'id':p['id'],'digest':p['profileDigest']},'source':source,
       'environment':{**{k:p[k] for k in ('kubernetes','nodeImage','driverSource','registrarImage')},'kernel':'6.12.0-linuxkit','runtime':'containerd-2.2.0','architecture':'arm64','registrarRuntimeImage':'sha256:'+'d'*64,'externalHealthMonitor':'not-installed','controllerEvidence':'api-fixture','driverCapabilities':['GET_VOLUME_STATS']},
       'workflow':dict.fromkeys(WORKFLOW,True),'windows':{},'recovery':dict.fromkeys(('collector','producer','source','pod'),20),'cleanup':'passed'}
    for phase in ('baseline','enabled'):
        seconds=p['measurement'][phase+'Seconds']; rows=[]; enabled=phase=='enabled'
        for elapsed in range(0,seconds+1,15):
            r=dict.fromkeys(NUMBERS,0)
            r.update(elapsedSeconds=elapsed,heapObjectsBytes=20<<20,collectorCPUMilli=10,producerCPUMilli=1,producerMemoryBytes=20<<20,kubeletCPUMilli=20,kubeletMemoryBytes=40<<20,
                     sourceReads=1+elapsed//15,sourceReadSeconds=.02,sourceResponseBytes=10000,postSuccess=1+elapsed//15,postSeconds=.01,
                     usageCommits=1+elapsed//15 if enabled else 0,usageBytes=4000 if enabled else 0,healthWrites=4 if enabled else 0,healthBytes=5000 if enabled else 0,apiReads=elapsed,apiWrites=0,
                     volumeReads=1+elapsed//15 if enabled else None,volumeErrors=0 if enabled else None,podSeconds=.1 if enabled else None,workloadSeconds=.2 if enabled else None,
                     filesystemAt=stamp(start+timedelta(seconds=elapsed)) if enabled else None,usageEnabled=enabled,healthEnabled=enabled)
            rows.append(r)
        e['windows'][phase]={'startedAt':stamp(start),'completedAt':stamp(start+timedelta(seconds=seconds)),'stable':True,'samples':rows}
        start+=timedelta(seconds=seconds+60)
    seal(e);return p,e,copy.deepcopy(source)

def seal(e):e['recordDigest']=digest({k:v for k,v in e.items() if k!='recordDigest'})

class ContractTests(unittest.TestCase):
    def test_complete_reference_remains_unqualified_for_providers(self):
        p,e,s=fixture();result=evaluate(p,e,s)
        self.assertEqual(result['outcome'],'pass');self.assertTrue(result['reviewEligible'])
        self.assertFalse(result['managedProviderQualified']);self.assertFalse(result['storageOperationLatencyMeasured'])
    def test_wrong_or_incomplete_evidence_rejected(self):
        mutations=[lambda e:e['source'].pop('cli'),lambda e:e['environment'].update(controllerEvidence='real-driver'),
                   lambda e:e['environment'].update(driverCapabilities=[]),lambda e:e['environment'].update(nodeImage='unversioned'),
                   lambda e:e['windows']['enabled']['samples'][2].update(heapObjectsBytes=None),
                   lambda e:e['windows']['enabled']['samples'][2].update(filesystemAt=None),
                   lambda e:e['windows']['enabled']['samples'].pop(),lambda e:e.update(schemaVersion=2),lambda e:e.update(schemaVersion=True),
                   lambda e:e['profile'].update(digest='sha256:'+'0'*64),lambda e:e['recovery'].update(source=0)]
        for change in mutations:
            with self.subTest(change=change):
                p,e,s=fixture();change(e);seal(e)
                with self.assertRaises(Invalid):evaluate(p,e,s)
    def test_failures_and_wrong_artifacts_do_not_pass(self):
        mutations=[lambda e:e.update(cleanup='failed'),lambda e:e['source'].update(cli='sha256:'+'0'*64),
                   lambda e:e['workflow'].update(namespaceIsolation=False),lambda e:e['workflow'].update(inodePressure=False),
                   lambda e:e['windows']['enabled'].update(stable=False),lambda e:e['recovery'].update(source=121),
                   lambda e:e['windows']['enabled']['samples'][-1].update(healthWrites=5),
                   lambda e:e['windows']['enabled']['samples'][-1].update(sourceReads=0),
                   lambda e:e['windows']['enabled']['samples'][-1].update(apiReads=0),
                   lambda e:e['windows']['enabled']['samples'][-1].update(volumeErrors=1),
                   lambda e:e['windows']['enabled']['samples'][2].update(heapObjectsBytes=300<<20)]
        for change in mutations:
            p,e,s=fixture();change(e);seal(e);self.assertEqual(evaluate(p,e,s)['outcome'],'fail')
    def test_dirty_source_not_eligible_for_review(self):
        p,e,s=fixture();e['source']['dirty']=s['dirty']=True;seal(e)
        self.assertFalse(evaluate(p,e,s)['reviewEligible'])
    def test_privacy_and_ambiguous_json(self):
        for marker in ('vol-0123456789abcdef0','arn:aws:ec2:region:123456789012:volume/id','Bearer abcdefghijklmnop'):
            p,e,s=fixture();e['environment']['kernel']=marker;seal(e)
            with self.assertRaises(Invalid):evaluate(p,e,s)
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'bad.json'
            for body in ('{"x":1,"x":2}','{"x":NaN}','{"token":"private"}'):
                path.write_text(body)
                with self.assertRaises(Invalid):load(path)
    def test_frozen_digest_and_bounded_input(self):
        p,e,s=fixture();p['budgets']['podReadP95Seconds']=100
        with self.assertRaises(Invalid):validate_profile(p)
        e['environment']['driverCapabilities']=['GET_VOLUME_STATS']*129;seal(e)
        with self.assertRaises(Invalid):evaluate(load(PROFILE),e,s)

if __name__=='__main__':unittest.main()
