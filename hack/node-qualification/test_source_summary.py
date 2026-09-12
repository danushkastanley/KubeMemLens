import copy
import json
import unittest

from common import ContractError, privacy
from source_summary import summarise


class SourceSummaryTest(unittest.TestCase):
    def setUp(self):
        self.nodes = ["private-node-a", "private-node-b"]
        self.observations = [{"nodeName": name, "nodeUID": "private-uid", "stats": {
            "provenance": "cadvisor", "memory": {"usageBytes": 0, "workingSetBytes": 1024, "rssBytes": 0},
            "swap": {"usageBytes": 0}, "systemContainers": []}, "context": {"hugepages": []}}
            for name in self.nodes]

    def test_missing_field_on_one_node_is_unreported_for_the_pool(self):
        del self.observations[1]["stats"]["memory"]["rssBytes"]
        result = summarise(self.observations, self.nodes)
        self.assertEqual(result["fields"]["rss"], "unreported")
        for field in ("usage", "swapUsage", "systemContainers", "hugepages"):
            self.assertEqual(result["fields"][field], "available")
        self.assertEqual(result["fields"]["psi"], "unreported")

    def test_mixed_or_unknown_provenance_does_not_become_cadvisor(self):
        for source in ("cri", "unknown", "cadvisor"):
            self.observations[1]["stats"]["provenance"] = source
            self.assertEqual(summarise(self.observations, self.nodes)["provenance"],
                             "cadvisor" if source == "cadvisor" else "unknown")

    def test_summary_discards_raw_values_and_private_identity(self):
        result = summarise(self.observations, self.nodes)
        privacy(result)
        text = json.dumps(result)
        self.assertNotIn("private", text)
        self.assertNotIn("1024", text)
        self.assertEqual(set(result), {"fields", "provenance"})
        self.assertEqual(result, summarise(list(reversed(self.observations)), self.nodes))

    def test_missing_duplicate_or_foreign_diagnostics_are_rejected(self):
        cases = [[], self.observations[:1], [self.observations[0]] * 2,
                 self.observations + [self.observations[0]]]
        foreign = copy.deepcopy(self.observations)
        foreign[1]["nodeName"] = "foreign-node"
        cases.append(foreign)
        for observations in cases:
            with self.assertRaises(ContractError):
                summarise(observations, self.nodes)
        with self.assertRaises(ContractError):
            summarise(self.observations, [self.nodes[0]] * 2)

    def test_unknown_schema_value_is_rejected(self):
        self.observations[0]["stats"]["provenance"] = "guessed"
        with self.assertRaises(ContractError):
            summarise(self.observations, self.nodes)

    def test_null_group_is_unreported_but_invalid_empty_group_is_rejected(self):
        self.observations[0]["stats"]["memory"] = None
        self.assertEqual(summarise(self.observations, self.nodes)["fields"]["usage"], "unreported")
        for invalid in ([], False, 0, ""):
            self.observations[0]["stats"]["memory"] = invalid
            with self.assertRaises(ContractError):
                summarise(self.observations, self.nodes)


if __name__ == "__main__":
    unittest.main()
