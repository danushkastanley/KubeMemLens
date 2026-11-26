"""Prevent unused discovery, attachment or RPC support re-entering the worker."""
import unittest
from worker_dependencies import FORBIDDEN, verify


class DependencyPolicyTests(unittest.TestCase):
    def test_required_core_packages_remain_allowed(self):
        verify("runtime\ngithub.com/cilium/ebpf\nk8s.io/apimachinery/pkg/apis/meta/v1\n"
               "github.com/inspektor-gadget/inspektor-gadget/pkg/operators/ebpf\n")

    def test_each_forbidden_package_and_descendant_is_rejected(self):
        for prefix in FORBIDDEN:
            for name in (prefix.rstrip("/"), prefix.rstrip("/") + "/child"):
                with self.subTest(name=name), self.assertRaises(ValueError):
                    verify("runtime\n" + name + "\n")

    def test_empty_inventory_is_not_a_pass(self):
        with self.assertRaises(ValueError):
            verify("")


if __name__ == "__main__":
    unittest.main()
