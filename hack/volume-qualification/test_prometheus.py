import unittest
from common import Invalid
from prometheus import api_counters

HEADER='# TYPE apiserver_request_total counter\n'

def row(resource,verb,count,group=''):
    return f'apiserver_request_total{{group="{group}",resource="{resource}",verb="{verb}",subresource=""}} {count}\n'

class CounterTests(unittest.TestCase):
    def test_reads_access_reviews_and_persisted_writes_are_separate(self):
        text=HEADER+row('pods','GET',4)+row('subjectaccessreviews','POST',3,'authorization.k8s.io')+row('tokenreviews','POST',2,'authentication.k8s.io')+row('pods','PATCH',5)+row('csinodes','PUT',6,'storage.k8s.io')+row('leases','PUT',100,'coordination.k8s.io')+row('pods','GET',50,'memory.kubememlens.io')
        self.assertEqual(api_counters(text),(9,11))
    def test_missing_or_invalid_family_is_not_zero(self):
        for text in ('',HEADER,HEADER+row('pods','GET','NaN'),HEADER+row('pods','GET',-1),HEADER+'apiserver_request_total{resource="pods"} 1\n'):
            with self.assertRaises(Invalid):api_counters(text)
    def test_measured_absent_label_series_is_zero(self):
        self.assertEqual(api_counters(HEADER+row('leases','GET',3,'coordination.k8s.io')),(0,0))
    def test_duplicate_labels_rejected(self):
        with self.assertRaises(Invalid):api_counters(HEADER+'apiserver_request_total{group="",group="",resource="pods",verb="GET"} 1\n')

if __name__=='__main__':unittest.main()
