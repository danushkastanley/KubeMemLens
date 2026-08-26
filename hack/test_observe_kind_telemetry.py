#!/usr/bin/env python3

import json
import unittest

from observe_kind_telemetry import KindTelemetryObserver, ObservationError


AGENT_A = "a" * 64
AGENT_B = "b" * 64
COLLECTOR = "c" * 64
NAMESPACE = "private-observer-namespace"


def container(container_id, component, namespace=NAMESPACE, pod="private-pod"):
    return {
        "id": container_id,
        "metadata": {"name": component},
        "labels": {
            "io.kubernetes.container.name": component,
            "io.kubernetes.pod.name": pod,
            "io.kubernetes.pod.namespace": namespace,
        },
    }


def stat(container_id, working_set):
    return {
        "attributes": {"id": container_id},
        "memory": {"workingSetBytes": {"value": str(working_set)}},
    }


def inspect(memory_limit, quota=None):
    cpu = {"shares": 10}
    if quota is not None:
        cpu["quota"] = quota
    return {
        "info": {
            "runtimeSpec": {
                "linux": {
                    "resources": {"memory": {"limit": memory_limit}, "cpu": cpu}
                }
            }
        }
    }


class FakeRunner:
    def __init__(self, nodes, malformed=None):
        self.nodes = nodes
        self.malformed = malformed
        self.calls = []

    def __call__(self, argv, timeout):
        argv = tuple(argv)
        self.calls.append((argv, timeout))
        node = argv[2]
        command = argv[3:]
        if self.malformed == command:
            return "not-json"
        data = self.nodes[node]
        if command == ("crictl", "ps", "-o", "json"):
            return json.dumps({"containers": data["containers"]})
        if command == ("crictl", "stats", "-o", "json"):
            return json.dumps({"stats": data["stats"]})
        if command[:4] == ("crictl", "inspect", "-o", "json"):
            return json.dumps(data["inspect"][command[4]])
        if command[0] == "find":
            container_id = command[-2].strip("*")
            paths = data.get("paths", {}).get(container_id, [cpu_path(container_id)])
            return "".join(path + "\n" for path in paths)
        if command[0] == "cat":
            container_id = command[1].split("cri-containerd-")[1].split(".scope")[0]
            usage, throttled = data["cpu"][container_id]
            return f"usage_usec {usage}\nuser_usec 0\nsystem_usec 0\nthrottled_usec {throttled}\n"
        raise AssertionError(f"unexpected command shape: {command[0]}")


def cpu_path(container_id):
    return f"/sys/fs/cgroup/kubepods/cri-containerd-{container_id}.scope/cpu.stat"


def node_data(containers, stats, inspections, cpu):
    return {"containers": containers, "stats": stats, "inspect": inspections, "cpu": cpu}


