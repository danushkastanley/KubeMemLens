"""Observe source loss and workload restarts across every bound qualification Node."""

import time

from common import ContractError, require
from install_observers import attach
from owned_mutations import restart_workload, suspended_producer_access


class PoolRecovery:
    def __init__(self, execution):
        require(set(execution.windows) == {"baseline", "enabled"}, "recovery requires completed fixed measurements")
        self.e = execution
        self.runtimes = execution.runtimes
        require(len(self.runtimes) == execution.bundle.profile["workload"]["linuxNodes"], "recovery pool is incomplete")
        self.budget = execution.bundle.profile["budgets"]["recoverySeconds"]

    def records(self):
        return [r.api("/nodecontexts/" + r.node)["record"] for r in self.runtimes]

    def generations(self):
        return [r.api("/nodecontexts/" + r.node + "/history?limit=1")["generation"] for r in self.runtimes]

    def identities(self, component):
        return [r.containers()[component]["id"] for r in self.runtimes]

    def capture(self):
        self.e.verify_binding()
        records = self.records()
        require(all(record["freshness"] == "fresh" and record.get("lastGood") and
                    record["nodeUID"] == runtime.node_uid for runtime, record in zip(self.runtimes, records)),
                "recovery must start with fresh evidence for every bound Node")
        return records

    def wait(self, predicate, started):
        while time.monotonic() - started < self.budget:
            try:
                if predicate():
                    return True
            except (ContractError, KeyError, ValueError):
                # Missing observations and API interruption are expected during
                # recovery; success still requires all identity/freshness checks.
                pass
            time.sleep(2)
        return False

    def fresh(self, before):
        self.e.verify_binding()
        records = self.records()
        for runtime, old, current in zip(self.runtimes, before, records):
            if not (current["freshness"] == "fresh" and current["nodeUID"] == old["nodeUID"] == runtime.node_uid
                    and current.get("lastGood", {}).get("nodeUID") == runtime.node_uid
                    and current["lastGood"].get("reportedAt", "") > old["lastGood"]["reportedAt"]):
                return False
        status = self.runtimes[0].api("/clusterstatus/current")["store"]
        return (status["reliability"]["freshNodes"] == len(self.runtimes)
                and status.get("nodeContext", {}).get("freshRecords") == len(self.runtimes))

    def result(self, started, passed, stale=False):
        elapsed = time.monotonic() - started
        passed = passed and elapsed <= self.budget
        return {"state": "passed" if passed else "failed", "elapsedSeconds": elapsed,
                "identityVerified": passed, "freshEvidence": passed, "staleRetained": stale}

    def source_loss(self):
        before, producers = self.capture(), self.identities("node-context")
        retained = []

        def stale_retained():
            self.e.verify_binding()
            current = self.records()
            if not all(r["freshness"] == "stale" and r.get("lastGood") and r["nodeUID"] == old["nodeUID"]
                       for old, r in zip(before, current)):
                return False
            good = [r["lastGood"] for r in current]
            if not retained:
                retained.append(good)
                return False
            return good == retained[0] and self.identities("node-context") == producers

        namespace = self.e.bundle.configuration["namespace"]
        with suspended_producer_access(self.e.ownership, namespace):
            stale = self.wait(stale_retained, time.monotonic())
            # Include restoration in the declared recovery interval.
            started = time.monotonic()
        passed = self.wait(lambda: self.fresh(before) and self.identities("node-context") == producers, started)
        return self.result(started, stale and passed, stale)

    def restart(self, component):
        require(component in {"agent", "collector"}, "unsupported recovery component")
        before, identities, generations = self.capture(), self.identities(component), self.generations()
        started = time.monotonic()
        restart_workload(self.e.ownership, self.e.bundle.configuration["namespace"], component)

        def replaced():
            return all(old != current for old, current in zip(identities, self.identities(component)))

        if not self.wait(replaced, started):
            return self.result(started, False)
        new_identities = self.identities(component)
        if component == "agent":
            for runtime in self.runtimes:
                remaining = self.budget - (time.monotonic() - started)
                if remaining <= 0:
                    return self.result(started, False)
                attach(runtime, self.e.bundle.profile["workload"]["image"], "agent", timeout=min(120, remaining))

        def recovered():
            if not self.fresh(before) or self.identities(component) != new_identities:
                return False
            current = self.generations()
            if component == "collector":
                return all(old != new for old, new in zip(generations, current))
            return current == generations and all(r.component_metrics(r.containers()["agent"], 8082).get(
                'kubememlens_agent_snapshot_posts_total{result="success"}', 0) > 0 for r in self.runtimes)

        return self.result(started, self.wait(recovered, started))
