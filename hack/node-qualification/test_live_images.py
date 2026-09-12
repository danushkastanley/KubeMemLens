import copy
import json
import unittest
from types import SimpleNamespace

from common import ContractError
from live_images import image_id, verify, verify_container
from owned_resources import Resource


class LiveImageTest(unittest.TestCase):
    def setUp(self):
        self.digest = "sha256:" + "a" * 64
        self.reference = "registry.example/image@" + self.digest
        self.expected = {"name": "node-context", "image": self.reference, "command": ["/memlens-node-context"], "args": ["--publish"]}
        self.current = {"podUID": "owned-pod", "id": "b" * 64}
        self.pod = {"metadata": {"uid": "owned-pod"}, "spec": {"containers": [copy.deepcopy(self.expected)]},
                    "status": {"containerStatuses": [{"name": "node-context", "ready": True, "state": {"running": {}},
                                                       "containerID": "containerd://" + "b" * 64, "imageID": self.reference}]}}

    def check(self):
        verify_container(self.pod, "node-context", self.current, self.expected, self.reference, {self.digest})

    def test_verified_runtime_and_common_cri_digest_encodings_pass(self):
        for value in (self.digest, self.reference, "docker-pullable://" + self.reference, "containerd://" + self.digest):
            self.assertEqual(image_id(value), self.digest)
            self.pod["status"]["containerStatuses"][0]["imageID"] = value
            self.check()

    def test_tag_or_different_digest_does_not_prove_runtime_identity(self):
        for value in ("registry.example/image:latest", "sha256:" + "c" * 64):
            self.pod["status"]["containerStatuses"][0]["imageID"] = value
            with self.assertRaises(ContractError):
                self.check()

    def test_changed_command_replaced_pod_or_shadowed_binary_is_rejected(self):
        original = copy.deepcopy(self.pod)
        for change in (
            lambda p: p["metadata"].update(uid="replacement"),
            lambda p: p["spec"]["containers"][0].update(command=["/different"]),
            lambda p: p["spec"]["containers"][0].update(volumeMounts=[{"mountPath": "/memlens-node-context"}]),
            lambda p: p["spec"]["containers"][0].update(volumeMounts=[{"mountPath": "//"}]),
            lambda p: p["status"]["containerStatuses"][0].update(containerID="containerd://" + "d" * 64),
        ):
            self.pod = copy.deepcopy(original); change(self.pod)
            with self.assertRaises(ContractError):
                self.check()

    def test_baseline_collector_uses_baseline_arguments(self):
        desired, pods, current = {"baseline": {}, "enabled": {}}, {}, {}
        for component in ("agent", "collector"):
            container = {"name": component, "image": self.reference, "command": ["/memlens-" + component], "args": []}
            resource = Resource("apps/v1", "Deployment" if component == "collector" else "DaemonSet", "kube-memlens-" + component, "fixture")
            for phase in desired:
                expected = copy.deepcopy(container)
                if phase == "enabled" and component == "collector":
                    expected["args"] = ["--node-context-username=fixture"]
                desired[phase][resource] = {"spec": {"template": {"spec": {"containers": [expected]}}}}
            current[component] = {"pod": component, "podUID": component, "id": "b" * 64}
            pods[component] = {"metadata": {"uid": component}, "spec": {"containers": [container]},
                               "status": {"containerStatuses": [{"name": component, "ready": True, "state": {"running": {}},
                                                                 "containerID": "containerd://" + "b" * 64, "imageID": self.reference}]}}
        runtime = SimpleNamespace(namespace="fixture", containers=lambda: current, k=lambda *args: json.dumps(pods[args[2]]))
        execution = SimpleNamespace(bundle=SimpleNamespace(configuration={"namespace": "fixture", "imageRepository": "registry.example/image", "imageDigest": self.digest}),
                                    binding={"runtime": {"architecture": "arm64"}}, verify_binding=lambda: None,
                                    ownership=SimpleNamespace(verify=lambda _: None),
                                    installer=SimpleNamespace(namespace=Resource("v1", "Namespace", "fixture"), desired=desired), runtimes=[runtime])
        proof = {key: self.digest for key in ("imageDigest", "archiveIndexDigest", "platformManifestDigest", "imageConfigDigest")}
        proof["architecture"] = "arm64"
        self.assertEqual(verify(execution, proof, "baseline")["checkedPods"], 2)


if __name__ == "__main__":
    unittest.main()
