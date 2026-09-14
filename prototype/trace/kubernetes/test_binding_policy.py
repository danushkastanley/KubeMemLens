import json
import unittest
from pathlib import Path


class BindingPolicyTests(unittest.TestCase):
    def test_node_policy_retains_non_attaching_bpf_and_tcp_only(self):
        policy = json.loads((Path(__file__).parent.parent / "seccomp/binding-node.json").read_text())
        self.assertEqual(policy["defaultAction"], "SCMP_ACT_ERRNO")
        forbidden = {"mount", "umount2", "setns", "unshare", "ptrace", "perf_event_open", "open_by_handle_at"}
        commands, families = set(), set()
        for rule in policy["syscalls"]:
            if rule["action"] != "SCMP_ACT_ALLOW":
                continue
            self.assertFalse(set(rule["names"]) & forbidden)
            if "bpf" in rule["names"]:
                self.assertEqual(rule["names"], ["bpf"])
                self.assertEqual(len(rule["args"]), 1)
                arg = rule["args"][0]
                self.assertEqual((arg["index"], arg["op"]), (0, "SCMP_CMP_EQ"))
                commands.add(arg["value"])
            if "socket" in rule["names"]:
                self.assertEqual(rule["names"], ["socket"])
                self.assertEqual(len(rule["args"]), 2)
                family, kind = rule["args"]
                self.assertEqual((family["index"], family["op"]), (0, "SCMP_CMP_EQ"))
                families.add(family["value"])
                self.assertEqual(kind, {"index": 1, "value": 15, "valueTwo": 1, "op": "SCMP_CMP_MASKED_EQ"})
        self.assertEqual(commands, {0, 5, 15})
        self.assertEqual(families, {2, 10})
