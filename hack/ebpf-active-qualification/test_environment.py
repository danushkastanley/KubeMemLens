import unittest
from unittest.mock import Mock
from environment import memory_configuration, kernel_accounting

CONFIG = 'CONFIG_MEMCG=y\nCONFIG_CGROUP_BPF=y\nCONFIG_BPF_SYSCALL=y\n'


class EnvironmentTests(unittest.TestCase):
    def test_runtime_configuration_still_requires_live_cgroup_and_boot_checks(self):
        runtime = Mock()
        runtime.kernel_configuration.return_value = CONFIG
        runtime.exec.side_effect = [b'6.12.1', b'quiet', b'cgroup2fs']
        result = kernel_accounting(runtime)
        self.assertEqual(result, memory_configuration(CONFIG, 'quiet', '6.12.1'))
        runtime.kernel_configuration.assert_called_once_with()
        self.assertEqual([call.args[0] for call in runtime.exec.call_args_list],
                         [['uname', '-r'], ['cat', '/proc/cmdline'], ['stat', '-f', '-c', '%T', '/sys/fs/cgroup']])
        runtime.exec.side_effect = [b'6.12.1', b'quiet', b'tmpfs']
        with self.assertRaisesRegex(ValueError, 'unified memory hierarchy'):
            kernel_accounting(runtime)

    def test_current_build_and_explicit_old_option_are_recorded(self):
        value = memory_configuration(CONFIG, 'quiet', '7.0.12-linuxkit')
        self.assertEqual(value['settings']['CONFIG_MEMCG_KMEM'], 'not-present')
        self.assertTrue(value['disablingBootOptionsAbsent'])
        self.assertEqual(memory_configuration(CONFIG + 'CONFIG_MEMCG_KMEM=y\n', '', '5.15.0')['settings']['CONFIG_MEMCG_KMEM'], 'y')

    def test_missing_disabled_or_old_accounting_cannot_pass(self):
        cases = [(CONFIG, '', '5.10.1'), (CONFIG, 'cgroup.memory=nobpf', '7.0.1'),
                 (CONFIG, 'cgroup.memory=nosocket,nokmem', '7.0.1'),
                 (CONFIG, 'cgroup.memory="nobpf"', '7.0.1'),
                 (CONFIG, 'cgroup_disable=cpu,memory', '7.0.1'),
                 (CONFIG + '# CONFIG_MEMCG_KMEM is not set\n', '', '5.15.0'),
                 (CONFIG.replace('CONFIG_MEMCG=y', '# CONFIG_MEMCG is not set'), '', '7.0.1')]
        for args in cases:
            with self.subTest(args=args), self.assertRaises(ValueError):
                memory_configuration(*args)


if __name__ == '__main__':
    unittest.main()
