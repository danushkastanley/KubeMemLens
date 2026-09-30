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


SELECTION_RUNNER = r"""
source [lindex $argv 0]
set timeout 2
set send_slow {1 0.001}
log_user 0
spawn -noecho python3 -c [lindex $argv 1] [lindex $argv 2]
expect -re {CHARGE}
select_node_cockpit fixture-worker
send -- "q"
expect eof
lassign [wait] pid spawn_id os_error status
if {$os_error != 0 || $status != 0} {exit 1}
"""
SELECTION_PRODUCER = r"""
import os,select,sys,tty
tty.setraw(0)
scenario=sys.argv[1];query=None;selected=0
def emit(value):os.write(1,(value+'\r\n').encode())
emit('CHARGE')
while True:
    key=os.read(0,1).decode()
    if not key:break
    if query is not None:
        if key=='\r':
            if query!='fixture-worker':sys.exit(2)
            query=None;emit('CHARGE')
        else:query+=key
    elif key=='/':query='';emit('search:')
    elif key=='e':
        node='fixture-worker2' if scenario=='missing' or (scenario in ('prefix','split-prefix') and selected==0) else 'fixture-worker'
        if scenario in ('split','split-prefix'):
            partial='fixture-' if scenario=='split' else 'fixture-worker'
            os.write(1,('Node cockpit: '+partial).encode())
            # A command before the title terminator proves premature selection.
            if select.select([0],[],[],0.1)[0]:sys.exit(3)
            emit(node[len(partial):])
        elif scenario=='coalesced':emit('\x1b[3;2Hode cockpit: '+node)
        elif scenario=='wrong-column':emit('\x1b[3;3Hode cockpit: '+node)
        else:emit('Node cockpit: '+node)
    elif key=='h':emit('CHARGE')
    elif key=='j':selected+=1
    elif key=='q':break
"""


class NodeSelectionPTYTest(unittest.TestCase):
    def test_filter_selects_requested_node_including_prefix_collision(self):
        for scenario in ("direct", "prefix", "split", "split-prefix", "coalesced"):
            with self.subTest(scenario=scenario):
                result = self.run_selection(scenario)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("PASS requested Node selected", result.stdout)

    def test_another_node_cannot_satisfy_target_selection(self):
        result = self.run_selection("missing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("did not select the requested Node", result.stderr)

    def test_title_suffix_at_another_cursor_position_does_not_pass(self):
        result = self.run_selection("wrong-column")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("target detail did not open", result.stderr)

    def run_selection(self, scenario):
        with tempfile.TemporaryDirectory(prefix="node-selection-pty-") as folder:
            runner = pathlib.Path(folder) / "selection.exp"
            runner.write_text(SELECTION_RUNNER)
            return subprocess.run(["expect", str(runner), str(HELPER), SELECTION_PRODUCER, scenario],
                                  text=True, capture_output=True, timeout=8, check=False)


if __name__ == "__main__":
    unittest.main()
