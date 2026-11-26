import hashlib
import json
from pathlib import Path
import stat
import tempfile
import unittest

from session import RunningTrace
from transport import QualificationError


class RunningTraceTests(unittest.TestCase):
    def helper(self, directory, report, progress='', exit_code=0):
        binary = directory/'helper'
        binary.write_text('#!/usr/bin/env python3\nimport sys\nsys.stdout.write('+repr(json.dumps(report))+')\nsys.stderr.write('+repr(progress)+')\nsys.exit('+str(exit_code)+')\n')
        binary.chmod(0o700)
        return binary, hashlib.sha256(binary.read_bytes()).hexdigest()

    def test_pinned_helper_private_modes_and_progress(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            progress = json.dumps({'type': 'admitted', 'id': 'a'*32})+'\n'+json.dumps({'type': 'metadata'})+'\n'
            binary, digest = self.helper(root, {'schemaVersion': 1, 'kind': 'TraceReport', 'redacted': True}, progress)
            running = RunningTrace(binary, digest, {'private': 'fixture-value'}, root/'case')
            self.addCleanup(running.close)
            result = running.finish()
            self.assertEqual(result['exitCode'], 0)
            self.assertFalse(result['forcedTermination'])
            self.assertEqual(running.progress(), ('a'*32, True))
            for path in (root/'case').iterdir():
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertNotIn('fixture-value', repr(running))
            with self.assertRaises(FileExistsError):
                RunningTrace(binary, digest, {}, root/'case')

    def test_nonzero_exit_preserves_report_without_claiming_success(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            report = {'schemaVersion': 1, 'kind': 'TraceReport', 'redacted': True,
                      'cleanup': 'unconfirmed'}
            binary, digest = self.helper(root, report, exit_code=1)
            running = RunningTrace(binary, digest, {}, root/'case')
            self.addCleanup(running.close)
            result = running.finish()
            self.assertEqual(result['exitCode'], 1)
            self.assertEqual(result['report'], report)
            self.assertFalse(result['forcedTermination'])

    def test_changed_binary_cannot_start_or_create_evidence(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            binary, _ = self.helper(root, {})
            with self.assertRaises(QualificationError):
                RunningTrace(binary, '0'*64, {}, root/'case')
            self.assertFalse((root/'case').exists())

    def test_version_two_requires_explicit_known_or_unknown_contract(self):
        for contract in (None, 1, True, 2, '1'):
            with self.subTest(contract=contract), tempfile.TemporaryDirectory() as name:
                root = Path(name)
                report = {'schemaVersion': 2, 'kind': 'TraceReport', 'redacted': True,
                          'contractVersion': contract}
                binary, digest = self.helper(root, report)
                running = RunningTrace(binary, digest, {}, root/'case')
                self.addCleanup(running.close)
                if contract is None or type(contract) is int and contract == 1:
                    self.assertEqual(running.finish()['report'], report)
                else:
                    with self.assertRaises(QualificationError):
                        running.finish()

    def test_unredacted_or_oversized_output_is_not_evidence(self):
        for report in [{'schemaVersion': 1, 'kind': 'TraceReport', 'redacted': False},
                       {'schemaVersion': True, 'kind': 'TraceReport', 'redacted': True},
                       {'schemaVersion': 2, 'kind': 'TraceReport', 'redacted': True},
                       {'schemaVersion': 3, 'kind': 'TraceReport', 'redacted': True},
                       {'schemaVersion': 1, 'kind': 'TraceReport', 'redacted': True, 'data': 'x'*32768}]:
            with tempfile.TemporaryDirectory() as name:
                root = Path(name); binary, digest = self.helper(root, report)
                running = RunningTrace(binary, digest, {}, root/'case')
                self.addCleanup(running.close)
                with self.assertRaises(QualificationError):
                    running.finish()

    def test_bad_progress_never_becomes_a_handle(self):
        for progress in ['{"type":"metadata"}\n', '{"type":"admitted","id":"private-path"}\n', 'x'*4097]:
            with tempfile.TemporaryDirectory() as name:
                root = Path(name)
                binary, digest = self.helper(root, {'schemaVersion': 1, 'kind': 'TraceReport', 'redacted': True}, progress)
                running = RunningTrace(binary, digest, {}, root/'case')
                self.addCleanup(running.close)
                running.finish()
                with self.assertRaises(QualificationError):
                    running.progress()


if __name__ == '__main__':
    unittest.main()
