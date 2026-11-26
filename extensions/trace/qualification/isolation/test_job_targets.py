"""Controller-generated target names retain every existing access probe."""
import unittest

from access import AccessCases
from transport import Observation, QualificationError


class JobTargetTests(unittest.TestCase):
    def test_generated_children_are_used_for_positive_and_foreign_probes(self):
        namespaces={'a':'kml-isolation-a','b':'kml-isolation-b'}
        pods={'a':'target-abc12','b':'target-xyz34'}
        engine='sha256:'+'f'*64;admissions={};calls=[]
        def response(code,body):return Observation(code,1000,100,body)
        def denied(code):
            return response(code,{'apiVersion':'v1','kind':'Status','metadata':{},'status':'Failure',
                'code':code,'reason':'Forbidden' if code==403 else 'NotFound','message':'request denied'})
        class Actor:
            def __init__(self,name):self.name=name
            def call(self,method,path,body=None):
                calls.append((self.name,method,path,body))
                parts=path.split('/');ns=parts[4] if path.startswith('/api/v1/') else parts[5]
                grants={'a':[namespaces['a']],'b':[namespaces['b']],'colleague':[namespaces['a']],
                        'admin':list(namespaces.values()),'none':[]}[self.name]
                if ns not in grants:return denied(403)
                if method=='POST':
                    expected=pods['a' if ns==namespaces['a'] else 'b']
                    if body['pod']!=expected:raise AssertionError('request ignored actual Job child')
                    reference=('%032x'%(len(admissions)+1))
                    value={'apiVersion':'tracing.kubememlens.io/v1alpha1','kind':'TraceAdmission',
                        'metadata':{'name':reference,'namespace':ns},'state':'admitted',
                        'expiresAt':'2030-01-01T00:00:30Z','engineDigest':engine}
                    admissions[reference]=(self.name,value);return response(201,value)
                reference=parts[7];found=admissions.get(reference)
                if not found or found[0]!=self.name:return denied(404)
                if method=='DELETE':
                    del admissions[reference]
                    return response(200,{'apiVersion':'v1','kind':'Status','status':'Success','code':200})
                return response(200,found[1])
        actors={n:Actor(n) for n in ('a','b','colleague','admin','none')}
        suite=AccessCases(actors,namespaces,engine,[],pods=pods)
        result=suite.verify()
        self.assertFalse(admissions)
        self.assertFalse(result['kernelCleanupVerified'])
        self.assertGreater(len(result['checks']),50)
        for peer,namespace in namespaces.items():
            foreign='b' if peer=='a' else 'a'
            self.assertIn((foreign,'GET','/api/v1/namespaces/'+namespace+'/pods/'+pods[peer],None),calls)
            self.assertIn((foreign,'GET','/api/v1/namespaces/'+namespace+'/pods/absent-fixture',None),calls)
        self.assertFalse(any(body and body.get('pod')=='target' for _,_,_,body in calls))

    def test_incomplete_unsafe_or_reserved_child_names_are_rejected(self):
        actors=dict.fromkeys(('a','b','colleague','admin','none'))
        for pods in ({'a':'target-x'},{'a':'target-x','b':'../other'},
                     {'a':'target-x','b':'absent-fixture'},{'a':'target-x','b':'target-y','extra':'target-z'}):
            with self.assertRaises(QualificationError):
                AccessCases(actors,{'a':'one','b':'two'},'sha256:'+'f'*64,[],pods=pods)


if __name__=='__main__':unittest.main()
