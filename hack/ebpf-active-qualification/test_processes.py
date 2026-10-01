from pathlib import Path
import json
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from processes import Processes


class ProcessTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)

    def test_observer_execution_uses_the_verified_runtime_command(self):
        runtime = Mock()
        argv = ['/usr/local/bin/kml-measure', '--config', '/private/input']
        runtime.observer_command.return_value = argv
        case = SimpleNamespace(runtime=runtime, cfg={'helpers': {'measure': {'path': argv[0]}}})
        processes = Processes(case, None, 'owned')
        processes.start = Mock()
        processes.native('resources', 'measure', '--config', '/private/input')
        runtime.observer_command.assert_called_once_with(argv)
        processes.start.assert_called_once_with('resources', argv)

    def test_complete_delivery_budget_failure_is_retained_but_crash_is_not(self):
        processes = Processes(None, self.directory, 'test')
        delivery = Mock(returncode=1)
        delivery.poll.return_value = 1
        processes.items = [('delivery-00', delivery, Mock(), Mock())]
        with self.assertRaises(ValueError):
            processes.wait_all(0)
        receipt = json.loads((self.directory / 'failed-processes.private.json').read_text())
        self.assertEqual(receipt['processes'], [{'label': 'delivery-00', 'exitCode': 1}])
        processes.accept_delivery_budget_failure(delivery)
        processes.wait_all(0)
        processes.healthy()
        crashed = Mock(returncode=2)
        with self.assertRaises(ValueError):
            processes.accept_delivery_budget_failure(crashed)
        resources = Mock(returncode=1)
        processes.items.append(('resources', resources, Mock(), Mock()))
        with self.assertRaises(ValueError):
            processes.accept_delivery_budget_failure(resources)

    def test_successful_observer_early_exit_is_invalid(self):
        processes = Processes(None, self.directory, 'test')
        observer = Mock()
        observer.poll.return_value = 0
        processes.items = [('resources', observer, Mock(), Mock())]
        with self.assertRaises(ValueError):
            processes.healthy()
        receipt = json.loads((self.directory / 'failed-processes.private.json').read_text())
        self.assertEqual(receipt['processes'], [{'label': 'resources', 'exitCode': 0}])

    def test_successful_scheduler_or_verifier_exit_before_window_end_is_invalid(self):
        for name in ('scheduler', 'verifier'):
            with self.subTest(name=name):
                directory = self.directory / name
                directory.mkdir()
                processes = Processes(None, directory, 'test')
                observer = Mock()
                observer.poll.return_value = 0
                processes.items = [(name, observer, Mock(), Mock())]
                with self.assertRaises(ValueError):
                    processes.healthy()
                receipt = json.loads((directory / 'failed-processes.private.json').read_text())
                self.assertEqual(receipt['processes'], [{'label': name, 'exitCode': 0}])

    def test_first_failure_excludes_still_running_workloads(self):
        processes = Processes(None, self.directory, 'test')
        processes.items = [(name, Mock(poll=Mock(return_value=code)), Mock(), Mock())
                           for name, code in [('resources', None), ('standard', 1), ('workload', None)]]
        with self.assertRaises(ValueError):
            processes.wait_all(0)
        receipt = json.loads((self.directory / 'failed-processes.private.json').read_text())
        self.assertEqual(receipt['processes'], [{'label': 'standard', 'exitCode': 1}])

    def test_process_label_cannot_escape_evidence_directory(self):
        processes = Processes(None, self.directory, 'test')
        with self.assertRaisesRegex(ValueError, 'process label'):
            processes.start('../private', ['unused'])
        self.assertEqual(list(self.directory.iterdir()), [])

    def test_cleanup_never_deletes_changed_private_configuration(self):
        runtime = Mock()
        runtime.exec.return_value = b'changed  /tmp/owned.json\n'
        processes = Processes(SimpleNamespace(runtime=runtime), None, 'test')
        processes.private = {'/tmp/owned.json': 'expected'}
        with self.assertRaises(ValueError):
            processes.close()
        self.assertEqual(runtime.exec.call_count, 1)
        self.assertEqual(runtime.exec.call_args.args[0], ['sha256sum', '/tmp/owned.json'])


if __name__ == '__main__':
    unittest.main()
