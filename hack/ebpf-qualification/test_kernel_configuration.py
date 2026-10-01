"""Require actual bounded configuration bytes from the selected runtime."""
import gzip
import unittest
from unittest.mock import Mock

from local_runtime import Runtime
from eks_host_runtime import EKSHostRuntime


class KernelConfigurationTests(unittest.TestCase):
    def test_local_reader_keeps_the_kernel_embedded_configuration(self):
        runtime = Runtime.__new__(Runtime)
        runtime.exec = Mock(return_value=gzip.compress(b'CONFIG_MEMCG=y\n'))
        self.assertEqual(runtime.kernel_configuration(), 'CONFIG_MEMCG=y\n')
        runtime.exec.assert_called_once_with(['cat', '/proc/config.gz'])

    def test_local_reader_refuses_truncated_or_oversized_compressed_data(self):
        for raw in (b'invalid', gzip.compress(b'x' * ((1 << 20) + 1))):
            runtime = Runtime.__new__(Runtime)
            runtime.exec = Mock(return_value=raw)
            with self.subTest(size=len(raw)), self.assertRaises((OSError, ValueError)):
                runtime.kernel_configuration()

    def test_eks_reads_only_the_bound_kernel_package_configuration(self):
        runtime = EKSHostRuntime.__new__(EKSHostRuntime)
        runtime.provider = {'nodeInfo': {'kernelVersion': '6.12.1-1.amzn2023.x86_64'}}
        runtime.exec = Mock(return_value=b'CONFIG_MEMCG=y\n')
        self.assertEqual(runtime.kernel_configuration(), 'CONFIG_MEMCG=y\n')
        runtime.exec.assert_called_once_with(['cat', '/boot/config-6.12.1-1.amzn2023.x86_64'])
        runtime.exec.side_effect = RuntimeError('missing package config')
        with self.assertRaisesRegex(RuntimeError, 'missing package config'):
            runtime.kernel_configuration()
        self.assertEqual(runtime.exec.call_count, 2)

    def test_eks_does_not_read_a_path_from_an_invalid_release(self):
        for release in ('../../private', '6.12/other', '', 'kernel\nname'):
            runtime = EKSHostRuntime.__new__(EKSHostRuntime)
            runtime.provider = {'nodeInfo': {'kernelVersion': release}}
            runtime.exec = Mock()
            with self.subTest(release=release), self.assertRaisesRegex(ValueError, 'bound kernel release'):
                runtime.kernel_configuration()
            runtime.exec.assert_not_called()


if __name__ == '__main__':
    unittest.main()
