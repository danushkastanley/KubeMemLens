"""Exercise disruptions only inside the disposable kind fixture."""

import argparse
import json
import sys
import time
from pathlib import Path

from common import ContractError, load, require, write_new
from kind_runtime import KindRuntime, NAMESPACE
from profiles import validate_profile


def event(state="not-run"):
    return {"state": state, "elapsedSeconds": None, "identityVerified": False,
            "freshEvidence": False, "staleRetained": False}


class Lifecycle:
    def __init__(self, runtime, budget):
        self.r, self.budget = runtime, budget

    def record(self):
        return self.r.api("/nodecontexts/" + self.r.node)["record"]

    def history(self):
        return self.r.api("/nodecontexts/" + self.r.node + "/history?limit=1")

    def wait(self, predicate, started):
        while time.monotonic() - started <= self.budget:
            try:
                if predicate():
                    return True
            except (ContractError, KeyError, ValueError):
                # API downtime and missing current data are expected during disruption.
                pass
            time.sleep(2)
        return False

    def recovered(self, before, uid=None):
        current = self.record()
        status = self.r.api("/clusterstatus/current")["store"]
        return (current["freshness"] == "fresh" and current.get("lastGood", {}).get("reportedAt", "") >
                before["lastGood"]["reportedAt"] and current["nodeUID"] == (uid or before["nodeUID"])
                and status["reliability"]["freshNodes"] == 1
                and status.get("nodeContext", {}).get("freshRecords") == 1)

    def result(self, started, passed, stale=False):
        return {"state": "passed" if passed else "failed", "elapsedSeconds": time.monotonic() - started,
                "identityVerified": passed, "freshEvidence": passed, "staleRetained": stale}

    def source_loss(self, private):
        before = self.record()
        producer = self.r.containers()["node-context"]["id"]
        binding = "kube-memlens-node-context-producer"
        path = private / "restore-binding.json"
        saved = json.loads(self.r.k("get", "clusterrolebinding", binding, "-o", "json"))
        saved["metadata"] = {key: saved["metadata"][key] for key in ("name", "labels", "annotations") if key in saved["metadata"]}
        path.write_text(json.dumps(saved))
        path.chmod(0o600)
        stale = False
        retained_good = []
        try:
            self.r.k("delete", "clusterrolebinding", binding)
            # The loss window is separate from recovery timing. Stale retention must
            # preserve the last successful source timestamp, not refresh old data.
            def retained():
                record = self.record()
                if record["freshness"] != "stale" or not record.get("lastGood"):
                    return False
                if not retained_good:
                    retained_good.append(record["lastGood"])
                    return False
                return record["lastGood"] == retained_good[0]
            stale = self.wait(retained, time.monotonic())
        finally:
            started = time.monotonic()
            self.r.k("apply", "-f", str(path))
            path.unlink()
        passed = self.wait(lambda: self.recovered(before) and
                           self.r.containers()["node-context"]["id"] == producer, started)
        return self.result(started, passed and stale, stale)

    def restart(self, component):
        before = self.record()
        identity = self.r.containers()[component]["id"]
        generation = self.history()["generation"]
        resource = "daemonset/kube-memlens-agent" if component == "agent" else "deployment/kube-memlens-collector"
        started = time.monotonic()
        self.r.k("rollout", "restart", resource, "-n", NAMESPACE)

        def recovered():
            containers = self.r.containers()
            if not self.recovered(before) or containers[component]["id"] == identity:
                return False
            current = self.history()["generation"]
            if component == "agent":
                telemetry = self.r.component_metrics(containers[component], 8082)
                return current == generation and telemetry['kubememlens_agent_snapshot_posts_total{result="success"}'] > 0
            return current != generation

        return self.result(started, self.wait(recovered, started))

    def replace_identity(self):
        before = self.record()
        old_uid = before["nodeUID"]
        generation = self.history()["generation"]
        started = time.monotonic()
        # This changes the Kubernetes Node identity, not the VM or kernel. Provider
        # Node replacement must be performed and observed by its approved runner.
        self.r.host("systemctl", "stop", "kubelet")
        try:
            self.r.k("delete", "node", self.r.node, "--wait=false")
        finally:
            self.r.host("systemctl", "start", "kubelet")

        registered = self.wait(lambda: json.loads(self.r.k("get", "node", self.r.node, "-o", "json"))["metadata"]["uid"] != old_uid, started)
        if not registered:
            return self.result(started, False)
        # Replace the two source Pods so their bound identities name the new Node
        # UID. The collector stays live and must discard the old current record.
        self.r.k("rollout", "restart", "daemonset/kube-memlens-agent",
                 "daemonset/kube-memlens-node-context", "-n", NAMESPACE)

        def recovered():
            node = json.loads(self.r.k("get", "node", self.r.node, "-o", "json"))
            uid = node["metadata"]["uid"]
            if uid == old_uid or not self.recovered(before, uid):
                return False
            record = self.record()
            return (record["lastGood"]["nodeUID"] == uid and record["report"]["nodeUID"] == uid
                    and self.history()["generation"] == generation)

        return self.result(started, self.wait(recovered, started))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("cluster", "node", "kubeconfig", "profile", "output"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    result = {name: event() for name in ("sourceLoss", "agentRestart", "collectorRestart", "nodeIdentityReplacement")}
    result["providerNodeReplacement"] = event("not-applicable")
    try:
        p = validate_profile(load(args.profile))
        require(p["profileClass"] == "local-kind", "lifecycle requires local profile")
        require(not Path(args.output).exists(), "lifecycle output already exists")
        lifecycle = Lifecycle(KindRuntime(args.cluster, args.node, args.kubeconfig), p["budgets"]["recoverySeconds"])
        for name, run in (("sourceLoss", lambda: lifecycle.source_loss(Path(args.output).parent)),
                          ("agentRestart", lambda: lifecycle.restart("agent")),
                          ("collectorRestart", lambda: lifecycle.restart("collector")),
                          ("nodeIdentityReplacement", lifecycle.replace_identity)):
            print("qualification lifecycle: " + name, flush=True)
            result[name] = event("failed")
            result[name] = run()
            if result[name]["state"] != "passed":
                break
    except ContractError as error:
        print(f"local lifecycle observation failed: {error}", file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"local lifecycle observation failed ({type(error).__name__})", file=sys.stderr)
    write_new(args.output, result)
    # Preserve all observations; the evaluator determines pass/fail after cleanup.
    return 0


if __name__ == "__main__":
    sys.exit(main())
