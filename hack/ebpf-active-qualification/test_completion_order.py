from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from processes import Processes
from local_runtime import Runtime
from eks_host_runtime import EKSHostRuntime


class CompletionOrderTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.runtime = Mock()
        self.runtime.observer_input_command.side_effect = lambda args: ['native', *args]
        self.case = SimpleNamespace(runtime=self.runtime, cfg={'helpers': {'scheduler': {'path': '/owned/scheduler'}}})
        self.run = Processes(self.case, self.directory, 'owned')

    def test_scheduler_uses_an_owned_input_pipe_and_explicit_completion_mode(self):
        process = Mock()
        self.run.start = Mock(return_value=process)
        self.assertIs(self.run.scheduler('/private/config'), process)
        self.run.start.assert_called_once_with('scheduler', ['native', '/owned/scheduler', '--config', '/private/config',
            '--acknowledge-owned-node', '--completion-signal', 'stdin-eof'], stdin=subprocess.PIPE)
        self.assertIs(self.run.scheduler_completion, process.stdin)

    def test_only_successful_verifier_exit_releases_scheduler(self):
        pipe = Mock(); self.run.scheduler_completion = pipe
        for code in (None, 1):
            self.run.release_scheduler([('scheduler', Mock(), None), ('verifier', Mock(), code)])
            pipe.close.assert_not_called()
        self.run.release_scheduler([('scheduler', Mock(), None), ('verifier', Mock(), 0)])
        pipe.close.assert_called_once()
        self.assertIsNone(self.run.scheduler_completion)
        self.run.release_scheduler([('scheduler', Mock(), 0), ('verifier', Mock(), 0)])
        pipe.close.assert_called_once()

    def test_missing_verifier_and_early_scheduler_exit_are_not_success(self):
        self.run.scheduler_completion = Mock()
        with self.assertRaisesRegex(ValueError, 'requires a verifier'):
            self.run.release_scheduler([('scheduler', Mock(), None)])
        with self.assertRaisesRegex(ValueError, 'before verifier'):
            self.run.release_scheduler([('scheduler', Mock(), 0), ('verifier', Mock(), None)])
        self.run.scheduler_completion.close.assert_not_called()
        self.assertTrue((self.directory / 'failed-processes.private.json').exists())

    def test_failure_cleanup_releases_pipe_before_waiting_for_processes(self):
        events = []
        pipe = Mock(); pipe.close.side_effect = lambda: events.append('pipe')
        process = Mock(); process.poll.return_value = None
        process.wait.side_effect = lambda **kwargs: events.append('process')
        self.run.scheduler_completion = pipe
        self.run.items = [('scheduler', process, Mock(), Mock())]
        self.run.close()
        self.assertEqual(events, ['pipe', 'process'])

    def test_local_and_host_transports_preserve_stdin_without_crossing_scope(self):
        local = Runtime.__new__(Runtime); local.cfg = {'node': 'owned-worker'}
        self.assertEqual(local.observer_input_command(['helper']), ['docker', 'exec', '-i', 'owned-worker', 'helper'])
        host = EKSHostRuntime.__new__(EKSHostRuntime); host.verify_host = Mock()
        self.assertEqual(host.observer_input_command(['helper']), ['helper'])
        host.verify_host.assert_called_once()


if __name__ == '__main__':
    unittest.main()
