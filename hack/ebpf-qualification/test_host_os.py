from pathlib import Path
import tempfile
import unittest

from host_os import read_release


class HostOSReleaseTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / 'os-release'

    def test_reads_quoted_distribution_fields_without_new_platform_api(self):
        self.path.write_text('NAME="Amazon Linux"\nID="amzn"\nVERSION_ID="2023"\nID_LIKE="fedora"\n')
        self.assertEqual(read_release(self.path), {'ID': 'amzn', 'VERSION_ID': '2023'})
        self.path.write_text("# identity\nID='amzn'\nVERSION_ID=2023\n")
        self.assertEqual(read_release(self.path), {'ID': 'amzn', 'VERSION_ID': '2023'})

    def test_symlinked_system_release_file_is_supported(self):
        target = self.path.with_name('vendor-release')
        target.write_text('ID=amzn\nVERSION_ID=2023\n')
        self.path.symlink_to(target)
        self.assertEqual(read_release(self.path), {'ID': 'amzn', 'VERSION_ID': '2023'})

    def test_missing_duplicate_and_malformed_identity_fields_fail(self):
        for text in ('ID=amzn\n', 'ID_LIKE=amzn\nVERSION_ID=2023\n',
                     'ID=amzn\nID=debian\nVERSION_ID=2023\n',
                     'ID=amzn\nVERSION_ID=2023\nVERSION_ID=2\n',
                     'ID\nVERSION_ID=2023\n', 'ID=amzn other\nVERSION_ID=2023\n',
                     'ID="amzn\nVERSION_ID=2023\n'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                self.path.write_text(text)
                read_release(self.path)

    def test_other_distributions_are_not_rewritten_as_al2023(self):
        self.path.write_text('ID=debian\nVERSION_ID=13\nID_LIKE=amzn\n')
        self.assertEqual(read_release(self.path), {'ID': 'debian', 'VERSION_ID': '13'})

    def test_shell_substitution_is_never_executed(self):
        marker = self.path.with_name('must-not-exist')
        self.path.write_text(f'ID="$(touch {marker})"\nVERSION_ID=2023\n')
        self.assertEqual(read_release(self.path)['ID'], f'$(touch {marker})')
        self.assertFalse(marker.exists())

    def test_missing_invalid_encoding_and_oversized_file_fail(self):
        with self.assertRaises(FileNotFoundError):
            read_release(self.path)
        for raw in (b'ID=amzn\nVERSION_ID=2023\n\xff', b'ID=amzn\nVERSION_ID=2023\n' + b'#' * 16384):
            with self.subTest(length=len(raw)), self.assertRaises(ValueError):
                self.path.write_bytes(raw)
                read_release(self.path)


if __name__ == '__main__':
    unittest.main()
