"""Measure controlled ingress and real-producer egress without claiming own-Node isolation."""

import ipaddress
import json
import time

from common import privacy, require
from network_specs import APP, MARKER, PORT, PREFIX, manifests
from observer_specs import IDENTITY_OBSERVER
from owned_mutations import suspended_network_rule
from owned_resources import Resource
from provider_execution import wait_until
from provider_recovery import PoolRecovery

REQUEST = ('reply=$(timeout 3 wget -q -T 2 -O - "$1" | head -c 64); '
           'case "$reply" in "' + MARKER + '") printf reachable;; "") printf blocked;; *) printf invalid-response;; esac')


def url(address):
    ip = ipaddress.ip_address(address)
    require(not (ip.is_unspecified or ip.is_loopback or ip.is_link_local or ip.is_multicast), "probe Pod address is invalid")
    host = "[" + str(ip) + "]" if ip.version == 6 else str(ip)
    return f"http://{host}:{PORT}/"


def policy_spec(spec):
    # The Kubernetes API omits empty direction arrays on serialisation. With
    # explicit policyTypes, an omitted direction and [] have identical meaning.
    return {"ingress": [], "egress": [], **spec}


class NetworkChecks:
    def __init__(self, execution):
        require(set(execution.windows) == {"baseline", "enabled"}, "network probes must follow fixed measurements")
        self.e, self.k, self.owner = execution, execution.k, execution.ownership
        self.namespace = execution.bundle.configuration["namespace"]
        self.nodes = [n["name"] for n in execution.binding["nodes"]]
        require([r.node for r in execution.runtimes] == self.nodes, "network runtime slots differ from the bound pool")
        self.image = execution.bundle.profile["workload"]["image"]
        self.documents = manifests(self.namespace, self.image, self.nodes)
        self.pods = {}
        self.producers = []

    def verify_policy(self):
        self.owner.verify(self.e.installer.namespace)
        resource = Resource("networking.k8s.io/v1", "NetworkPolicy", "kube-memlens-node-context", self.namespace)
        actual = self.owner.verify(resource)
        expected = self.e.installer.desired["enabled"][resource]
        require(policy_spec(actual["spec"]) == policy_spec(expected["spec"])
                and actual["spec"]["podSelector"] == {"matchLabels": APP},
                "producer policy differs from the approved chart")

    def prepare(self):
        self.e.verify_binding()
        self.verify_policy()
        self.producers = [r.containers()["node-context"] for r in self.e.runtimes]
        self.owner.require_absent([Resource.from_object(d) for d in self.documents])
        for document in self.documents:
            self.owner.create(document)
        for slot, node in enumerate(self.nodes):
            for role in ("ingress", "egress", "control"):
                name = f"{PREFIX}-{role}-{slot}"
                resource = Resource("v1" if role == "control" else "batch/v1", "Pod" if role == "control" else "Job", name, self.namespace)

                def ready():
                    parent = self.owner.verify(resource)
                    candidates = [parent] if role == "control" else json.loads(self.k(
                        "get", "pods", "-n", self.namespace, "-l", "job-name=" + name, "-o", "json"))["items"]
                    live = [p for p in candidates if not p["metadata"].get("deletionTimestamp")]
                    if len(live) != 1:
                        return False
                    pod = live[0]
                    if role != "control":
                        require(any(r.get("controller") is True and r.get("uid") == parent["metadata"]["uid"]
                                    for r in pod["metadata"].get("ownerReferences", [])), "probe Pod has a different controller")
                    require(pod["spec"]["nodeName"] == node and pod["spec"].get("hostNetwork", False) is False,
                            "probe Pod moved outside the bound network")
                    if not any(c["name"] == "probe" and c.get("ready") for c in pod.get("status", {}).get("containerStatuses", [])):
                        return False
                    url(pod["status"]["podIP"])
                    self.pods[(role, slot)] = pod
                    return True

                wait_until(ready, 60, "network probe")

    def verify_pod(self, role, slot):
        saved = self.pods[(role, slot)]
        current = json.loads(self.k("get", "pod", saved["metadata"]["name"], "-n", self.namespace, "-o", "json"))
        require(current["metadata"]["uid"] == saved["metadata"]["uid"] and not current["metadata"].get("deletionTimestamp")
                and current["spec"]["nodeName"] == self.nodes[slot] and current["status"]["podIP"] == saved["status"]["podIP"],
                "network probe identity or address changed")
        return current

    def exec_probe(self, role, slot, endpoint):
        pod = self.verify_pod(role, slot)
        result = self.k("exec", pod["metadata"]["name"], "-n", self.namespace, "-c", "probe", "--",
                        "sh", "-c", REQUEST, "sh", endpoint, maximum=128, timeout=8).strip()
        require(result in {"reachable", "blocked", "invalid-response"}, "network probe output is invalid")
        return result

    def request(self, direction, slot):
        self.e.verify_binding()
        self.verify_policy()
        target_slot = (slot + 1) % len(self.nodes) if direction == "egress" else slot
        target = self.verify_pod(direction, target_slot)
        require(self.exec_probe(direction, target_slot, f"http://127.0.0.1:{PORT}/") == "reachable",
                "network target is not serving the positive control")
        endpoint = url(target["status"]["podIP"])
        if direction == "ingress":
            return self.exec_probe("control", (slot + 1) % len(self.nodes), endpoint)
        runtime = self.e.runtimes[slot]
        producer = runtime.containers()["node-context"]
        require(producer == self.producers[slot], "producer identity changed during network verification")
        return runtime.pod_exec(producer["pod"], IDENTITY_OBSERVER, "sh", "-c", REQUEST, "sh", endpoint, maximum=128).strip()

    def wait_state(self, direction, slot, expected):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if self.request(direction, slot) == expected:
                return True
            time.sleep(1)
        return False

    def cycle(self, direction, slot):
        result = {"allowedBefore": self.wait_state(direction, slot, "reachable"), "blocked": False, "allowedAfter": False}
        if not result["allowedBefore"]:
            return result
        resource = Resource("networking.k8s.io/v1", "NetworkPolicy", PREFIX + "-" + direction, self.namespace)
        with suspended_network_rule(self.owner, resource, direction):
            result["blocked"] = self.wait_state(direction, slot, "blocked")
        result["allowedAfter"] = self.wait_state(direction, slot, "reachable")
        return result

    def run(self):
        self.prepare()
        recovery = PoolRecovery(self.e)
        before = recovery.capture()
        nodes = [{"slot": slot, "ingress": self.cycle("ingress", slot), "egress": self.cycle("egress", slot)}
                 for slot in range(len(self.nodes))]
        self.e.verify_binding()
        self.verify_policy()
        fresh_source = recovery.wait(lambda: recovery.fresh(before) and all(
            r.containers()["node-context"] == producer for r, producer in zip(self.e.runtimes, self.producers)), time.monotonic())
        result = {"schemaVersion": 1, "method": "controlled-ingress-producer-egress-v1", "qualified": False, "ownNodeIsolation": "not-claimed",
                  "nodes": nodes, "freshSourceRetained": fresh_source,
                  "passed": fresh_source and all(value for n in nodes for direction in ("ingress", "egress") for value in n[direction].values())}
        privacy(result)
        return result
