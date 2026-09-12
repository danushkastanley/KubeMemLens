import copy
import unittest

from cilium_kind import IMAGES, verify_render
from common import ContractError


class CiliumRenderTest(unittest.TestCase):
    def setUp(self):
        self.documents = [{"kind": "ConfigMap", "metadata": {"name": "cilium-config", "namespace": "kube-system"},
                           "data": {"policy-cidr-match-mode": "nodes", "enable-policy": "default"}}]
        self.documents += [{"kind": "DaemonSet", "metadata": {"name": "fixture-" + str(slot), "namespace": "kube-system"},
                            "spec": {"template": {"spec": {"containers": [{"image": image}]}}}}
                           for slot, image in enumerate(sorted(IMAGES))]

    def test_expected_images_and_settings_pass(self):
        verify_render(self.documents)

    def test_changed_policy_or_image_is_rejected(self):
        changed_image = copy.deepcopy(self.documents)
        changed_image[1]["spec"]["template"]["spec"]["containers"][0]["image"] = "quay.io/cilium/cilium:latest"
        changed_policy = copy.deepcopy(self.documents)
        changed_policy[0]["data"]["enable-policy"] = "never"
        changed_cidr = copy.deepcopy(self.documents)
        changed_cidr[0]["data"]["policy-cidr-match-mode"] = "pods"
        for documents in (changed_image, changed_policy, changed_cidr):
            with self.assertRaises(ContractError):
                verify_render(documents)

    def test_missing_image_or_foreign_namespace_is_rejected(self):
        foreign = copy.deepcopy(self.documents)
        foreign[1]["metadata"]["namespace"] = "unrelated"
        for documents in (self.documents[:2], foreign):
            with self.assertRaises(ContractError):
                verify_render(documents)


if __name__ == "__main__":
    unittest.main()
