"""Observe an approved operator replacement; never mutate provider infrastructure."""

import copy
import json
import time

from common import ContractError, digest, require, write_new
from install_observers import attach
from kubernetes_runtime import KubernetesRuntime
from live_images import verify as verify_images
from owned_resources import Resource
from provider_inventory import inspect_inventory
from provider_recovery import PoolRecovery
from replacement_binding import capture, detect, rebind
from replacement_policy import replace_route
from source_summary import summarise

ACKNOWLEDGEMENT = "provider-action-approved"
REGISTRATION_WAIT_SECONDS = 1800


class ProviderReplacement:
    def __init__(self, execution, receipt, clock=time.monotonic, sleep=time.sleep):
        self.e, self.receipt, self.clock, self.sleep = execution, receipt, clock, sleep
        require(execution.bundle.profile["profileClass"] == "provider", "machine replacement requires a provider profile")
        self.budget = execution.bundle.profile["budgets"]["recoverySeconds"]

    def wait(self, observe, deadline, label):
        while self.clock() < deadline:
            try:
                value = observe()
                if value:
                    require(self.clock() < deadline, label + " exceeded its deadline")
                    return value
            except (ContractError, KeyError, ValueError):
                # Registration, readiness and reports may be incomplete during
                # replacement. No mutation follows until the full check succeeds.
                pass
            self.sleep(2)
        raise ContractError(label + " did not complete within its deadline")

    def remaining(self, deadline):
        seconds = deadline - self.clock()
        require(seconds > 0, "provider replacement exceeded its recovery budget")
        return seconds

    def run(self, slot, acknowledge):
        e, c = self.e, self.e.bundle.configuration
        require(acknowledge == ACKNOWLEDGEMENT, "operator replacement requires explicit approval")
        require(type(slot) is int and 0 <= slot < len(e.binding["nodes"]), "approved replacement slot is invalid")
        require(e.replacement is None, "this execution already observed a replacement")
        before = copy.deepcopy(e.binding)
        records = PoolRecovery(e).capture()
        machines = capture(before, json.loads(e.k("get", "nodes", "-o", "json")))
        target = before["nodes"][slot]
        # This private receipt gives the operator an exact target. Its existence
        # neither authorises an action nor proves that an action occurred.
        write_new(e.private / "replacement-target.private.json", {
            "slot": slot, "node": target, "machine": machines[target["uid"]],
            "registrationWaitSeconds": REGISTRATION_WAIT_SECONDS, "recoverySeconds": self.budget})
        print("provider replacement: waiting for the approved operator action", flush=True)
        self.wait(lambda: detect(before, json.loads(e.k("get", "nodes", "-o", "json")), target["uid"]),
                  self.clock() + REGISTRATION_WAIT_SECONDS, "replacement registration")
        started = self.clock()
        deadline = started + self.budget

        def ready_binding():
            receipt, document = inspect_inventory(e.bundle)
            bound, routes = rebind(e.bundle, before, machines, document, self.receipt, receipt, target["uid"])
            return bound, routes, receipt, capture(bound, document)

        bound, routes, receipt, after_machines = self.wait(ready_binding, deadline, "replacement inventory")
        self.remaining(deadline)
        e.binding = bound
        e.verify_binding()
        namespace = c["namespace"]
        resource = Resource("networking.k8s.io/v1", "NetworkPolicy", "kube-memlens-node-context", namespace)
        e.ownership.verify(e.installer.namespace)
        policy = replace_route(e.ownership, namespace, e.installer.desired["enabled"][resource]["spec"],
                               target["route"], bound["nodes"][slot]["route"])
        e.installer.desired["enabled"][resource]["spec"] = copy.deepcopy(policy)
        private_binding = {"before": before, "after": bound, "machinesBefore": machines,
                           "machinesAfter": after_machines, "nodeCIDRs": routes, "policy": policy}
        write_new(e.private / "replacement-binding.private.json", private_binding)
        write_new(e.private / "replacement-provider-inventory.json", receipt)
        e.replacement = {"bindingDigest": digest(private_binding, "bindingDigest"),
                         "providerReceiptDigest": receipt["receiptDigest"]}
        self.remaining(deadline)
        node = bound["nodes"][slot]
        e.runtimes[slot] = KubernetesRuntime(c["kubeconfigPath"], c["context"], namespace,
            e.ownership.owned[e.installer.namespace], node["name"], str(e.api_bridge), node_uid=node["uid"])

        def ready_containers():
            current = e.runtimes[slot].containers()
            return current if "node-context" in current else None

        self.wait(ready_containers, deadline, "replacement containers")
        for component in ("agent", "node-context"):
            arguments = {"audience": c["kubeletAudience"]} if component == "node-context" else {}
            attach(e.runtimes[slot], e.bundle.profile["workload"]["image"], component,
                   timeout=min(120, self.remaining(deadline)), **arguments)

        def recovered():
            e.verify_binding()
            current = [r.api("/nodecontexts/" + r.node)["record"] for r in e.runtimes]
            for runtime, old, record in zip(e.runtimes, records, current):
                good = record.get("lastGood", {})
                if not (record["freshness"] == "fresh" and record["nodeUID"] == good.get("nodeUID") == runtime.node_uid
                        and record.get("report", {}).get("nodeUID") == runtime.node_uid
                        and good.get("reportedAt", "") > old["lastGood"]["reportedAt"]):
                    return None
            runtime = e.runtimes[slot]
            posts = runtime.component_metrics(runtime.containers()["agent"], 8082).get(
                'kubememlens_agent_snapshot_posts_total{result="success"}', 0)
            status = runtime.api("/clusterstatus/current")["store"]
            if not (posts > 0 and status["reliability"]["freshNodes"] == len(e.runtimes)
                    and status.get("nodeContext", {}).get("freshRecords") == len(e.runtimes)):
                return None
            return current

        current = self.wait(recovered, deadline, "replacement source recovery")
        images = verify_images(e, e.image_proof, "enabled")
        self.remaining(deadline)
        e.replacement.update(liveImages=images,
            sourceSummary=summarise([r["lastGood"] for r in current], [r.node for r in e.runtimes]))
        elapsed = self.clock() - started
        require(elapsed <= self.budget, "provider replacement exceeded its recovery budget")
        return {"state": "passed", "elapsedSeconds": elapsed,
                "identityVerified": True, "freshEvidence": True, "staleRetained": False}
