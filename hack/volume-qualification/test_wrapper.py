import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]
SCRIPT=ROOT/'hack/qualify-volume-kind.sh'

class WrapperTests(unittest.TestCase):
    def test_dry_run_never_calls_cluster_tools(self):
        with tempfile.TemporaryDirectory() as directory:
            tools=Path(directory)
            for name in ('kind','kubectl','docker','helm'):
                path=tools/name;path.write_text('#!/bin/sh\necho cluster-tool-was-called >&2\nexit 99\n');path.chmod(0o755)
            env={**os.environ,'PATH':str(tools)+os.pathsep+os.environ['PATH']}
            result=subprocess.run(['bash',str(SCRIPT),'--dry-run'],cwd=ROOT,env=env,capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertFalse(json.loads(result.stdout)['createsResources'])
    def test_run_requires_explicit_local_acknowledgement(self):
        env={k:v for k,v in os.environ.items() if k not in ('VOLUME_QUALIFICATION_ACKNOWLEDGE','VOLUME_QUALIFICATION_PROFILE')}
        result=subprocess.run(['bash',str(SCRIPT)],cwd=ROOT,env=env,capture_output=True,text=True,timeout=10)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('VOLUME_QUALIFICATION_ACKNOWLEDGE',result.stderr)
    def test_unknown_flags_fail(self):
        result=subprocess.run(['bash',str(SCRIPT),'--provider','eks'],cwd=ROOT,capture_output=True,text=True,timeout=10)
        self.assertNotEqual(result.returncode,0)

if __name__=='__main__':unittest.main()
