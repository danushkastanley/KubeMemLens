import contextlib
import io
import json
import unittest

from common import ContractError, load
from kubernetes_runtime import KubernetesRuntime
from sample_nodes import NodeWindows, measure_nodes
from test_contract import ROOT


class Runtime:
    namespace, namespace_uid = "fixture", "namespace-identity"
    workload_namespace, workload_uid = namespace, namespace_uid
    kubectl = ["kubectl", "--context", "fixture"]

    def __init__(self, node, cpu, clock, enabled=True):
        self.node, self.node_uid, self.cpu, self.clock = node, node + "-identity", cpu, clock
        self.enabled, self.reads, self.fail = enabled, 0, False
        self.agent_id = "agent"

    def containers(self):
        result = {"agent": {"id": self.agent_id}, "collector": {"id": "collector"}}
        if self.enabled:
            result["node-context"] = {"id": self.node + "-producer"}
        return result

    def kubelet(self, _):
        return self.cpu, 1000

    def component_metrics(self, _, port):
        if self.fail:
            raise ContractError("observation unavailable")
        if port == 8082:
            return {"kubememlens_agent_last_scan_duration_seconds": .02}
        self.reads += 1
        return {'kubememlens_node_context_reads_total{result="success"}': self.reads,
                "kubememlens_node_context_last_read_seconds": .01,
                "kubememlens_node_context_last_response_bytes": 1000}

    def workload(self):
        return 32, 32

    def api(self, _):
        return {"store": {"reliability": {"freshNodes": 2}, "nodeContext": {"freshRecords": 2}}}

    def stability(self):
        return 0, 0

    def producer_resources(self, *_):
        return self.cpu, 1000

    def projected_identity(self, _):
        threshold = 450 if self.node == "first" else 520
        return "new" if self.clock() >= threshold else "old"


class PoolSamplerTest(unittest.TestCase):
    def setUp(self):
        self.now = 0
        self.clock = lambda: self.now
        self.runtimes = [Runtime("first", 3, self.clock), Runtime("second", 7, self.clock)]

    def test_fixed_window_retains_each_nodes_values_and_rotation(self):
        def sleep(seconds):
            self.now += seconds
        p = load(ROOT / "profiles/gke-standard.json")
        with contextlib.redirect_stdout(io.StringIO()):
            result = measure_nodes(self.runtimes, p, "enabled", self.clock, sleep)
        self.assertEqual(self.now, 690)
        self.assertEqual([len(n["samples"]) for n in result["nodes"]], [44, 44])
        self.assertEqual([n["samples"][0]["producerCPUMilli"] for n in result["nodes"]], [3, 7])
        self.assertEqual([n["rotation"]["elapsedSeconds"] for n in result["nodes"]], [420, 495])
        self.assertTrue(all(n["rotation"]["continuedAcquisition"] for n in result["nodes"]))
        self.assertNotIn("first", json.dumps(result))
        self.assertNotIn("second-identity", json.dumps(result))

    def test_failed_node_does_not_commit_a_partial_pool_sample(self):
        with NodeWindows(self.runtimes, "enabled", self.clock) as windows:
            self.now = 15
            self.runtimes[1].fail = True
            with self.assertRaises(ContractError):
                windows.observe()
            self.assertEqual(windows.samples, [[], []])

    def test_input_order_cannot_swap_the_node_slots(self):
        with NodeWindows(list(reversed(self.runtimes)), "enabled", self.clock) as windows:
            self.now = 15
            windows.observe()
            self.assertEqual([n["samples"][0]["producerCPUMilli"] for n in windows.result()], [3, 7])

    def test_agent_replacement_does_not_reset_stability_to_healthy(self):
        with NodeWindows(self.runtimes, "enabled", self.clock) as windows:
            self.now = 15
            self.runtimes[1].agent_id = "replacement"
            with self.assertRaisesRegex(ContractError, "component replaced"):
                windows.observe()

    def test_duplicate_unbound_or_mixed_targets_are_rejected(self):
        with self.assertRaises(ContractError):
            NodeWindows([self.runtimes[0]] * 2, "enabled")
        self.runtimes[1].node_uid = None
        with self.assertRaises(ContractError):
            NodeWindows(self.runtimes, "enabled")
        self.runtimes[1].node_uid = "second-identity"
        self.runtimes[1].namespace_uid = "different-namespace"
        with self.assertRaises(ContractError):
            NodeWindows(self.runtimes, "enabled")

    def test_node_identity_is_checked_across_new_measurement_windows(self):
        runtime = KubernetesRuntime("/private", "fixture", "fixture", "ns", "node", "/bridge", node_uid="original")
        runtime.k = lambda *_: json.dumps({"metadata": {"uid": "replacement"}})
        with self.assertRaisesRegex(ContractError, "Node identity changed"):
            runtime.kubelet(1)


if __name__ == "__main__":
    unittest.main()
