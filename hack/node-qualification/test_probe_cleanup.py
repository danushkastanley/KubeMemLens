"""Keep probe cleanup ahead of controller-triggered garbage collection."""

import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from common import ContractError
from owned_resources import OwnedResources, Resource
from provider_probes import run_probes
from test_owned_resources import Kubernetes
from test_provider_probes import probe_child


class ProbeKubernetes(Kubernetes):
    def __init__(self):
        super().__init__()
        self.collecting = set()
        self.output = ""

    def __call__(self, *args, data=None, **kwargs):
        if args[0] == "logs":
            return self.output
        if args[0] == "delete":
            resource = next(r for r in self.objects if r.uri == args[2])
            if resource in self.collecting:
                del self.objects[resource]
                raise ContractError("controller collected the Pod between GET and DELETE")
            if resource.kind == "Job":
                uid = self.objects[resource]["metadata"]["uid"]
                self.collecting.update(r for r, d in self.objects.items()
                                       if any(o["uid"] == uid for o in d["metadata"].get("ownerReferences", [])))
        return super().__call__(*args, data=data)

    def terminal(self, parent, owner, reader, node, timeout):
        case = parent["metadata"]["name"].removeprefix("qualification-").rsplit("-", 1)[0]
        phase = "Succeeded" if case == "allowed" else "Failed"
        child = probe_child(parent, phase)
        child["metadata"]["name"] = parent["metadata"]["name"] + "-pod"
        child = json.loads(self("create", data=json.dumps(child)))
        resource = Resource.from_object(child)
        owner.remember(resource, child["metadata"]["uid"])
        reasons = {"denied": "access-denied", "bad-ca": "untrusted-tls", "wrong-node": "invalid-target"}
        self.output = (json.dumps({"kind": "NodeContextDiagnostic", "schemaVersion": 1, "redacted": True,
                                   "observation": {"nodeName": node, "availability": "available",
                                                   "evidence": {"freshness": "fresh"}}})
                       if case == "allowed" else "node-context read failed: " + reasons[case])
        return resource, child


class ProbeCleanupTest(unittest.TestCase):
    def test_controller_collection_cannot_race_owned_pod_deletion(self):
        config = {"namespace": "fixture", "imageRepository": "registry.example/image",
                  "imageDigest": "sha256:" + "a" * 64, "kubeletAudience": "fixture"}
        k = ProbeKubernetes()
        with tempfile.TemporaryDirectory() as directory:
            owner = OwnedResources(k, Path(directory) / "journal")
            with patch("provider_probes.resources", return_value=[]), \
                    patch("provider_probes.require_no_extra_access"), \
                    patch("provider_probes.terminal_pod", side_effect=k.terminal):
                observations = run_probes(config, [{"name": "first"}, {"name": "second"}], owner, k)
            self.assertEqual([o["nodeName"] for o in observations], ["first", "second"])
            self.assertFalse(k.objects)
            self.assertEqual(len(k.deletes), 16)
            self.assertTrue(all(options["preconditions"]["uid"] for _, options in k.deletes))


if __name__ == "__main__":
    unittest.main()
