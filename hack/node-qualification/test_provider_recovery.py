import copy
import unittest
from contextlib import contextmanager
from types import SimpleNamespace
from unittest.mock import patch

from common import ContractError
from provider_recovery import PoolRecovery


class Runtime:
    def __init__(self, slot):
        self.node = "node-" + str(slot)
        self.node_uid = "uid-" + str(slot)
        self.timestamp = "2026-09-12T01:00:00Z"
        self.freshness, self.generation, self.posts = "fresh", "generation-1", 1
        self.ids = {c: c + "-" + str(slot) for c in ("agent", "collector", "node-context")}

    def api(self, path):
        if path == "/clusterstatus/current":
            return {"store": {"reliability": {"freshNodes": 2}, "nodeContext": {"freshRecords": 2}}}
        if path.endswith("history?limit=1"):
            return {"generation": self.generation}
        return {"record": {"freshness": self.freshness, "nodeUID": self.node_uid,
                           "lastGood": {"nodeUID": self.node_uid, "reportedAt": self.timestamp}}}

    def containers(self):
        return {c: {"id": identity} for c, identity in self.ids.items()}

    def component_metrics(self, container, port):
        return {'kubememlens_agent_snapshot_posts_total{result="success"}': self.posts}


class RecoveryTest(unittest.TestCase):
    def setUp(self):
        self.runtimes = [Runtime(0), Runtime(1)]
        bundle = SimpleNamespace(profile={"workload": {"linuxNodes": 2, "image": "fixture"},
                                          "budgets": {"recoverySeconds": 120}}, configuration={"namespace": "fixture"})
        self.execution = SimpleNamespace(bundle=bundle, runtimes=self.runtimes, windows={"baseline": {}, "enabled": {}},
                                         ownership=object(), verify_binding=lambda: None)
        self.recovery = PoolRecovery(self.execution)
        self.recovery.wait = lambda predicate, started: predicate() or predicate()

    def advance(self):
        for r in self.runtimes:
            r.timestamp = "2026-09-12T01:00:30Z"

    def test_freshness_requires_new_reports_from_each_bound_node(self):
        before = self.recovery.capture()
        self.runtimes[0].timestamp = "2026-09-12T01:00:30Z"
        self.assertFalse(self.recovery.fresh(before))
        self.advance()
        self.assertTrue(self.recovery.fresh(before))
        self.runtimes[1].node_uid = "replacement"
        self.assertFalse(self.recovery.fresh(before))

    def test_incomplete_windows_or_pool_cannot_start_recovery(self):
        for windows, runtimes in (({"baseline": {}}, self.runtimes), (self.execution.windows, self.runtimes[:1])):
            execution = copy.copy(self.execution)
            execution.windows, execution.runtimes = windows, runtimes
            with self.assertRaises(ContractError):
                PoolRecovery(execution)

    def test_source_loss_requires_stable_stale_evidence_on_both_nodes(self):
        for stale_second in (False, True):
            with self.subTest(stale_second=stale_second):
                self.setUp()
                @contextmanager
                def suspended(*_):
                    self.runtimes[0].freshness = "stale"
                    self.runtimes[1].freshness = "stale" if stale_second else "fresh"
                    try:
                        yield
                    finally:
                        for r in self.runtimes:
                            r.freshness = "fresh"
                        self.advance()
                with patch("provider_recovery.suspended_producer_access", suspended):
                    result = self.recovery.source_loss()
                self.assertEqual(result["state"], "passed" if stale_second else "failed")
                self.assertEqual(result["staleRetained"], stale_second)

    def restart(self, component, replace_second=True, generation_change=False, posts=1):
        def change(*_):
            self.advance()
            for slot, r in enumerate(self.runtimes):
                if slot == 0 or replace_second:
                    r.ids[component] += "-new"
                if generation_change:
                    r.generation = "generation-2"
                r.posts = posts
        with patch("provider_recovery.restart_workload", change), patch("provider_recovery.attach") as attach:
            result = self.recovery.restart(component)
            return result, attach.call_count

    def test_agent_restart_needs_both_new_containers_successful_posts_and_same_collector(self):
        for replaced, generation, posts, expected in ((False, False, 1, False), (True, True, 1, False),
                                                      (True, False, 0, False), (True, False, 1, True)):
            with self.subTest(replaced=replaced, generation=generation, posts=posts):
                self.setUp()
                result, attachments = self.restart("agent", replaced, generation, posts)
                self.assertEqual(result["state"], "passed" if expected else "failed")
                self.assertEqual(attachments, 2 if replaced else 0)

    def test_collector_restart_requires_changed_generation_for_both_nodes(self):
        for generation in (False, True):
            with self.subTest(generation=generation):
                self.setUp()
                result, attachments = self.restart("collector", generation_change=generation)
                self.assertEqual(result["state"], "passed" if generation else "failed")
                self.assertEqual(attachments, 0)


if __name__ == "__main__":
    unittest.main()
