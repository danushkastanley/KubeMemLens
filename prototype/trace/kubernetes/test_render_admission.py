import unittest

from admission_resources import deployments, permissions
from render_admission import render


class AdmissionProfileTests(unittest.TestCase):
    def test_node_privileges_are_separate_from_controller(self):
        items = deployments("example.invalid/image@sha256:" + "a" * 64,
                            "node", "uid", "admin", "/kubelet", "b" * 64)
        specs = {item["metadata"]["name"]: item["spec"]["template"]["spec"]
                 for item in items if item["kind"] == "Deployment"}
        node, api = specs["binding-node"], specs["admission-api"]
        self.assertFalse(node["automountServiceAccountToken"])
        self.assertTrue(api["automountServiceAccountToken"])
        self.assertEqual(node["containers"][0]["securityContext"]["capabilities"],
                         {"drop": ["ALL"], "add": ["BPF", "PERFMON"]})
        self.assertEqual(api["containers"][0]["securityContext"]["capabilities"], {"drop": ["ALL"]})
        self.assertFalse(any("hostPath" in volume for volume in api["volumes"]))
        self.assertTrue(all(mount["readOnly"] for mount in node["containers"][0]["volumeMounts"]))
        self.assertFalse(any(node.get(key, False) for key in ("hostPID", "hostIPC", "hostNetwork")))
        for item in items:
            if item["kind"] == "Deployment":
                self.assertEqual(item["spec"]["replicas"], 1)
                self.assertEqual(item["spec"]["strategy"]["type"], "Recreate")

    def test_rbac_grants_no_node_credentials_or_pod_execution(self):
        items = permissions("admin")
        for item in items:
            if item["kind"].endswith("Binding"):
                self.assertEqual(item["subjects"], [{"kind": "ServiceAccount", "namespace": "admin", "name": "admission-api"}])
            for rule in item.get("rules", []):
                self.assertNotIn("*", rule["resources"])
                self.assertNotIn("pods/exec", rule["resources"])
                self.assertNotIn("secrets", rule["resources"])
                self.assertNotIn("delete", rule["verbs"])

    def test_unreviewed_profile_inputs_fail_before_reading_certificates(self):
        defaults = ["example.invalid/image@sha256:" + "a" * 64, "node", "uid", "admin", "/kubelet", "/does-not-exist"]
        for index, value in ((0, "image:latest"), (1, "../node"), (2, "bad uid"),
                             (3, "../tenant"), (4, "/a/b/c/d/e"), (4, "/../kubelet")):
            args = defaults.copy()
            args[index] = value
            with self.assertRaises(ValueError):
                render(*args)
