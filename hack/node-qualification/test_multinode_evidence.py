import copy
import unittest

from common import ContractError, digest, load, privacy
from evaluate import evaluate
from evidence import validate_evidence
from test_contract import ROOT, fixture, seal, version_two


class PerNodeEvidenceTest(unittest.TestCase):
    def fixture(self):
        return fixture(load(ROOT / "profiles/gke-standard.json"))

    def test_each_node_passes_without_self_qualification(self):
        p, e = self.fixture()
        result = evaluate(p, e)
        self.assertEqual(result["outcome"], "pass")
        self.assertFalse(result["qualified"])
        self.assertEqual(result["observation"], e["observation"])
        self.assertEqual(result["nodeChecks"][1]["slot"], 1)
        self.assertIn("projected-rotation", {c["id"] for c in result["nodeChecks"][1]["checks"]})

    def test_other_node_cannot_hide_cpu_or_memory_regression(self):
        for key, before, after in (("kubeletCPUMilli", (300, 10), (300, 200)),
                                   ("kubeletMemoryBytes", (300 << 20, 10 << 20), (300 << 20, 200 << 20))):
            with self.subTest(metric=key):
                p, e = self.fixture()
                for slot, node in enumerate(e["nodes"]):
                    for sample in node["samples"]["baseline"]:
                        sample[key] = before[slot]
                    for sample in node["samples"]["enabled"]:
                        sample[key] = after[slot]
                self.assertEqual(max(after) - max(before), 0)
                seal(e)
                result = evaluate(p, e)
                self.assertEqual(result["outcome"], "fail")
                failures = [c["id"] for c in result["nodeChecks"][1]["checks"] if not c["passed"]]
                self.assertTrue(any(c.startswith("kubelet-") for c in failures))

    def test_every_node_requires_counters_coverage_and_rotation(self):
        changes = {
            "reset": lambda n: n["samples"]["enabled"][20].update(sourceReads=0),
            "failure": lambda n: n["samples"]["enabled"][-1].update(sourceFailures=1),
            "no rotation": lambda n: n["rotation"].update(observed=False),
            "short window": lambda n: n["samples"].update(enabled=n["samples"]["enabled"][:10]),
            "missing measurement": lambda n: n["samples"]["enabled"][5].update(producerCPUMilli=None),
        }
        for name, change in changes.items():
            with self.subTest(scenario=name):
                p, e = self.fixture(); change(e["nodes"][1]); seal(e)
                self.assertEqual(evaluate(p, e)["outcome"], "fail")

    def test_missing_duplicate_reordered_or_identified_slots_are_rejected(self):
        for scenario in ("missing", "duplicate", "reordered", "identity", "method", "image"):
            with self.subTest(scenario=scenario):
                p, e = self.fixture()
                if scenario == "missing":
                    e["nodes"].pop()
                elif scenario == "duplicate":
                    e["nodes"][1]["slot"] = 0
                elif scenario == "reordered":
                    e["nodes"].reverse()
                elif scenario == "identity":
                    e["nodes"][0]["nodeName"] = "private"
                else:
                    e["observation"][scenario] = "unapproved"
                seal(e)
                with self.assertRaises(ContractError):
                    validate_evidence(p, e)

    def test_legacy_provider_aggregate_cannot_pass_new_qualification(self):
        p, e = self.fixture()
        node = e.pop("nodes")[0]
        e.pop("observation")
        e.update(schemaVersion=1, samples=node["samples"], rotation=node["rotation"])
        for sample in e["samples"]["enabled"]:
            sample["producerReplicas"] = 2
        seal(e)
        validate_evidence(p, e)
        result = evaluate(p, e)
        failures = [c["id"] for c in result["checks"] if not c["passed"]]
        self.assertEqual(failures, ["per-node-measurements"])

    def test_local_v2_uses_same_cost_rules_as_legacy(self):
        p, legacy = fixture()
        modern = version_two(p, copy.deepcopy(legacy))
        old, new = evaluate(p, legacy), evaluate(p, modern)
        self.assertEqual(old["outcome"], new["outcome"])
        self.assertEqual({c["id"]: c for c in old["checks"]},
                         {c["id"]: c for c in new["checks"] + new["nodeChecks"][0]["checks"]})

    def test_largest_valid_node_count_keeps_every_result_array_bounded(self):
        p = load(ROOT / "profiles/gke-standard.json")
        p["workload"]["linuxNodes"] = 10
        p["profileDigest"] = digest(p, "profileDigest")
        p, e = fixture(p)
        result = evaluate(p, e)
        self.assertEqual(result["outcome"], "pass")
        self.assertEqual(len(result["nodeChecks"]), 10)
        privacy(result)


if __name__ == "__main__":
    unittest.main()
