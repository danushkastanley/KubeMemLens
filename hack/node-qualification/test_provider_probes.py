import json
import unittest

from common import ContractError
from kubernetes_commands import KubernetesCommands
from provider_probes import identities, pod


class ProbeContractTest(unittest.TestCase):
    def test_api_and_kubelet_tokens_have_separate_audiences_and_mounts(self):
        config = {"namespace": "fixture", "imageRepository": "registry.example/image", "imageDigest": "sha256:" + "a" * 64,
                  "kubeletAudience": "kubelet-fixture"}
        spec = pod(config, "assigned", 0, "wrong-node", "different")["spec"]
        self.assertFalse(spec["automountServiceAccountToken"])
        self.assertEqual(spec["nodeName"], "assigned")
        self.assertIn("--node-name=different", spec["containers"][0]["args"])
        volumes = {v["name"]: v for v in spec["volumes"]}
        api_token = volumes["api"]["projected"]["sources"][0]["serviceAccountToken"]
        kubelet_token = volumes["identity"]["projected"]["sources"][0]["serviceAccountToken"]
        self.assertNotIn("audience", api_token)
        self.assertEqual(kubelet_token["audience"], "kubelet-fixture")
        self.assertTrue(all(m["readOnly"] for m in spec["containers"][0]["volumeMounts"]))
        self.assertFalse(any("hostPath" in v for v in spec["volumes"]))

    def test_probe_rbac_has_no_proxy_or_secret_access(self):
        roles = [m for m in identities("fixture") if m["kind"] == "ClusterRole"]
        self.assertEqual({r for role in roles for rule in role["rules"] for r in rule["resources"]}, {"nodes", "nodes/stats"})
        self.assertTrue(all(rule["verbs"] == ["get"] for role in roles for rule in role["rules"]))

    def test_authorisation_decision_uses_typed_review_and_exact_target(self):
        calls = []
        def run(args, **kwargs):
            calls.append((args, kwargs))
            return json.dumps({"status": {"allowed": False}})
        k = KubernetesCommands("/private/config", "selected", run)
        self.assertFalse(k.allowed("system:serviceaccount:fixture:probe", "get", "nodes", "proxy", name="bound"))
        args, kwargs = calls[0]
        self.assertEqual(args[:5], ["kubectl", "--kubeconfig", "/private/config", "--context", "selected"])
        review = json.loads(kwargs["data"])
        self.assertEqual(review["spec"]["resourceAttributes"]["name"], "bound")
        self.assertIn("--as", args)
        self.assertEqual(args.count("--as-group"), 3)
        self.assertIn("system:serviceaccounts:fixture", args)

    def test_authorisation_error_does_not_count_as_denied(self):
        for status in ({}, {"allowed": False, "evaluationError": "unavailable"}, {"allowed": "false"}):
            k = KubernetesCommands("/private", "selected", lambda *args, **kwargs: json.dumps({"status": status}))
            with self.assertRaises(ContractError):
                k.allowed("system:serviceaccount:fixture:probe", "get", "nodes", "proxy")


if __name__ == "__main__":
    unittest.main()
