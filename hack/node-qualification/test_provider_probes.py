import json
import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch

from common import ContractError
from kubernetes_commands import KubernetesCommands
from provider_probes import identities, job, run_probes, terminal_pod
from owned_resources import Resource


class ProbeContractTest(unittest.TestCase):
    def test_api_and_kubelet_tokens_have_separate_audiences_and_mounts(self):
        config = {"namespace": "fixture", "imageRepository": "registry.example/image", "imageDigest": "sha256:" + "a" * 64,
                  "kubeletAudience": "kubelet-fixture"}
        manifest = job(config, "assigned", 0, "wrong-node", "different", 90)
        self.assertEqual(manifest['kind'], 'Job')
        self.assertEqual((manifest['spec']['backoffLimit'], manifest['spec']['activeDeadlineSeconds']), (0, 90))
        spec = manifest['spec']['template']['spec']
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

    def test_failed_allowed_probe_retains_only_the_fixed_producer_reason(self):
        config = {"namespace": "fixture", "imageRepository": "registry.example/image", "imageDigest": "sha256:" + "a" * 64,
                  "kubeletAudience": "kubelet-fixture"}
        objects, owned = {}, {}
        def create(document):
            document['metadata']['uid'] = document['metadata']['name'] + '-uid'
            objects[Resource.from_object(document)] = document
            return document
        owner = SimpleNamespace(require_absent=Mock(), create=create,
                                verify=lambda resource: objects[resource], owned=owned,
                                remember=lambda resource, uid: owned.update({resource: uid}))
        class Reader:
            def allowed(self, *args):
                return False
            def __call__(self, *args, **kwargs):
                if args[0] == 'logs':
                    return self.output
                parent = next(d for d in objects.values() if d['kind'] == 'Job')
                child = probe_child(parent, 'Failed')
                objects[Resource.from_object(child)] = child
                return json.dumps({'items': [child]})
        reader = Reader()
        reader.output = "node-context read failed: access-denied"
        with self.assertRaisesRegex(ContractError, "production stats probe failed: access-denied"):
            run_probes(config, [{"name": "first"}, {"name": "second"}], owner, reader)
        reader.output = "node-context read failed: private-credential-data"
        with self.assertRaises(ContractError) as caught:
            run_probes(config, [{"name": "first"}, {"name": "second"}], owner, reader)
        self.assertNotIn("private-credential-data", str(caught.exception))

    def test_replaced_generated_pod_is_rejected_before_log_read(self):
        parent = {'apiVersion': 'batch/v1', 'kind': 'Job',
                  'metadata': {'name': 'probe', 'namespace': 'fixture', 'uid': 'job-uid'},
                  'spec': {'template': {'spec': {'nodeName': 'first'}}}}
        first = probe_child(parent, 'Pending')
        second = probe_child(parent, 'Succeeded')
        second['metadata']['uid'] = 'replacement-pod'
        owned = {}
        owner = SimpleNamespace(verify=lambda resource: parent, owned=owned,
                                remember=lambda resource, uid: owned.update({resource: uid}))
        reader = Mock(side_effect=[json.dumps({'items': [first]}), json.dumps({'items': [second]})])
        with patch('provider_probes.time.sleep'), self.assertRaisesRegex(ContractError, 'Pod was replaced'):
            terminal_pod(parent, owner, reader, 'first', 90)
        for call in reader.call_args_list:
            self.assertIn('batch.kubernetes.io/controller-uid=job-uid', call.args)

    def test_probe_job_cannot_exceed_existing_deadline(self):
        for timeout in (0, 91, True):
            with self.subTest(timeout=timeout), self.assertRaises(ContractError):
                job({}, 'node', 0, 'allowed', 'node', timeout)

    def test_authorisation_error_does_not_count_as_denied(self):
        for status in ({}, {"allowed": False, "evaluationError": "unavailable"}, {"allowed": "false"}):
            k = KubernetesCommands("/private", "selected", lambda *args, **kwargs: json.dumps({"status": status}))
            with self.assertRaises(ContractError):
                k.allowed("system:serviceaccount:fixture:probe", "get", "nodes", "proxy")


def probe_child(parent, phase):
    meta = parent['metadata']
    return {'apiVersion': 'v1', 'kind': 'Pod',
            'metadata': {'name': 'generated-probe', 'namespace': meta['namespace'], 'uid': 'pod-uid',
                         'ownerReferences': [{'apiVersion': 'batch/v1', 'kind': 'Job', 'controller': True,
                                              'name': meta['name'], 'uid': meta['uid']}]},
            'spec': {'nodeName': parent['spec']['template']['spec']['nodeName']},
            'status': {'phase': phase}}


if __name__ == "__main__":
    unittest.main()
