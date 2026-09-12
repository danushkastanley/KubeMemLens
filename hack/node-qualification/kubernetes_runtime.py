"""Provider-capable observation through Kubernetes, without SSH or nodes/proxy."""

import json
import re
from pathlib import PurePosixPath

from common import require
from kind_runtime import metrics
from observer_specs import HOST_OBSERVER, IDENTITY_OBSERVER, METRICS_OBSERVER
from process import execute

API_FAILURE_CODES = {code: "qualification API read failed: " + category for code, category in {
    10: "authentication", 11: "forbidden", 12: "not-found", 13: "rate-limited", 14: "server-unavailable",
    15: "deadline", 16: "cancelled", 17: "transport", 18: "invalid-response"}.items()}


class KubernetesRuntime:
    def __init__(self, kubeconfig, context, namespace, namespace_uid, node, bridge,
                 workload_namespace=None, workload_uid=None, run=execute, node_uid=None):
        require(all(isinstance(v, str) and v for v in (kubeconfig, context, namespace, namespace_uid, node, bridge)),
                "explicit runtime target and namespace identity are required")
        for value in (namespace, node, workload_namespace or namespace):
            require(re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,251}[a-z0-9]|[a-z0-9]", value), "invalid runtime resource name")
        self.kubectl = ["kubectl", "--kubeconfig", kubeconfig, "--context", context, "--request-timeout=10s"]
        self.bridge = [bridge, "--kubeconfig", kubeconfig, "--context", context]
        self.namespace, self.namespace_uid, self.node = namespace, namespace_uid, node
        self.workload_namespace = workload_namespace or namespace
        self.workload_uid = workload_uid or namespace_uid
        self.run, self.previous = run, {}
        self.node_uid = node_uid

    def k(self, *args, data=None, maximum=2 * 1024 * 1024, timeout=12):
        return self.run(self.kubectl + list(args), data=data, maximum=maximum, timeout=timeout)

    def verify_namespace(self, namespace=None, uid=None):
        data = json.loads(self.k("get", "namespace", namespace or self.namespace, "-o", "json"))
        require(data["metadata"]["uid"] == (uid or self.namespace_uid), "qualification namespace identity changed")

    def api(self, path):
        namespace = self.namespace
        args = []
        if path == "/clusterstatus/current":
            operation = "status"
        elif path == "/containers?limit=500":
            operation, namespace = "containers", self.workload_namespace
        elif path == "/nodecontexts/" + self.node:
            operation, args = "node", ["--node", self.node]
        elif path == "/nodecontexts/" + self.node + "/history?limit=1":
            operation, args = "history", ["--node", self.node]
        else:
            raise ValueError("unsupported qualification API read")
        return json.loads(self.run(self.bridge + ["--namespace", namespace, "--operation", operation] + args,
                                   failure_codes=API_FAILURE_CODES))

    def pods(self):
        self.verify_namespace()
        return json.loads(self.k("get", "pods", "-n", self.namespace, "-o", "json"))["items"]

    def containers(self):
        selected = {}
        for pod in self.pods():
            if pod["metadata"].get("deletionTimestamp"):
                continue
            app = pod["metadata"].get("labels", {}).get("app.kubernetes.io/name")
            for status in pod.get("status", {}).get("containerStatuses", []):
                component = status["name"]
                if component not in {"agent", "collector", "node-context"} or app != "kube-memlens-" + component:
                    continue
                if component != "collector" and pod["spec"].get("nodeName") != self.node:
                    continue
                if not status.get("ready") or "running" not in status.get("state", {}):
                    continue
                require(component not in selected, "ambiguous active component")
                identity = status.get("containerID", "").split("://")[-1]
                require(re.fullmatch(r"[a-f0-9]{64}", identity), "invalid runtime container identity")
                selected[component] = {"id": identity, "pod": pod["metadata"]["name"], "podUID": pod["metadata"]["uid"]}
        require({"agent", "collector"} <= selected.keys(), "active agent or collector is missing")
        return selected

    def pod_exec(self, pod, container, *args, maximum=16 * 1024):
        return self.k("exec", pod, "-n", self.namespace, "-c", container, "--", *args, maximum=maximum)

    def host_exec(self, *args):
        pods = [p for p in self.pods() if p["metadata"].get("labels", {}).get("app.kubernetes.io/name") == HOST_OBSERVER
                and p["spec"].get("nodeName") == self.node and not p["metadata"].get("deletionTimestamp")]
        require(len(pods) == 1, "host observer is missing or ambiguous")
        require(any(c["name"] == "observer" and c.get("ready") and "running" in c.get("state", {})
                    for c in pods[0].get("status", {}).get("containerStatuses", [])), "host observer is not ready")
        return self.pod_exec(pods[0]["metadata"]["name"], "observer", *args)

    def group(self, pattern):
        paths = self.host_exec("find", "/host/sys/fs/cgroup", "-xdev", "-maxdepth", "12", "-type", "d", "-name", pattern).splitlines()
        require(len(paths) == 1, "isolated cgroup telemetry is missing or ambiguous")
        path = paths[0]
        require(path.startswith("/host/sys/fs/cgroup/") and ".." not in PurePosixPath(path).parts,
                "cgroup telemetry escaped the read-only root")
        return path

    def resources(self, key, identity, path, now):
        text = self.host_exec("sh", "-c", 'cat "$1/cpu.stat" && printf "\\nMEMORY\\n" && cat "$1/memory.current"', "sh", path)
        cpu, memory = text.split("\nMEMORY\n")
        usage = int(dict(line.split() for line in cpu.splitlines() if line)["usage_usec"])
        memory = int(memory.strip())
        require(usage >= 0 and memory > 0, "invalid charged resource counters")
        before = self.previous.get(key)
        self.previous[key] = (usage, now, identity)
        if before is None:
            return None, memory
        require(before[2] == identity and usage >= before[0] and now > before[1], "resource identity changed or counter reset")
        return (usage - before[0]) / ((now - before[1]) * 1000), memory

    def kubelet(self, now):
        node = json.loads(self.k("get", "node", self.node, "-o", "json"))
        if self.node_uid is None:
            self.node_uid = node["metadata"]["uid"]
        require(node["metadata"]["uid"] == self.node_uid, "selected Node identity changed")
        return self.resources("kubelet", node["metadata"]["uid"], self.group("kubelet.service"), now)

    def producer_resources(self, container, now):
        return self.resources("producer", container["id"], self.group("*" + container["id"] + "*"), now)

    def component_metrics(self, container, port):
        require(port in {8082, 8083}, "unexpected component metrics port")
        observer = METRICS_OBSERVER if port == 8082 else IDENTITY_OBSERVER
        return metrics(self.pod_exec(container["pod"], observer, "wget", "-qO-", "-T", "3", f"http://127.0.0.1:{port}/metrics"))

    def projected_identity(self, container):
        value = self.pod_exec(container["pod"], IDENTITY_OBSERVER, "sha256sum", "/identity/token").split()[0]
        require(re.fullmatch(r"[a-f0-9]{64}", value), "projected identity observation failed")
        return value

    def workload(self):
        self.verify_namespace(self.workload_namespace, self.workload_uid)
        pods = json.loads(self.k("get", "pods", "-n", self.workload_namespace, "-l", "app=qualification-load", "-o", "json"))["items"]
        ready = {(p["metadata"]["uid"], c["name"]) for p in pods for c in p.get("status", {}).get("containerStatuses", []) if c.get("ready")}
        page = self.api("/containers?limit=500")
        mapped = {(s["podUID"], s["containerName"]) for i in page["items"] for s in [i["snapshot"]]
                  if s.get("namespace") == self.workload_namespace and s.get("containerID") and s.get("podUID") and s.get("containerName")}
        return len(ready), len(ready & mapped)

    def stability(self):
        pods = self.pods()
        if self.workload_namespace != self.namespace:
            self.verify_namespace(self.workload_namespace, self.workload_uid)
            pods += json.loads(self.k("get", "pods", "-n", self.workload_namespace, "-o", "json"))["items"]
        states = [c for p in pods for field in ("containerStatuses", "ephemeralContainerStatuses") for c in p.get("status", {}).get(field, [])]
        return (sum(c.get("restartCount", 0) for c in states),
                sum(c.get("lastState", {}).get("terminated", {}).get("reason") == "OOMKilled" or
                    c.get("state", {}).get("terminated", {}).get("reason") == "OOMKilled" for c in states))
