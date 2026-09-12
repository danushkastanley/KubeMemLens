"""Select bounded counters; unrelated or missing metrics never become evidence."""
import json
import re
from common import Invalid, require, number

LABEL = re.compile(r'([a-zA-Z_][a-zA-Z_0-9]*)=("(?:\\.|[^"\\])*")')
RELATED = {'pods','persistentvolumeclaims','persistentvolumes','csinodes','deployments','replicasets','statefulsets','daemonsets','jobs','cronjobs','replicationcontrollers','nodes'}
PERSISTED = {'pods','persistentvolumeclaims','persistentvolumes','csinodes'}
AUTH = {'subjectaccessreviews','selfsubjectaccessreviews','tokenreviews'}
WRITES = {'POST','PUT','PATCH','DELETE','DELETECOLLECTION','CREATE','UPDATE'}

def api_counters(text):
    require(len(text.encode()) <= 16<<20, 'API metrics byte limit exceeded')
    require('# TYPE apiserver_request_total counter' in text, 'API request counter family missing')
    reads=writes=rows=0
    for line in text.splitlines():
        if not line.startswith('apiserver_request_total{'):
            continue
        match=re.fullmatch(r'apiserver_request_total\{(.*)\}\s+(\S+)',line)
        require(match is not None, 'invalid API request counter')
        labels={};position=0
        for item in LABEL.finditer(match[1]):
            require(item.start()==position and item[1] not in labels, 'ambiguous API counter labels')
            labels[item[1]]=json.loads(item[2]);position=item.end()+1
        require(position==len(match[1])+1, 'invalid API counter labels')
        try:value=float(match[2])
        except ValueError as error:raise Invalid('invalid API counter number') from error
        require(number(value) and value.is_integer(), 'invalid API counter number')
        resource,verb,group=labels.get('resource'),labels.get('verb'),labels.get('group')
        require(resource is not None and verb is not None and group is not None, 'API counter dimensions missing')
        rows+=1
        if (resource in RELATED and group in ('','apps','batch','storage.k8s.io') and verb in ('GET','LIST','WATCH')) or (resource in AUTH and group in ('authorization.k8s.io','authentication.k8s.io')):
            reads+=int(value)
        if resource in PERSISTED and group in ('','storage.k8s.io') and verb in WRITES:
            writes+=int(value)
    require(rows>0,'API request counters unreported')
    return reads,writes
