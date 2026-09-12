import copy
import json
import tempfile
import unittest
from pathlib import Path
from common import Invalid, load, write_new
from record import prepare, finalise
from test_contract import fixture

class RecordTests(unittest.TestCase):
    def stage(self,root):
        p,e,s=fixture();work=root/'private';out=root/'public';work.mkdir();out.mkdir()
        def private(name,value):(work/name).write_text(json.dumps(value))
        private('volume-cockpit-result.json',e['workflow'])
        private('volume-pressure-result.json',{'filesystemPressure':True,'inodePressure':True})
        private('volume-health-result.json',{'conflictingSourcesVerified':True,'profile':'alpha','outcome':'passed'})
        private('volume-result.json',{'producerReplacement':True,'collectorRestart':True,'crossNamespaceDenied':True,'nodeViewerDenied':True,'profileRollback':True,'volumeDisabledNodeMemoryFresh':True,'outcome':'passed','recovery':e['recovery']})
        private('volume-doctor.json',{'checks':[{'name':'connection','status':'pass','summary':'private identity kept outside record'}]})
        write_new(out/'volume-source.json',s);write_new(work/'volume-environment.json',e['environment'])
        for phase,value in e['windows'].items():write_new(out/('volume-window-'+phase+'.json'),value)
        prepare(p,work,out)
        return p,e,s,work,out
    def test_cleanup_is_required_before_finalisation(self):
        with tempfile.TemporaryDirectory() as directory:
            p,e,s,work,out=self.stage(Path(directory))
            write_new(out/'summary.json',{'cleanup':'pending','sourceCommit':s['commit'],'sourceTreeSHA256':s['tree'].removeprefix('sha256:')})
            with self.assertRaises(Invalid):finalise(p,out)
            self.assertFalse((out/'volume-qualification.json').exists())
    def test_finalise_binds_source_and_preserves_raw_privacy(self):
        with tempfile.TemporaryDirectory() as directory:
            p,e,s,work,out=self.stage(Path(directory))
            write_new(out/'summary.json',{'cleanup':'passed','sourceCommit':s['commit'],'sourceTreeSHA256':s['tree'].removeprefix('sha256:')})
            self.assertTrue(finalise(p,out))
            self.assertNotIn('private identity',(out/'volume-qualification.json').read_text())
            self.assertEqual(load(out/'volume-evaluation.json')['outcome'],'pass')
            with self.assertRaises(FileExistsError):finalise(p,out)
    def test_different_cleanup_source_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            p,e,s,work,out=self.stage(Path(directory))
            write_new(out/'summary.json',{'cleanup':'passed','sourceCommit':'f'*40,'sourceTreeSHA256':s['tree'].removeprefix('sha256:')})
            with self.assertRaises(Invalid):finalise(p,out)

if __name__=='__main__':unittest.main()
