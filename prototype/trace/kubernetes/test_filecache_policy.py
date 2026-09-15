import json
import unittest
from pathlib import Path


class FileCachePolicyTests(unittest.TestCase):
    def test_incident_profile_extends_only_reviewed_operations(self):
        folder = Path(__file__).parent.parent / "seccomp"
        baseline = json.loads((folder / "binding-node.json").read_text())
        candidate = json.loads((folder / "filecache-node.json").read_text())
        self.assertEqual(candidate["defaultAction"], "SCMP_ACT_ERRNO")
        self.assertEqual(candidate["defaultErrnoRet"], 1)
        general = candidate["syscalls"][0]
        self.assertEqual(set(general["names"]) - set(baseline["syscalls"][0]["names"]), {"pread64"})
        self.assertEqual(set(baseline["syscalls"][0]["names"]) - set(general["names"]), {"clone"})
        self.assertNotIn("args", general)
        self.assertEqual(general["action"], "SCMP_ACT_ALLOW")
        for rule in baseline["syscalls"][1:]:
            self.assertIn(rule, candidate["syscalls"])
        extra = candidate["syscalls"][len(baseline["syscalls"]):]
        self.assertEqual(len(extra), 14)
        expected = {
            "bpf": {(0, value) for value in (1, 2, 18, 22, 28)},
            "memfd_create": {(1, 11), (1, 19)},
            "fchmod": {(1, 0o400), (1, 0o500)},
            "clone": {(0, 0x50f00), (0, 0x4111), (0, 0x5111)},
        }
        for syscall, arguments in expected.items():
            rules = [rule for rule in extra if rule["names"] == [syscall]]
            self.assertEqual(len(rules), len(arguments))
            self.assertEqual({(rule["args"][0]["index"], rule["args"][0]["value"]) for rule in rules}, arguments)
            for rule in rules:
                self.assertEqual(rule["action"], "SCMP_ACT_ALLOW")
                self.assertEqual(len(rule["args"]), 1)
                self.assertEqual(rule["args"][0]["op"], "SCMP_CMP_EQ")
        for syscall, arguments in {
            "seccomp": [(0, 1), (1, 1)],
            "perf_event_open": [(1, 2**64-1), (2, 0), (3, 2**64-1), (4, 8)],
        }.items():
            rules = [rule for rule in extra if rule["names"] == [syscall]]
            self.assertEqual(len(rules), 1)
            self.assertEqual(rules[0]["action"], "SCMP_ACT_ALLOW")
            self.assertEqual(rules[0]["args"], [{"index": i, "value": v, "op": "SCMP_CMP_EQ"} for i, v in arguments])

    def test_global_handles_and_namespace_changes_remain_denied(self):
        policy = json.loads((Path(__file__).parent.parent / "seccomp/filecache-node.json").read_text())
        forbidden = {"mount", "umount2", "setns", "unshare", "ptrace", "open_by_handle_at", "socketpair", "connect"}
        commands = set()
        for rule in policy["syscalls"]:
            if rule["action"] != "SCMP_ACT_ALLOW":
                continue
            self.assertFalse(set(rule["names"]) & forbidden)
            if "bpf" in rule["names"]:
                self.assertEqual(rule["names"], ["bpf"])
                self.assertEqual(len(rule["args"]), 1)
                commands.add(rule["args"][0]["value"])
        self.assertEqual(commands, {0, 1, 2, 5, 15, 18, 22, 28})


if __name__ == "__main__":
    unittest.main()
