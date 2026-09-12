#!/usr/bin/env python3
"""Derive a sanitised record, then finalise only after owned-resource cleanup."""
import argparse
import json
from pathlib import Path
from common import require, load, write_new, digest
from evidence import WORKFLOW, validate
from evaluate import evaluate
from profile import validate as validate_profile


def private_json(root,name):
    with (root/name).open('rb') as source:body=source.read((2<<20)+1)
    require(len(body)<=2<<20,'private receipt exceeds bound')
    return json.loads(body)

def prepare(p,work,out):
    cockpit=private_json(work,'volume-cockpit-result.json')
    pressure=private_json(work,'volume-pressure-result.json')
    health=private_json(work,'volume-health-result.json')
    volumes=private_json(work,'volume-result.json')
    doctor=private_json(work,'volume-doctor.json')
    workflow={key:cockpit[key] for key in WORKFLOW if key in cockpit}
    workflow.update(doctor=any(c['name']=='connection' and c['status']=='pass' for c in doctor['checks']) and all(c['status'] in ('pass','warn') for c in doctor['checks']),
                    filesystemPressure=pressure['filesystemPressure'],inodePressure=pressure['inodePressure'],
                    healthConflict=health['conflictingSourcesVerified'],sourceRecovery=volumes['producerReplacement'] and volumes['collectorRestart'],
                    namespaceIsolation=volumes['crossNamespaceDenied'] and volumes['nodeViewerDenied'],rollback=volumes['profileRollback'] and volumes['volumeDisabledNodeMemoryFresh'])
    require(set(workflow)==set(WORKFLOW),'workflow receipt coverage incomplete')
    require(health['profile']=='alpha' and health['outcome']==volumes['outcome']=='passed','required workflow failed')
    record={'schemaVersion':1,'scope':'local-reference','profile':{'id':p['id'],'digest':p['profileDigest']},
            'source':load(out/'volume-source.json'),'environment':load(work/'volume-environment.json'),
            'workflow':workflow,'windows':{phase:load(out/('volume-window-'+phase+'.json')) for phase in ('baseline','enabled')},
            'recovery':volumes['recovery'],'cleanup':'pending'}
    record['recordDigest']=digest(record);validate(p,record)
    write_new(out/'volume-draft.json',record)

def finalise(p,out):
    e=load(out/'volume-draft.json');summary=load(out/'summary.json')
    require(summary['cleanup']=='passed' and summary['sourceCommit']==e['source']['commit'] and summary['sourceTreeSHA256']==e['source']['tree'].removeprefix('sha256:'),'cleanup/source receipt mismatch')
    e['cleanup']='passed';e['recordDigest']=digest({k:v for k,v in e.items() if k!='recordDigest'})
    validate(p,e);write_new(out/'volume-qualification.json',e)
    result=evaluate(p,e,load(out/'volume-source.json'));write_new(out/'volume-evaluation.json',result)
    print('Volume qualification: '+result['outcome'])
    return result['outcome']=='pass'

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile',required=True);parser.add_argument('--output-dir',required=True)
    parser.add_argument('--work-dir');parser.add_argument('--finalise',action='store_true')
    a=parser.parse_args();p=validate_profile(load(a.profile));out=Path(a.output_dir)
    if a.finalise:raise SystemExit(0 if finalise(p,out) else 1)
    require(a.work_dir is not None,'private workflow receipts required');prepare(p,Path(a.work_dir),out)
