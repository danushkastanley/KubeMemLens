#!/usr/bin/env python3
"""Evaluate fixed local budgets; never promote a fixture to provider support."""
import argparse
import math
from common import load, write_new
from evidence import validate


def percentile(values):
    return sorted(values)[math.ceil(len(values)*.95)-1] if values else None

def evaluate(p,e,source):
    validate(p,e); checks=[]; b=p['budgets']
    windows={phase:e['windows'][phase]['samples'] for phase in ('baseline','enabled')}
    base, active=windows['baseline'],windows['enabled']
    def check(name, passed, observed=None, limit=None):
        checks.append({'id':name,'passed':bool(passed),'observed':observed,'limit':limit})
    def maximum(key, value):
        check(key,value is not None and value<=b[key],value,b[key])
    def peak(rows,key):return max(r[key] for r in rows)
    def mean(rows,key):
        return sum(r[key]*(r['elapsedSeconds']-before['elapsedSeconds']) for before,r in zip(rows,rows[1:]))/(rows[-1]['elapsedSeconds']-rows[0]['elapsedSeconds'])
    def increase(key):return max(0,mean(active,key)-mean(base,key))
    def rate(rows,key):
        delta=rows[-1][key]-rows[0][key]
        return delta/(rows[-1]['elapsedSeconds']-rows[0]['elapsedSeconds']) if delta>=0 else None
    counters='sourceReads sourceErrors postSuccess postFailures usageCommits healthWrites apiReads apiWrites'.split()
    for phase,rows in windows.items():
        check(phase+'-stable',e['windows'][phase]['stable'])
        check(phase+'-monotonic-counters',all(r[k]>=prior[k] for prior,r in zip(rows,rows[1:]) for k in counters))
        check(phase+'-no-source-or-delivery-failures',all(rows[-1][k]==rows[0][k] for k in ('sourceErrors','postFailures')))
        check(phase+'-heap-measured',all(r['heapObjectsBytes']>0 for r in rows))
    check('enabled-volume-counters',all(r[k]>=prior[k] for prior,r in zip(active,active[1:]) for k in ('volumeReads','volumeErrors')))
    check('no-volume-parser-failures',active[-1]['volumeErrors']==active[0]['volumeErrors'])
    check('volume-acquisition-coverage',active[-1]['volumeReads']-active[0]['volumeReads']>=p['measurement']['enabledSeconds']/30)
    maximum('collectorHeapPeakBytes',peak(active,'heapObjectsBytes'))
    maximum('collectorHeapIncreaseBytes',max(0,peak(active,'heapObjectsBytes')-peak(base,'heapObjectsBytes')))
    maximum('collectorCPUIncreaseMilli',increase('collectorCPUMilli'))
    maximum('producerCPUMilli',mean(active,'producerCPUMilli'))
    maximum('producerMemoryBytes',peak(active,'producerMemoryBytes'))
    maximum('kubeletCPUIncreaseMilli',increase('kubeletCPUMilli'))
    maximum('kubeletMemoryIncreaseBytes',max(0,peak(active,'kubeletMemoryBytes')-peak(base,'kubeletMemoryBytes')))
    for counter,key,budget in (('sourceReads','sourceReadSeconds','sourceReadP95Seconds'),('postSuccess','postSeconds','deliveryP95Seconds')):
        values=[r[key] for prior,r in zip(active,active[1:]) if r[counter]>prior[counter]]
        check(counter+'-latency-coverage',len(values)>=(len(active)-1)/2 and all(v>0 for v in values))
        maximum(budget,percentile(values))
    maximum('podReadP95Seconds',percentile([r['podSeconds'] for r in active]))
    maximum('workloadReadP95Seconds',percentile([r['workloadSeconds'] for r in active]))
    check('operator-latency-measured',all(r[k]>0 for r in active for k in ('podSeconds','workloadSeconds')))
    maximum('apiReadRate',rate(active,'apiReads'))
    before_writes,after_writes=rate(base,'apiWrites'),rate(active,'apiWrites')
    maximum('apiWriteRateIncrease',max(0,after_writes-before_writes) if before_writes is not None and after_writes is not None else None)
    commits=rate(active,'usageCommits')
    maximum('usageCommitsPerMinute',commits*60 if commits is not None else None)
    writes=active[-1]['healthWrites']-active[0]['healthWrites']
    maximum('healthPayloadWrites',writes if writes>=0 else None)
    maximum('usageRetainedBytes',peak(active,'usageBytes'))
    maximum('healthRetainedBytes',peak(active,'healthBytes'))
    maximum('responseBytes',peak(active,'sourceResponseBytes'))
    check('retention-measured',all(r['usageBytes']>0 and r['healthBytes']>0 for r in active))
    for name,result in e['workflow'].items():check(name,result)
    check('build-binding',e['source']==source)
    for name,seconds in e['recovery'].items():
        check(name+'-recovery',seconds<=b['recoverySeconds'],seconds,b['recoverySeconds'])
    check('cleanup',e['cleanup']=='passed')
    return {'schemaVersion':1,'scope':'local-reference','profile':e['profile'],'recordDigest':e['recordDigest'],
            'outcome':'pass' if all(c['passed'] for c in checks) else 'fail','checks':checks,
            'reviewEligible':not e['source']['dirty'],'managedProviderQualified':False,'storageOperationLatencyMeasured':False}

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('profile','evidence','source','output'):parser.add_argument('--'+name,required=True)
    a=parser.parse_args(); result=evaluate(load(a.profile),load(a.evidence),load(a.source));write_new(a.output,result)
    raise SystemExit(0 if result['outcome']=='pass' else 1)
