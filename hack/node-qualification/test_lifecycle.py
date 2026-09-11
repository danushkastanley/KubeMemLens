import json
import unittest

from lifecycle import Lifecycle


class ReplacementRuntime:
    node = "fixture"

    def __init__(self, replace=True, refresh=True, reset=False):
        self.replace, self.refresh, self.reset = replace, refresh, reset
        self.node_uid = "old"
        self.fresh = False

    def host(self, *args):
        if args == ("systemctl", "start", "kubelet") and self.replace:
            self.node_uid = "new"

    def k(self, *args):
        if args[:2] == ("get", "node"):
            return json.dumps({"metadata": {"uid": self.node_uid}})
        if args[:2] == ("rollout", "restart"):
            self.fresh = self.refresh
        return ""

    def api(self, path):
        if path.endswith("history?limit=1"):
            return {"generation": "new" if self.fresh and self.reset else "same"}
        if path == "/clusterstatus/current":
            return {"store": {"reliability": {"freshNodes": 1}, "nodeContext": {"freshRecords": 1}}}
        uid = self.node_uid if self.fresh else "old"
        sample = {"nodeUID": uid, "reportedAt": "2026-09-12T12:00:30Z" if self.fresh else "2026-09-12T12:00:00Z"}
        return {"record": {"nodeUID": uid, "freshness": "fresh", "lastGood": sample, "report": sample}}


class LifecycleTest(unittest.TestCase):
    def test_registration_requires_new_identity_new_source_and_same_collector(self):
        scenarios = [(False, True, False), (True, False, False), (True, True, True), (True, True, False)]
        for replace, refresh, reset in scenarios:
            with self.subTest(replace=replace, refresh=refresh, reset=reset):
                lifecycle = Lifecycle(ReplacementRuntime(replace, refresh, reset), 120)
                lifecycle.wait = lambda predicate, started: predicate()
                result = lifecycle.replace_identity()
                expected = replace and refresh and not reset
                self.assertEqual(result["state"], "passed" if expected else "failed")
                self.assertEqual(result["identityVerified"], expected)
                self.assertEqual(result["freshEvidence"], expected)


if __name__ == "__main__":
    unittest.main()
