"""Coordinate an already validated target through installation and fixed measurements."""

import copy
import ipaddress
import json
import time
from pathlib import Path

from common import ContractError, load, require, write_new
from install_observers import attach
from kubernetes_runtime import KubernetesRuntime
from live_images import verify as verify_images
from owned_resources import OwnedResources, Resource
from provider_install import Installer
from provider_probes import identities, require_no_extra_access, run_probes
from sample_nodes import measure_nodes
from window_contract import join_windows


def wait_until(predicate, timeout, label):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            if predicate():
                return
        except (ContractError, KeyError, ValueError):
            # API startup and absent observations are expected while waiting.
            # No following mutation is allowed until the predicate succeeds.
            pass
        time.sleep(2)
    raise ContractError(label + " did not become ready within its deadline")


class Execution:
    def __init__(self, bundle, binding, commands, private, inventory_binary, api_bridge, image_proof):
        self.bundle, self.binding, self.k, self.private = bundle, binding, commands, Path(private)
        self.ownership = OwnedResources(commands, self.private / "ownership")
        self.installer = Installer(bundle, self.ownership, inventory_binary)
        self.api_bridge, self.runtimes, self.windows, self.observations = api_bridge, [], {}, []
        self.initial_binding, self.replacement = copy.deepcopy(binding), None
        self.image_proof, self.image_checks = image_proof, {}
        require(image_proof["imageDigest"] == bundle.configuration["imageDigest"]
                and image_proof["architecture"] == binding["runtime"]["architecture"], "image proof differs from the execution target")

    def preflight(self):
        self.installer.preflight()
        namespace = self.bundle.configuration["namespace"]
        self.ownership.require_absent([Resource.from_object(m) for m in identities(namespace)])
        authentication = json.loads(self.k("get", "configmap", "extension-apiserver-authentication", "-n", "kube-system", "-o", "json"))
        names = json.loads(authentication.get("data", {}).get("requestheader-allowed-names", "[]"))
        require(isinstance(names, list) and names and all(isinstance(n, str) and n for n in names),
                "requestheader proxy identity is unavailable; the profile cannot weaken this prerequisite")
        self.verify_binding()

    def verify_binding(self):
        fields = {"kubernetes": "kubeletVersion", "kernel": "kernelVersion", "runtime": "containerRuntimeVersion",
                  "osImage": "osImage", "architecture": "architecture"}
        for expected in self.binding["nodes"]:
            node = json.loads(self.k("get", "node", expected["name"], "-o", "json"))
            require(node["metadata"]["uid"] == expected["uid"], "selected Node identity changed")
            require(not node["metadata"].get("deletionTimestamp") and any(c.get("type") == "Ready" and c.get("status") == "True"
                    for c in node["status"].get("conditions", [])), "selected Node is no longer ready")
            addresses = {str(ipaddress.ip_address(a["address"])) for a in node["status"].get("addresses", []) if a["type"] == "InternalIP"}
            require(str(ipaddress.ip_network(expected["route"]).network_address) in addresses
                    and node["spec"].get("providerID", "") == expected.get("providerID", ""), "selected Node address or provider identity changed")
            require(all(node["status"]["nodeInfo"][field] == self.binding["runtime"][key] for key, field in fields.items()),
                    "selected Node runtime changed")

    def prepare(self):
        self.preflight()
        self.installer.create_namespace()
        c = self.bundle.configuration
        # The public CA was already parsed and hash-bound by bundle validation.
        trust = json.loads((self.bundle.directory / "serving-trust.json").read_bytes())
        self.ownership.create(trust)
        self.observations = run_probes(c, self.binding["nodes"], self.ownership, self.k)
        self.installer.install("baseline")
        self.ownership.create(load(self.bundle.directory / "workload.json"))
        for manifest in load(self.bundle.directory / "host-observers.json")["items"]:
            self.ownership.create(manifest)
        namespace_uid = self.ownership.owned[self.installer.namespace]
        self.runtimes = [KubernetesRuntime(c["kubeconfigPath"], c["context"], c["namespace"], namespace_uid,
                         node["name"], str(self.api_bridge), node_uid=node["uid"]) for node in self.binding["nodes"]]
        for resource in ("daemonset/kube-memlens-agent", "deployment/qualification-load", "daemonset/node-qualification-host-observer"):
            self.k("rollout", "status", resource, "-n", c["namespace"], "--timeout=120s", timeout=125)
        expected = self.bundle.profile["workload"]["containers"]
        wait_until(lambda: all(r.workload() == (expected, expected) for r in self.runtimes), 120, "workload mapping")
        for runtime in self.runtimes:
            attach(runtime, self.bundle.profile["workload"]["image"], "agent")
        self.image_checks["baseline"] = verify_images(self, self.image_proof, "baseline")

    def measure(self, phase):
        require(phase in {"baseline", "enabled"} and phase not in self.windows, "measurement phase is invalid or already complete")
        require(bool(self.runtimes), "execution has not been prepared")
        require(phase == "baseline" or "baseline" in self.windows, "enabled measurement requires a completed baseline")
        self.verify_binding()
        result = measure_nodes(self.runtimes, self.bundle.profile, phase)
        self.verify_binding()
        self.windows[phase] = result
        write_new(self.private / (phase + "-measurements.json"), result)
        return result

    def enable(self):
        require("baseline" in self.windows and "enabled" not in self.windows, "profile enabling is out of order")
        self.verify_binding()
        self.installer.install("enabled")
        c = self.bundle.configuration
        self.k("rollout", "status", "daemonset/kube-memlens-node-context", "-n", c["namespace"], "--timeout=120s", timeout=125)
        require_no_extra_access(self.k, c["namespace"], "kube-memlens-node-context", self.binding["nodes"])
        def fresh():
            return all(r.api("/nodecontexts/" + r.node)["record"].get("freshness") == "fresh" for r in self.runtimes)
        wait_until(fresh, 120, "Node context")
        for runtime in self.runtimes:
            attach(runtime, self.bundle.profile["workload"]["image"], "node-context", audience=c["kubeletAudience"])
        self.image_checks["enabled"] = verify_images(self, self.image_proof, "enabled")

    def measurements(self):
        require(set(self.windows) == {"baseline", "enabled"}, "both measurement phases are required")
        return join_windows(self.bundle.profile, self.windows["baseline"], self.windows["enabled"])

    def cleanup(self):
        self.ownership.cleanup()
