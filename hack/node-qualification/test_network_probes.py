import unittest
from contextlib import contextmanager
from types import SimpleNamespace
from unittest.mock import patch

from common import ContractError
from network_probes import NetworkChecks, url
from network_specs import APP
from owned_resources import Resource


class NetworkProbeTest(unittest.TestCase):
    def setUp(self):
        execution = SimpleNamespace(windows={"baseline": {}, "enabled": {}}, k=object(), ownership=object(),
                                    bundle=SimpleNamespace(configuration={"namespace": "fixture"},
                                                           profile={"workload": {"image": "registry.example/probe@sha256:" + "a" * 64}}),
                                    binding={"nodes": [{"name": "node-a"}, {"name": "node-b"}]},
                                    runtimes=[SimpleNamespace(node=n) for n in ("node-a", "node-b")], verify_binding=lambda: None)
        self.checks = NetworkChecks(execution)
        self.checks.wait_state = lambda direction, slot, expected: self.checks.request(direction, slot) == expected
        self.allowed, self.suspensions = True, 0

    @contextmanager
    def suspend(self, *_):
        self.suspensions += 1
        self.allowed = False
        try:
            yield
        finally:
            self.allowed = True

    def test_unenforced_deny_does_not_pass_with_successful_positive_controls(self):
        self.checks.request = lambda *_: "reachable"
        with patch("network_probes.suspended_network_rule", self.suspend):
            result = self.checks.cycle("egress", 0)
        self.assertEqual(result, {"allowedBefore": True, "blocked": False, "allowedAfter": True})

    def test_unreachable_target_does_not_count_as_enforcement(self):
        self.checks.request = lambda *_: "blocked"
        with patch("network_probes.suspended_network_rule", self.suspend):
            result = self.checks.cycle("ingress", 0)
        self.assertEqual(result, {"allowedBefore": False, "blocked": False, "allowedAfter": False})
        self.assertEqual(self.suspensions, 0)

    def test_complete_allow_deny_allow_cycle_is_required(self):
        self.checks.request = lambda *_: "reachable" if self.allowed else "blocked"
        with patch("network_probes.suspended_network_rule", self.suspend):
            result = self.checks.cycle("egress", 1)
        self.assertTrue(all(result.values()))
        self.assertTrue(self.allowed)

    def test_one_node_failure_keeps_the_pool_unqualified(self):
        self.checks.prepare = lambda: None
        self.checks.verify_policy = lambda: None
        self.checks.cycle = lambda direction, slot: {"allowedBefore": True, "blocked": slot == 0, "allowedAfter": True}
        with patch("network_probes.PoolRecovery") as recovery:
            recovery.return_value.wait.return_value = True
            result = self.checks.run()
        self.assertFalse(result["passed"])
        self.assertEqual(result["ownNodeIsolation"], "not-claimed")
        self.assertEqual([n["slot"] for n in result["nodes"]], [0, 1])

    def test_network_controls_cannot_pass_when_fresh_source_reports_do_not_return(self):
        self.checks.prepare = lambda: None
        self.checks.verify_policy = lambda: None
        self.checks.cycle = lambda *_: {"allowedBefore": True, "blocked": True, "allowedAfter": True}
        with patch("network_probes.PoolRecovery") as recovery:
            recovery.return_value.wait.return_value = False
            result = self.checks.run()
        self.assertFalse(result["passed"])
        self.assertFalse(result["freshSourceRetained"])

    def test_invalid_or_non_pod_addresses_are_not_requested(self):
        self.assertEqual(url("10.0.1.2"), "http://10.0.1.2:18080/")
        self.assertEqual(url("fd00::2"), "http://[fd00::2]:18080/")
        for address in ("127.0.0.1", "0.0.0.0", "169.254.1.1", "224.0.0.1"):
            with self.assertRaises(ContractError):
                url(address)
        with self.assertRaises(ValueError):
            url("https://other.invalid")

    def test_api_omitted_empty_direction_keeps_the_same_policy_semantics(self):
        resource = Resource("networking.k8s.io/v1", "NetworkPolicy", "kube-memlens-node-context", "fixture")
        expected = {"spec": {"podSelector": {"matchLabels": APP}, "policyTypes": ["Ingress", "Egress"],
                             "ingress": [], "egress": [{"ports": [{"protocol": "TCP", "port": 443}]}]}}
        actual = {"spec": {k: v for k, v in expected["spec"].items() if k != "ingress"}}
        self.checks.owner = SimpleNamespace(verify=lambda _: actual)
        self.checks.e.installer = SimpleNamespace(namespace=Resource("v1", "Namespace", "fixture"),
                                                desired={"enabled": {resource: expected}})
        self.checks.verify_policy()
        actual["spec"]["ingress"] = [{}]
        with self.assertRaises(ContractError):
            self.checks.verify_policy()


if __name__ == "__main__":
    unittest.main()
