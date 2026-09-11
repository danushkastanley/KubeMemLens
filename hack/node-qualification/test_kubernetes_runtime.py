import json
import unittest
from unittest.mock import patch

from check_kubernetes_observer import verify_kind_target
from common import ContractError
from install_observers import attach
from kubernetes_runtime import KubernetesRuntime
from observer_specs import ephemeral_observer, host_observer, host_policy


class KubernetesObservationTest(unittest.TestCase):
    def test_local_context_name_cannot_substitute_a_provider_endpoint(self):
        with patch("check_kubernetes_observer.execute", side_effect=["private exported config",
                    json.dumps({"clusters": [{"cluster": {"server": "https://local"}}]}),
                    json.dumps({"clusters": [{"cluster": {"server": "https://provider"}}]})]) as command:
            with self.assertRaises(ContractError):
                verify_kind_target("/private", "kind-kube-memlens-node-context-fixture")
        self.assertEqual(command.call_count, 3)
        self.assertTrue(all("config" in call.args[0] or "kubeconfig" in call.args[0] for call in command.call_args_list))

    def test_footprint_is_read_only_and_has_no_host_process_or_network_access(self):
        pod = host_observer("fixture", "fixture@sha256:" + "a" * 64, {"kubernetes.io/os": "linux"})["spec"]["template"]["spec"]
        self.assertFalse(pod["hostPID"])
        self.assertFalse(pod["hostNetwork"])
        self.assertFalse(pod["automountServiceAccountToken"])
        self.assertEqual(pod["volumes"], [{"name": "cgroups", "hostPath": {"path": "/sys/fs/cgroup", "type": "Directory"}}])
        container = pod["containers"][0]
        self.assertTrue(container["volumeMounts"][0]["readOnly"])
        self.assertFalse(container["securityContext"]["privileged"])
        self.assertEqual(container["securityContext"]["capabilities"], {"drop": ["ALL"]})
        self.assertEqual(host_policy("fixture")["spec"]["egress"], [])
        ephemeral = ephemeral_observer("pinned-image", "node-context")
        self.assertNotIn("resources", ephemeral)
        self.assertEqual(ephemeral["volumeMounts"], [{"name": "kubelet-token", "mountPath": "/identity", "readOnly": True}])

    def test_namespace_replacement_is_rejected_before_reading_pods(self):
        calls = []
        def run(args, **kwargs):
            calls.append(args)
            return json.dumps({"metadata": {"uid": "replacement"}})
        runtime = KubernetesRuntime("/private", "fixture", "fixture", "original", "node", "/bridge", run=run)
        with self.assertRaises(ContractError):
            runtime.pods()
        self.assertEqual(len(calls), 1)
        self.assertIn("namespace", calls[0])

    def test_cgroup_ambiguity_and_counter_resets_fail(self):
        runtime = object.__new__(KubernetesRuntime)
        runtime.host_exec = lambda *args: "/host/sys/fs/cgroup/a\n/host/sys/fs/cgroup/b\n"
        with self.assertRaises(ContractError):
            runtime.group("kubelet.service")
        runtime.previous = {}
        runtime.host_exec = lambda *args: "usage_usec 1000\n\nMEMORY\n100\n"
        self.assertEqual(runtime.resources("producer", "same", "/safe", 1), (None, 100))
        runtime.host_exec = lambda *args: "usage_usec 16000\n\nMEMORY\n120\n"
        self.assertEqual(runtime.resources("producer", "same", "/safe", 16), (1, 120))
        with self.assertRaises(ContractError):
            runtime.resources("producer", "replacement", "/safe", 31)

    def test_existing_observer_is_not_adopted(self):
        class Runtime:
            namespace = "fixture"
            node = "node"
            def verify_namespace(self):
                pass
            def containers(self):
                return {"agent": {"id": "a" * 64, "pod": "agent", "podUID": "original"}}
            def k(self, *args, **kwargs):
                return json.dumps({"metadata": {"uid": "original"}, "spec": {"nodeName": "node",
                    "securityContext": {"runAsUser": 65532, "runAsGroup": 65532}, "ephemeralContainers": [{"name": "someone-else"}]}})
        with self.assertRaises(ContractError):
            attach(Runtime(), "pinned-image", "agent")


if __name__ == "__main__":
    unittest.main()
