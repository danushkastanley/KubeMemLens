import copy
import unittest

from admission_resources import deployments
from render_filecache import configure


class FileCacheDeploymentTests(unittest.TestCase):
    def test_policy_mount_and_node_profile_preserve_privilege_boundaries(self):
        for paths in (False, True):
            items = deployments("example.invalid/image@sha256:" + "a" * 64,
                                "node", "uid", "admin", "/kubelet", "b" * 64)
            configured = configure(items, "accepted-filecache", paths)
            self.assertFalse(any(item["kind"] == "ConfigMap" for item in configured))
            for item in configured:
                if item["kind"] != "Deployment":
                    continue
                name = item["metadata"]["name"]
                pod = item["spec"]["template"]["spec"]
                container = pod["containers"][0]
                self.assertIn("/acceptance/policy.json", container["args"])
                self.assertTrue(all(m["readOnly"] for m in container["volumeMounts"]))
                self.assertEqual(pod["volumes"][-1]["configMap"], {
                    "name": "accepted-filecache", "defaultMode": 0o444,
                    "items": [{"key": "policy.json", "path": "policy.json"}],
                })
                self.assertFalse(any(pod.get(k, False) for k in ("hostPID", "hostIPC", "hostNetwork")))
                self.assertFalse(container["securityContext"]["allowPrivilegeEscalation"])
                if name == "binding-node":
                    self.assertFalse(pod["automountServiceAccountToken"])
                    self.assertEqual(container["securityContext"]["capabilities"], {"drop": ["ALL"], "add": ["BPF", "PERFMON"]})
                    self.assertEqual(pod["securityContext"]["seccompProfile"]["localhostProfile"], "kube-memlens-trace/filecache-node.json")
                    self.assertNotIn("--allow-confirmed-paths", container["args"])
                else:
                    self.assertEqual(container["securityContext"]["capabilities"], {"drop": ["ALL"]})
                    self.assertEqual("--allow-confirmed-paths" in container["args"], paths)
                    self.assertFalse(any("hostPath" in v for v in pod["volumes"]))

    def test_invalid_policy_name_and_missing_deployments_fail(self):
        for name in ("", "../policy", "policy/file", "x" * 64):
            with self.assertRaises(ValueError):
                configure([], name)
        with self.assertRaises(ValueError):
            configure([], "accepted-filecache")

    def test_two_trace_limit_is_installation_owned_and_preserves_resources(self):
        for maximum in (1, 2):
            items = deployments("example.invalid/image@sha256:" + "a" * 64,
                                "node", "uid", "admin", "/kubelet", "b" * 64)
            resources = {item["metadata"]["name"]: copy.deepcopy(item["spec"]["template"]["spec"]["containers"][0]["resources"])
                         for item in items if item["kind"] == "Deployment"}
            for item in configure(items, "accepted", max_node_traces=maximum):
                if item["kind"] != "Deployment":
                    continue
                container = item["spec"]["template"]["spec"]["containers"][0]
                name = item["metadata"]["name"]
                self.assertEqual(container["resources"], resources[name])
                self.assertEqual("--max-node-traces" in container["args"],
                                 maximum == 2 and name == "admission-api")
        for invalid in (0, 3, True, 2.0):
            with self.assertRaises(ValueError):
                configure([], "accepted", max_node_traces=invalid)


if __name__ == "__main__":
    unittest.main()
