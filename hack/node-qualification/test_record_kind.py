import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from common import load
from record_kind import assemble
from test_contract import ROOT, fixture


class LocalRecordTest(unittest.TestCase):
    def test_record_uses_measured_values_and_does_not_retain_private_input(self):
        p, e = fixture()
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "image").mkdir()
            for path in ("image/kubectl-memlens", "image/producer", "serving-ca.crt",
                         "qualification-values.json", "qualification-audience"):
                (root / path).write_bytes(b"private-example-input")
            (root / "image-id").write_text("sha256:" + "a" * 64)
            (root / "qualification-start").write_text(e["startedAt"])
            documents = {
                "source.json": {"sourceCommit": "a" * 40, "sourceTreeSHA256": "b" * 64, "sourceDirty": True},
                "node.json": {"metadata": {"name": "private-node", "uid": "private-uid"},
                              "status": {"nodeInfo": {"kubeletVersion": "v1.37.0", "kernelVersion": "7.0.12-linuxkit",
                                                      "containerRuntimeVersion": "containerd://2.3.1", "osImage": "Debian Linux",
                                                      "architecture": "arm64"}}},
                "allowed.log": {"observation": {"nodeName": "private-node", "stats": {
                    "provenance": "unknown", "memory": {"usageBytes": 0, "workingSetBytes": 1024},
                    "swap": {"usageBytes": 0}}, "context": {}}},
                "qualification-baseline.json": {"profile": e["profile"], "samples": e["samples"]["baseline"], "rotation": e["rotation"]},
                "qualification-enabled.json": {"profile": e["profile"], "samples": e["samples"]["enabled"], "rotation": e["rotation"]},
                "qualification-lifecycle.json": e["lifecycle"],
            }
            for path, document in documents.items():
                (root / path).write_text(json.dumps(document))
            with patch("record_kind.subprocess.check_output", return_value="c" * 40):
                record = assemble(root, p)
            text = json.dumps(record)
            for private in ("private-node", "private-uid", "private-example-input"):
                self.assertNotIn(private, text)
            self.assertEqual(record["samples"], e["samples"])
            self.assertEqual(record["fields"]["usage"], "available")
            self.assertEqual(record["fields"]["swapUsage"], "available")
            self.assertEqual(record["fields"]["available"], "unreported")
            self.assertFalse(record["cleanup"]["workloadsRemoved"])
            self.assertTrue(record["artefacts"]["sourceDirty"])
            for phase, started, completed in (("baseline", "12:00:00", "12:03:00"), ("enabled", "12:03:00", "12:15:00")):
                window = {"schemaVersion": 2, "phase": phase, "profile": e["profile"],
                          "observation": {"method": "kubernetes-probes-v1", "image": p["workload"]["image"]},
                          "startedAt": "2026-09-11T" + started + "Z", "completedAt": "2026-09-11T" + completed + "Z",
                          "nodes": [{"slot": 0, "samples": e["samples"][phase], "rotation": e["rotation"]}]}
                (root / ("qualification-" + phase + ".json")).write_text(json.dumps(window))
            (root / "qualification-observer-settings.json").write_text('{"privateNodeName":"private-node"}')
            with patch("record_kind.subprocess.check_output", return_value="c" * 40):
                modern = assemble(root, p)
            self.assertEqual(modern["schemaVersion"], 2)
            self.assertEqual(modern["nodes"][0]["samples"], e["samples"])
            self.assertNotIn("samples", modern)
            self.assertNotEqual(modern["artefacts"]["valuesDigest"], record["artefacts"]["valuesDigest"])
            self.assertNotIn("private-node", json.dumps(modern))


if __name__ == "__main__":
    unittest.main()
