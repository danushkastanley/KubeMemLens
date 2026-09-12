#!/usr/bin/env python3
"""Exercise capture confirmation against full and incremental PTY frames."""

import pathlib
import subprocess
import tempfile
import unittest


HELPER = pathlib.Path(__file__).resolve().parent / "lib/node-cockpit-pty.exp"
RUNNER = r"""
source [lindex $argv 0]
set timeout 3
set capture [lindex $argv 1]
log_user 0
spawn -noecho python3 -c [lindex $argv 2] $capture [lindex $argv 3]
require_node_capture $capture
send -- "\r"
expect eof
"""
PRODUCER = r"""
import os,pathlib,sys,tempfile
target=pathlib.Path(sys.argv[1]);scenario=sys.argv[2]
if scenario=='changed-source':
    print('Error: Node evidence changed during the read; refresh and try again',flush=True)
else:
    if scenario!='no-file':
        fd,name=tempfile.mkstemp(dir=target.parent)
        os.write(fd,b'{}');os.close(fd);os.replace(name,target)
    text='Redacted Node capture written' if scenario=='full' else '\x1b[3;10HNode capture written'
    print(text,flush=True)
sys.stdin.readline()
"""


class NodeCapturePTYTest(unittest.TestCase):
    def run_probe(self, scenario):
        with tempfile.TemporaryDirectory(prefix="node-capture-pty-") as folder:
            root = pathlib.Path(folder)
            runner = root / "probe.exp"
            runner.write_text(RUNNER)
            return subprocess.run(
                ["expect", str(runner), str(HELPER), str(root / "capture.json"), PRODUCER, scenario],
                text=True, capture_output=True, timeout=8, check=False,
            )

    def test_full_and_coalesced_frames_both_require_a_new_file(self):
        for scenario in ("full", "incremental"):
            with self.subTest(scenario=scenario):
                result = self.run_probe(scenario)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("PASS private Node capture written", result.stdout)

    def test_confirmation_without_file_fails(self):
        result = self.run_probe("no-file")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("file_created=0 confirmation_seen=1", result.stderr)

    def test_source_change_is_a_failure_not_a_confirmation_timeout(self):
        result = self.run_probe("changed-source")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rejected a changing source sample", result.stderr)


if __name__ == "__main__":
    unittest.main()
