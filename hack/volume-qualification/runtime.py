"""Observe only the parent-owned kind fixture; raw responses stay private."""
import json
import sys
import time
from pathlib import Path
from urllib.parse import urlsplit

from common import Invalid, require, number
# Reuse the existing read-only ownership, CRI and cgroup observer. Its error and
# number primitives have the same contract; no provider coordinator is imported.
sys.path.append(str(Path(__file__).resolve().parents[1]/'node-qualification'))
from kind_runtime import KindRuntime, metrics
from prometheus import api_counters

class VolumeRuntime(KindRuntime):
    def __init__(self,cluster,node,kubeconfig,phase):
        super().__init__(cluster,node,kubeconfig)
        require(phase in ('baseline','enabled'),'invalid volume phase')
        self.phase=phase
        self.identities=None
        self.pods=None
        self.cli=self.private/'kubectl-memlens'
        self.reader=self.private/'volume-reader-kubeconfig'

    def raw_api(self,path):
        require(path=='/metrics','unsupported observation endpoint')
        server=(self.private/'api-server').read_text().strip()
        url=urlsplit(server)
        require(url.scheme=='https' and url.hostname and not url.username and not url.password and not url.query and not url.fragment and url.path in ('','/'),'invalid owned API endpoint')
        return self.run(['curl','--fail','--silent','--show-error','--max-time','8','--max-filesize',str(16<<20),
                         '--cacert',str(self.private/'api-ca.crt'),'--cert',str(self.private/'api-client.crt'),
                         '--key',str(self.private/'api-client.key'),server.rstrip('/')+path])

    def current_workload(self):
        items=json.loads(self.k('get','pods','-n','kube-memlens-csi-e2e','-o','json'))['items']
        selected=[p for p in items if p['metadata']['name']=='persistent' or p['metadata'].get('labels',{}).get('app')=='volume-workload']
        require(len(selected)==3,'reference workload coverage differs')
        identities={p['metadata']['uid'] for p in selected}
        if self.pods is None:self.pods=identities
        require(identities==self.pods,'reference workload replaced during measurement')
        for p in selected:
            statuses=p.get('status',{}).get('containerStatuses',[])
            require(len(statuses)==1 and all(c.get('ready') and c.get('restartCount')==0 for c in statuses),'reference workload unstable')

    def query(self,args):
        started=time.monotonic()
        body=self.run([str(self.cli),'--kubeconfig',str(self.reader),*args,'-n','kube-memlens-csi-e2e','-o','json'])
        require(len(body.encode())<=512<<10,'operator result exceeds bound')
        return json.loads(body),time.monotonic()-started

    def observe(self,started):
        current=time.monotonic();enabled=self.phase=='enabled'
        processes=self.containers()
        require({'agent','collector','node-context'}==set(processes),'reference component coverage differs')
        identities={k:v['id'] for k,v in processes.items()}
        if self.identities is None:self.identities=identities
        require(self.identities==identities,'component replaced during measurement')
        self.current_workload()
        collector_cpu,_=self.resources('collector',processes['collector']['pid'],current)
        producer_cpu,producer_memory=self.resources('producer',processes['node-context']['pid'],current)
        kubelet_cpu,kubelet_memory=self.kubelet(current)
        producer=self.component_metrics(processes['node-context'],8083)
        collector=metrics(self.api('/metrics/current')['content'])
        read_count,write_count=api_counters(self.raw_api('/metrics'))
        p='kubememlens_node_context_';v='kubememlens_volume_'
        def get(values,key):
            require(key in values and number(values[key]),'required operational metric missing')
            return values[key]
        require(get(collector,v+'usage_enabled')==int(enabled) and get(collector,v+'health_enabled')==int(enabled),'configured collection mode differs')
        row={'elapsedSeconds':current-started,'heapObjectsBytes':get(collector,'kubememlens_collector_heap_objects_bytes'),
             'collectorCPUMilli':collector_cpu,'producerCPUMilli':producer_cpu,'producerMemoryBytes':producer_memory,'kubeletCPUMilli':kubelet_cpu,'kubeletMemoryBytes':kubelet_memory,
             'sourceReads':get(producer,p+'reads_total{result="success"}'),'sourceErrors':sum(value for key,value in producer.items() if key.startswith(p+'reads_total{') and key!=p+'reads_total{result="success"}'),
             'sourceReadSeconds':get(producer,p+'last_read_seconds'),'sourceResponseBytes':get(producer,p+'last_response_bytes'),
             'postSuccess':get(producer,p+'posts_total{result="success"}'),'postFailures':get(producer,p+'posts_total{result="failure"}'),'postSeconds':get(producer,p+'last_post_seconds'),
             'usageCommits':get(collector,v+'usage_commits_total'),'usageBytes':get(collector,v+'usage_retained_bytes'),'healthWrites':get(collector,v+'health_payload_writes_total'),'healthBytes':get(collector,v+'health_retained_bytes'),
             'apiReads':read_count,'apiWrites':write_count,'volumeReads':None,'volumeErrors':None,'podSeconds':None,'workloadSeconds':None,'filesystemAt':None,'usageEnabled':enabled,'healthEnabled':enabled}
        if enabled:
            pod,pod_seconds=self.query(['volumes','pod','persistent'])
            workload,workload_seconds=self.query(['volumes','workload','Deployment/volume-workload'])
            usage=next(r['usage'] for r in pod['context']['volumes'] if r['volumeName']=='data')
            require(usage['availability']=='reported' and usage['freshness']=='fresh','current filesystem usage unavailable')
            require(len(workload['evidence']['workload']['pods'])==2,'workload memory coverage incomplete')
            row.update(volumeReads=get(producer,v+'stats_reads_total'),volumeErrors=get(producer,v+'stats_errors_total'),podSeconds=pod_seconds,workloadSeconds=workload_seconds,filesystemAt=usage['filesystem']['capturedAt'])
        return row
