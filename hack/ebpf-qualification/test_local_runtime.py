"""Ownership and mutation boundaries without touching a real cluster."""

from copy import deepcopy
import unittest
from unittest.mock import patch

from local_runtime import Runtime, spec_digest


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
        runtime.kube = ["kubectl", "--context", "kind-test"]
        runtime.deployment = lambda _: {"metadata": {"uid": "owned-uid", "resourceVersion": "17"}}
        with patch("local_runtime.command") as command:
            runtime.scale("node", 0)
            body = command.call_args.args[1].decode()
            self.assertIn('"path":"/metadata/uid","value":"owned-uid"', body)
            self.assertIn('"path":"/metadata/resourceVersion","value":"17"', body)
            self.assertIn('"path":"/spec/replicas","value":0', body)

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
