"""Keep local coordinator probes bound to the chart's observed host routes."""

import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from local_execution import prepare
from provider_probes import resources


class LocalPlanTest(unittest.TestCase):
    def test_local_preparation_supplies_probe_routes_before_chart_install(self):
        info = {"kubeletVersion": "v1.37.0", "kernelVersion": "6.1.0",
                "containerRuntimeVersion": "containerd://2.2.0", "osImage": "Debian GNU/Linux 12",
                "architecture": "amd64"}
        nodes = [{"metadata": {"name": name, "uid": name + "-uid"}, "spec": {},
                  "status": {"nodeInfo": info, "addresses": [{"type": "InternalIP", "address": address}]}}
                 for name, address in (("control", "172.18.0.2"), ("worker", "172.18.0.3"))]

        def read(*args):
            if args[:2] == ("get", "nodes"):
                return json.dumps({"items": nodes})
            if args[:2] == ("get", "--raw"):
                return json.dumps({"issuer": "https://kubernetes.default.svc"})
            self.assertEqual(args[:3], ("get", "service", "kubernetes"))
            return json.dumps({"spec": {"clusterIP": "10.96.0.1"}})

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "host-cli").write_bytes(b"test executable identity")
            (root / "serving-ca.crt").write_text("test public certificate")
            (root / "image-proof.json").write_text(json.dumps({"scope": "fixture"}))
            args = SimpleNamespace(profile=str(Path(__file__).parent / "profiles/kind-137-execution.json"),
                                   kubeconfig=str(root / "kubeconfig"), context="kind-owned",
                                   private=str(root), output=str(root / "result.json"),
                                   image_repository="localhost/test", image_digest="sha256:" + "a" * 64)
            with patch("local_execution.verify_kind_target"), \
                    patch("local_execution.KubernetesCommands", return_value=read), \
                    patch("local_execution.Execution") as execution:
                prepare(args)
            config = execution.call_args.args[0].configuration
            policy = next(m for m in resources(config) if m["kind"] == "NetworkPolicy")
            self.assertEqual(config["apiServerCIDRs"], ["10.96.0.1/32", "172.18.0.2/32", "172.18.0.3/32"])
            self.assertEqual(config["nodeCIDRs"], ["172.18.0.2/32", "172.18.0.3/32"])
            for phase in ("baseline", "enabled"):
                values = json.loads((root / "local-proposal" / (phase + "-values.json")).read_text())["nodeContext"]
                for index, key in enumerate(("apiServerCIDRs", "nodeCIDRs")):
                    self.assertEqual([peer["ipBlock"]["cidr"] for peer in policy["spec"]["egress"][index]["to"]], values[key])


if __name__ == "__main__":
    unittest.main()