class KindTelemetryObserverTests(unittest.TestCase):
    def test_aggregates_agent_and_collector_across_nodes(self):
        runner = FakeRunner(
            {
                "kind-control-plane": node_data(
                    [container(AGENT_A, "agent"), container(COLLECTOR, "collector")],
                    [stat(AGENT_A, 100), stat(COLLECTOR, 300)],
                    {AGENT_A: inspect(1000, 50000), COLLECTOR: inspect(3000, 50000)},
                    {AGENT_A: (1000, 10), COLLECTOR: (4000, 20)},
                ),
                "kind-worker": node_data(
                    [container(AGENT_B, "agent")],
                    [stat(AGENT_B, 200)],
                    {AGENT_B: inspect(2000, 50000)},
                    {AGENT_B: (2000, 30)},
                ),
            }
        )
        previous = {AGENT_A: (900, 5), AGENT_B: (1900, 29), COLLECTOR: (3000, 15)}
        result = KindTelemetryObserver(runner, previous).observe(
            ["kind-control-plane", "kind-worker"], NAMESPACE
        )

        self.assertEqual(
            result["components"]["agent"],
            {
                "containerCount": 2,
                "workingSetBytes": 300,
                "memoryLimitBytes": 3000,
                "maximumMemoryLimitRatio": 0.1,
                "cpuUsageUsec": 3000,
                "cpuThrottledUsec": 40,
                "cpuQuotaConfigured": True,
                "maximumCPUThrottlingRatio": 0.05,
            },
        )
        self.assertEqual(result["components"]["collector"]["containerCount"], 1)
        self.assertTrue(all(call[0][0:2] == ("docker", "exec") for call in runner.calls))
        self.assertTrue(all(call[1] == 10 for call in runner.calls))

    def test_no_cpu_quota_reports_zero_throttling_percent(self):
        runner = FakeRunner(
            {
                "kind-control-plane": node_data(
                    [container(AGENT_A, "agent"), container(COLLECTOR, "collector")],
                    [stat(AGENT_A, 100), stat(COLLECTOR, 300)],
                    {AGENT_A: inspect(1000, -1), COLLECTOR: inspect(3000, -1)},
                    {AGENT_A: (500, 0), COLLECTOR: (700, 0)},
                )
            }
        )
        previous = {AGENT_A: (400, 0), COLLECTOR: (600, 0)}
        result = KindTelemetryObserver(runner, previous).observe(["kind-control-plane"], NAMESPACE)
        agent = result["components"]["agent"]
        self.assertEqual(agent["cpuUsageUsec"], 500)
        self.assertEqual(agent["cpuThrottledUsec"], 0)
        self.assertEqual(agent["maximumCPUThrottlingRatio"], 0.0)

    def test_rejects_reset_interval_counters(self):
        runner = FakeRunner(
            {
                "kind-control-plane": node_data(
                    [container(AGENT_A, "agent"), container(COLLECTOR, "collector")],
                    [stat(AGENT_A, 100), stat(COLLECTOR, 300)],
                    {AGENT_A: inspect(1000), COLLECTOR: inspect(3000)},
                    {AGENT_A: (500, 0), COLLECTOR: (700, 0)},
                )
            }
        )
        previous = {AGENT_A: (600, 0), COLLECTOR: (600, 0)}
        with self.assertRaisesRegex(ObservationError, "counters reset"):
            KindTelemetryObserver(runner, previous).observe(["kind-control-plane"], NAMESPACE)

    def test_rejects_malformed_or_missing_runtime_data(self):
        valid = node_data(
            [container(AGENT_A, "agent")],
            [stat(AGENT_A, 100)],
            {AGENT_A: inspect(1000)},
            {AGENT_A: (500, 0)},
        )
        cases = {
            "malformed json": FakeRunner(
                {"kind-control-plane": valid},
                malformed=("crictl", "ps", "-o", "json"),
            ),
            "missing stats": FakeRunner(
                {
                    "kind-control-plane": node_data(
                        [container(AGENT_A, "agent")], [], {AGENT_A: inspect(1000)}, {AGENT_A: (500, 0)}
                    )
                }
            ),
            "missing memory limit": FakeRunner(
                {
                    "kind-control-plane": node_data(
                        [container(AGENT_A, "agent")],
                        [stat(AGENT_A, 100)],
                        {AGENT_A: {"info": {"runtimeSpec": {"linux": {"resources": {"cpu": {}}}}}}},
                        {AGENT_A: (500, 0)},
                    )
                }
            ),
            "ambiguous cgroup": FakeRunner(
                {
                    "kind-control-plane": {
                        **valid,
                        "paths": {AGENT_A: [cpu_path(AGENT_A), "/sys/fs/cgroup/other/" + AGENT_A + "/cpu.stat"]},
                    }
                }
            ),
            "missing cgroup": FakeRunner(
                {"kind-control-plane": {**valid, "paths": {AGENT_A: []}}}
            ),
        }
        for name, runner in cases.items():
            with self.subTest(name=name), self.assertRaises(ObservationError):
                KindTelemetryObserver(runner).observe(["kind-control-plane"], NAMESPACE)

    def test_output_excludes_runtime_and_kubernetes_identifiers(self):
        private_node = "kind-private-node"
        private_pod = "customer-secret-pod"
        runner = FakeRunner(
            {
                private_node: node_data(
                    [
                        container(AGENT_A, "agent", pod=private_pod),
                        container(COLLECTOR, "collector", pod=private_pod),
                    ],
                    [stat(AGENT_A, 100), stat(COLLECTOR, 300)],
                    {AGENT_A: inspect(1000), COLLECTOR: inspect(3000)},
                    {AGENT_A: (500, 0), COLLECTOR: (700, 0)},
                )
            }
        )
        encoded = json.dumps(
            KindTelemetryObserver(runner).observe([private_node], NAMESPACE),
            sort_keys=True,
        )
        for secret in (private_node, private_pod, NAMESPACE, AGENT_A, COLLECTOR):
            self.assertNotIn(secret, encoded)
        self.assertEqual(
            set(json.loads(encoded)["components"]), {"agent", "collector"}
        )


if __name__ == "__main__":
    unittest.main()
