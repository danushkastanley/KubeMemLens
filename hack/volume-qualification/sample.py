#!/usr/bin/env python3
"""Sample complete fixed windows; cancellation or missing observations fail."""
import argparse
import time
from common import Invalid, load, now, require, write_new
from evidence import validate_window
from profile import validate
from runtime import VolumeRuntime


def measure(runtime,p,phase,clock=time.monotonic,sleep=time.sleep):
    validate(p)
    m=p['measurement'];sleep(m['settleSeconds'])
    runtime.observe(clock())  # Warm the health cache and prime CPU counters.
    started=clock();started_at=now();rows=[]
    for index in range(m[phase+'Seconds']//m['intervalSeconds']+1):
        sleep(max(0,started+index*m['intervalSeconds']-clock()))
        # A sample's label is printed only after it passes, so name the one that failed.
        try:rows.append(runtime.observe(started))
        except Invalid as error:raise Invalid(f'{phase} volume observation {index+1} failed: {error}') from error
        print(phase+' volume observation '+str(index+1),flush=True)
    result={'startedAt':started_at,'completedAt':now(),'stable':True,'samples':rows}
    validate_window(p,result,phase)
    return result

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('profile','cluster','node','kubeconfig','phase','output'):parser.add_argument('--'+name,required=True)
    a=parser.parse_args();p=validate(load(a.profile))
    require(a.phase in ('baseline','enabled'),'invalid phase')
    r=VolumeRuntime(a.cluster,a.node,a.kubeconfig,a.phase)
    write_new(a.output,measure(r,p,a.phase))
