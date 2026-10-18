import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from chart_inventory import inventory, verify_inventory
from campaign import Campaign, sources, canonical, digest


class ChartInventoryTests(unittest.TestCase):
    def binding(self, directory):
        path = Path(directory) / 'inventory'
        path.write_bytes(b'approved executable bytes')
        path.chmod(0o700)
        return {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

    def test_verified_binary_receives_render_without_compilation(self):
        with tempfile.TemporaryDirectory() as directory:
            binding = self.binding(directory)
            with patch('chart_inventory.command', return_value=b'[{"kind":"Deployment"}]') as command:
                self.assertEqual(inventory(binding, b'private render'), [{'kind': 'Deployment'}])
            command.assert_called_once_with([binding['path']], b'private render', timeout=15)

    def test_wrong_hash_shared_write_nonexecutable_and_empty_files_never_execute(self):
        for change in ('hash', 'writable', 'executable', 'empty'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                binding = self.binding(directory)
                path = Path(binding['path'])
                if change == 'hash':
                    binding['sha256'] = '0' * 64
                elif change == 'writable':
                    path.chmod(0o720)
                elif change == 'executable':
                    path.chmod(0o600)
                else:
                    path.write_bytes(b'')
                with patch('chart_inventory.command') as command, self.assertRaises(ValueError):
                    inventory(binding, b'render')
                command.assert_not_called()

    def test_symlink_and_non_regular_file_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            binding = self.binding(directory)
            link = Path(directory) / 'link'
            link.symlink_to(binding['path'])
            with self.assertRaises(OSError):
                verify_inventory({**binding, 'path': str(link)})
            with self.assertRaises((OSError, ValueError)):
                verify_inventory({**binding, 'path': directory})

    def test_missing_unknown_or_relative_binding_is_rejected(self):
        for binding in ({}, {'path': 'relative', 'sha256': 'a' * 64},
                        {'path': '/approved', 'sha256': 'a' * 64, 'extra': True},
                        {'path': '/approved', 'sha256': 'unknown'}):
            with self.subTest(binding=binding), self.assertRaises(ValueError):
                verify_inventory(binding)

    def test_change_during_execution_discards_inventory(self):
        with tempfile.TemporaryDirectory() as directory:
            binding = self.binding(directory)
            def replace(*args, **kwargs):
                Path(binding['path']).write_bytes(b'changed executable')
                return b'[]'
            with patch('chart_inventory.command', side_effect=replace):
                with self.assertRaisesRegex(ValueError, 'approved build'):
                    inventory(binding, b'render')

    def test_invalid_binary_fails_before_runtime_or_cluster_access(self):
        from profile import load_profile
        with patch('campaign.sources', return_value={}), patch('campaign.LocalCase') as runtime:
            with self.assertRaisesRegex(ValueError, 'exact prebuilt'):
                Campaign({'sourceSHA256': digest(canonical({})), 'chartInventory': {}}, Path('/unused'), load_profile())
        runtime.assert_not_called()

    def test_frozen_sources_include_chart_templates_values_and_decoder(self):
        bound = sources()
        for path in ('charts/kube-memlens/Chart.yaml', 'charts/kube-memlens/values.yaml',
                     'charts/kube-memlens/values.schema.json', 'charts/kube-memlens/templates/_helpers.tpl',
                     'charts/kube-memlens/templates/deployment.yaml', 'hack/node-qualification/chart-inventory/main.go'):
            self.assertIn(path, bound)

    def test_chart_changed_after_freeze_fails_before_runtime_access(self):
        from profile import load_profile
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('go.mod', 'go.sum', 'prototype/trace/go.mod', 'prototype/trace/go.sum',
                         'charts/kube-memlens/values.yaml'):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('original bytes')
            with patch('campaign.ROOT', root):
                frozen = digest(canonical(sources()))
                (root / 'charts/kube-memlens/values.yaml').write_text('changed placement')
                with patch('campaign.LocalCase') as runtime, patch('campaign.verify_inventory') as decoder:
                    with self.assertRaisesRegex(ValueError, 'source differs'):
                        Campaign({'sourceSHA256': frozen}, root / 'unused-output', load_profile())
                runtime.assert_not_called()
                decoder.assert_not_called()


if __name__ == '__main__':
    unittest.main()
