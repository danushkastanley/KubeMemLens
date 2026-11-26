import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from common import Invalid, load
from runtime import VolumeRuntime
from sample import measure

CLUSTER='kube-memlens-node-context-diagnostics'
NODE='diagnostics-control-plane'
P='kubememlens_node_context_';V='kubememlens_volume_'
IDS={'agent':'1'*64,'collector':'2'*64,'node-context':'3'*64}
MOUNT={'destination':'/var/run/kubelet-token','source':'/var/lib/kubelet/pods/p/volumes/kubernetes.io~projected/t'}
POD=lambda name,uid,labels={}:{'metadata':{'name':name,'uid':uid,'labels':labels},'status':{'containerStatuses':[{'ready':True,'restartCount':0}]}}
COLLECTOR={V+'usage_enabled':1,V+'health_enabled':1,'kubememlens_collector_heap_objects_bytes':1,V+'usage_commits_total':1,
           V+'usage_retained_bytes':1,V+'health_payload_writes_total':1,V+'health_retained_bytes':1}
PRODUCER={P+'reads_total{result="success"}':1,P+'last_read_seconds':1,P+'last_response_bytes':1,P+'posts_total{result="success"}':1,
          P+'posts_total{result="failure"}':0,P+'last_post_seconds':1,V+'stats_reads_total':1,V+'stats_errors_total':0}
text=lambda values:''.join(f'{k} {v}\n' for k,v in values.items())
# Ordered substring routes for the cluster-facing tools; the first match answers.
ROUTES={'docker inspect':CLUSTER,
        'crictl ps':json.dumps({'containers':[{'id':i,'labels':{'io.kubernetes.pod.namespace':'kube-memlens-node-context','io.kubernetes.container.name':n}} for n,i in IDS.items()]}),
        **{'crictl inspect -o json '+i:json.dumps({'info':{'pid':n+10,'runtimeSpec':{'mounts':[MOUNT]}}}) for n,i in enumerate(IDS.values())},
        'cpu.stat':'usage_usec 1000\n','memory.current':'4096\n','cat /proc/':'0::/fixture\n','systemctl':'7\n','nsenter':text(PRODUCER),
        'kubectl --kubeconfig':json.dumps({'items':[POD('persistent','a'),POD('w1','b',{'app':'volume-workload'}),POD('w2','c',{'app':'volume-workload'})]}),
        '/metrics/current':json.dumps({'content':text(COLLECTOR)}),
        '/metrics':'# TYPE apiserver_request_total counter\napiserver_request_total{group="",resource="pods",verb="GET"} 1\n'}
TOOL='''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
argv=' '.join([Path(sys.argv[0]).name,*sys.argv[1:]])
routes=json.loads(Path(os.environ['KML_FAKE_ROUTES']).read_text())
sys.stdout.write(next(v for k,v in routes.items() if k in argv))
'''

def sample(freshness):
    usage={'availability':'reported','freshness':freshness,'filesystem':{'capturedAt':'2026-10-18T00:00:00Z'}}
    return {'context':{'volumes':[{'volumeName':'data','usage':usage}]},'evidence':{'workload':{'pods':[{},{}]}}}

class SampleDiagnosticTests(unittest.TestCase):
    def setUp(self):
        directory=tempfile.TemporaryDirectory();self.addCleanup(directory.cleanup)
        self.root=root=Path(directory.name);tools=root/'bin';tools.mkdir()
        (root/'routes.json').write_text(json.dumps(ROUTES));(root/'api-server').write_text('https://127.0.0.1:6443\n')
        for name in ('docker','kubectl','curl'):
            path=tools/name;path.write_text(TOOL);path.chmod(0o755)
        environment=mock.patch.dict(os.environ,{'PATH':str(tools)+os.pathsep+os.environ['PATH'],'KML_FAKE_ROUTES':str(root/'routes.json')})
        environment.start();self.addCleanup(environment.stop)
        self.runtime=VolumeRuntime(CLUSTER,NODE,root/'kubeconfig','enabled')

    def cli(self,script):
        path=self.root/'kubectl-memlens';path.write_text('#!/bin/sh\n'+script);path.chmod(0o755)

    def respond(self,document):
        (self.root/'cli.json').write_text(json.dumps(document));self.cli(f'cat "{self.root}/cli.json"\n')

    def test_fresh_sample_still_passes(self):
        self.respond(sample('fresh'))
        self.assertEqual(self.runtime.observe(0)['filesystemAt'],'2026-10-18T00:00:00Z')

    def test_stale_sample_names_returned_availability_and_freshness(self):
        self.respond(sample('stale'))
        with self.assertRaises(Invalid) as caught:self.runtime.observe(0)
        message=str(caught.exception)
        self.assertIn('current filesystem usage unavailable',message)
        self.assertIn("availability='reported'",message)
        self.assertIn("freshness='stale'",message)
        self.assertNotIn('capturedAt',message)

    def test_returned_values_are_bounded_and_printable(self):
        self.respond(sample('stale\x1b[2J'+'y'*4096))
        with self.assertRaises(Invalid) as caught:self.runtime.observe(0)
        message=str(caught.exception)
        self.assertTrue(message.isprintable() and '\x1b' not in message,repr(message))
        self.assertLess(len(message),160,repr(message))

    def test_failed_cli_call_reports_exit_code_and_bounded_stderr_tail(self):
        noise='x'*10000
        self.cli(f"printf 'secret-head {noise}\\033[31m\\n\\aerror: the server rejected the read\\n' >&2\nexit 3\n")
        with self.assertRaises(Invalid) as caught:self.runtime.query(['volumes','pod','persistent'])
        message=str(caught.exception)
        self.assertIn('exit-3',message)
        self.assertIn('error: the server rejected the read',message)
        self.assertTrue(message.isprintable() and '\x1b' not in message and '\x07' not in message,repr(message[-200:]))
        self.assertNotIn('secret-head',message)
        self.assertLess(len(message),2200)

class FailedSampleNumberTests(unittest.TestCase):
    def test_failed_sample_is_named_by_its_number(self):
        class Runtime:
            calls=0
            def observe(self,started):
                self.calls+=1
                # Call 1 is the warm-up; calls 2 and 3 are observations 1 and 2.
                if self.calls==3:raise Invalid("current filesystem usage unavailable: availability='reported' freshness='stale'")
                return {}
        profile=load(Path(__file__).with_name('profiles')/'kind-csi-137.json')
        with self.assertRaises(Invalid) as caught:measure(Runtime(),profile,'enabled',clock=lambda:0,sleep=lambda seconds:None)
        self.assertEqual(str(caught.exception),"enabled volume observation 2 failed: current filesystem usage unavailable: availability='reported' freshness='stale'")

if __name__=='__main__':unittest.main()
