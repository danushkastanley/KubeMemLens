"""Ownership and mutation boundaries without touching a real cluster."""

from copy import deepcopy
import unittest
from unittest.mock import patch

from local_runtime import Runtime, SERVICES, deployment_names, pod_selector, spec_digest


class RuntimeGuards(unittest.TestCase):
    def test_spec_hash_allows_only_replica_transition(self):
        spec = {"replicas": 1, "template": {"spec": {"containers": [{"image": "immutable", "args": ["node"]}]}}}
        before = spec_digest(spec)
        changed = deepcopy(spec)
        changed["replicas"] = 0
        self.assertEqual(before, spec_digest(changed))
        changed["template"]["spec"]["containers"][0]["image"] = "different"
        self.assertNotEqual(before, spec_digest(changed))

    def test_rejects_nonlocal_context_before_command(self):
        with patch("local_runtime.command") as command:
            with self.assertRaises(ValueError):
                Runtime({"context": "production", "node": "node"})
            command.assert_not_called()

    def test_scaling_requires_exact_uid_and_resource_version(self):
        runtime = Runtime.__new__(Runtime)
        runtime.cfg = {"namespace": "owned"}
        runtime.services = dict(SERVICES)
        runtime.kube = ["kubectl", "--context", "kind-test"]
        runtime.deployment = lambda _: {"metadata": {"uid": "owned-uid", "resourceVersion": "17"}}
        with patch("local_runtime.command") as command:
            runtime.scale("node", 0)
            body = command.call_args.args[1].decode()
            self.assertIn('"path":"/metadata/uid","value":"owned-uid"', body)
            self.assertIn('"path":"/metadata/resourceVersion","value":"17"', body)
            self.assertIn('"path":"/spec/replicas","value":0', body)

    def test_chart_deployment_name_is_used_for_guarded_scaling(self):
        runtime = Runtime.__new__(Runtime)
        runtime.cfg = {"namespace": "owned"}
        runtime.services = {"api": "reviewed-trace-api", "node": "reviewed-trace-node-local"}
        runtime.kube = ["kubectl", "--context", "kind-test"]
        runtime.deployment = lambda _: {"metadata": {"uid": "owned-uid", "resourceVersion": "17"}}
        with patch("local_runtime.command") as command:
            runtime.scale("node", 0)
            self.assertIn("reviewed-trace-node-local", command.call_args.args[0])
            self.assertNotIn("binding-node", command.call_args.args[0])

    def test_chart_pods_use_the_pinned_deployment_selector(self):
        runtime = Runtime.__new__(Runtime)
        runtime.cfg = {"namespace": "owned"}
        selector = {"app.kubernetes.io/instance": "reviewed", "app.kubernetes.io/component": "trace-node",
                    "tracing.kubememlens.io/node-id": "local"}
        runtime.deployment = lambda _: {"spec": {"selector": {"matchLabels": selector}}}
        calls = []
        runtime.json = lambda args: calls.append(args) or {"items": [{"metadata": {"uid": "pod"}}]}
        self.assertEqual(runtime.pods("node"), [{"metadata": {"uid": "pod"}}])
        self.assertEqual(calls[0][calls[0].index("-l") + 1],
                         "app.kubernetes.io/component=trace-node,app.kubernetes.io/instance=reviewed,tracing.kubememlens.io/node-id=local")

    def test_ambiguous_deployment_names_fail_before_cluster_access(self):
        cfg = {"context": "kind-test", "node": "owned-node", "namespace": "owned", "kubeconfig": "private"}
        for names in ({}, {"api": "one"}, {"api": "same", "node": "same"},
                      {"api": "../foreign", "node": "node"}, {"api": "api", "node": "node", "other": "extra"}):
            with self.subTest(names=names), patch("local_runtime.command") as command:
                with self.assertRaises(ValueError):
                    Runtime({**cfg, "deploymentNames": names})
                command.assert_not_called()

    def test_legacy_names_and_selector_keep_their_existing_contract(self):
        self.assertEqual(deployment_names({}), SERVICES)
        self.assertEqual(pod_selector({"spec": {"selector": {"matchLabels": {"app": "binding-node"}}}}),
                         "app=binding-node")

    def test_empty_expression_or_injected_selector_fails(self):
        for selector in ({}, {"matchLabels": {}}, {"matchLabels": {"app": "node"}, "matchExpressions": []},
                         {"matchLabels": {"app": "node,foreign=yes"}}, {"matchLabels": {"app=foreign": "node"}}):
            with self.subTest(selector=selector), self.assertRaises(ValueError):
                pod_selector({"spec": {"selector": selector}})

    def test_refuses_scale_outside_zero_or_one(self):
        runtime = Runtime.__new__(Runtime)
        for count in (-1, 2, True):
            with self.assertRaises(ValueError):
                runtime.scale("node", count)

    def test_absent_pods_do_not_hide_live_runtime_process(self):
        runtime = Runtime.__new__(Runtime)
        runtime.absent = lambda: None
        runtime.exec = lambda _: b'{"containers":[{"id":"abc","state":"CONTAINER_RUNNING"}]}'
        with self.assertRaises(ValueError):
            runtime.stopped({"node": {"container": "abc"}})
        runtime.exec = lambda _: b'{"containers":[{"id":"other","state":"CONTAINER_EXITED"}]}'
        with self.assertRaises(ValueError):
            runtime.stopped({"node": {"container": "abc"}})
        runtime.exec = lambda _: b'{"containers":[{"id":"abc","state":"CONTAINER_EXITED"}]}'
        runtime.stopped({"node": {"container": "abc"}})


if __name__ == "__main__":
    unittest.main()
