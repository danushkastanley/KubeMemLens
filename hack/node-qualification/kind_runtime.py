"""Private, read-only telemetry from the runner-owned kind Node."""

import json
import re
import subprocess
from pathlib import Path, PurePosixPath
from urllib.parse import urlsplit

from common import ContractError, number, require

PREFIX = "/apis/memory.kubememlens.io/v1alpha1"
NAMESPACE = "kube-memlens-node-context"
LOAD_NAMESPACE = "node-qualification-load"


def command(argv, timeout=12):
    try:
        result = subprocess.run(argv, capture_output=True, timeout=timeout, check=True)
    except subprocess.CalledProcessError as error:
        stderr = error.stderr or b""
        reasons = {b"Permission denied": "permission-denied", b"No such file or directory": "missing-path",
                   b"Connection refused": "connection-refused", b"Couldn't connect to server": "connection-refused",
                   b"Operation not permitted": "operation-not-permitted"}
        reason = next((value for key, value in reasons.items() if key in stderr), "exit-" + str(error.returncode))
        raise ContractError("local command failed: " + reason) from error
    except (OSError, subprocess.SubprocessError) as error:
        # Command arguments/output can contain private cluster identities.
        raise ContractError("local qualification command failed") from error
    require(len(result.stdout) <= 16 * 1024 * 1024, "runtime response exceeded bounds")
    return result.stdout.decode()


def metrics(text):
    result = {}
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        require(len(parts) == 2 and parts[0] not in result, "invalid metrics response")
        try:
            value = float(parts[1])
        except ValueError as error:
            raise ContractError("invalid metric number") from error
        require(number(value), "invalid metric value")
        result[parts[0]] = value
    return result


def cgroup_path(value):
    path = PurePosixPath(value)
    require(value.startswith("/") and ".." not in path.parts and "\n" not in value,
            "invalid cgroup location")
    return "/sys/fs/cgroup" + value.rstrip("/")


def projected_path(document):
    mounts = document["info"]["runtimeSpec"]["mounts"]
    selected = [m["source"] for m in mounts if m.get("destination") == "/var/run/kubelet-token"]
    require(len(selected) == 1, "projected identity mount is missing or ambiguous")
    source = selected[0]
    require(isinstance(source, str) and source.startswith("/var/lib/kubelet/pods/")
            and "/volumes/kubernetes.io~projected/" in source
            and ".." not in PurePosixPath(source).parts and "\n" not in source,
            "projected identity mount is outside kubelet projection storage")
    return source.rstrip("/") + "/token"


class KindRuntime:
    def __init__(self, cluster, node, kubeconfig, run=command):
        require(re.fullmatch(r"kube-memlens-node-context-[a-z0-9-]+", cluster),
                "invalid owned fixture name")
        require(re.fullmatch(r"[a-z0-9-]+", node), "invalid fixture Node name")
        self.cluster, self.node, self.run = cluster, node, run
        self.kubectl = ["kubectl", "--kubeconfig", str(kubeconfig), "--context", "kind-" + cluster,
                        "--request-timeout=10s"]
        owner = self.docker("inspect", "--format", '{{index .Config.Labels "io.x-k8s.kind.cluster"}}', node).strip()
        require(owner == cluster, "refusing observation outside owned fixture")
        self.private = Path(kubeconfig).parent
        self.previous = {}

    def docker(self, *args):
        return self.run(["docker", *args])

    def host(self, *args):
        try:
            return self.docker("exec", self.node, *args)
        except ContractError as error:
            # Only executable names from this module are exposed, never arguments.
            raise ContractError(f"fixture {args[0]} operation: {error}") from error

    def k(self, *args):
        return self.run(self.kubectl + list(args))

    def api(self, path):
        server = (self.private / "api-server").read_text().strip()
        url = urlsplit(server)
        require(url.scheme == "https" and url.hostname and not url.username and not url.password
                and url.path in {"", "/"} and not url.query and not url.fragment,
                "invalid owned API endpoint")
        args = ["curl", "--fail", "--silent", "--show-error", "--max-time", "8",
                "--max-filesize", str(16 * 1024 * 1024),
                "--cacert", str(self.private / "api-ca.crt"),
                "--cert", str(self.private / "api-client.crt"),
                "--key", str(self.private / "api-client.key"),
                "--header", "X-KubeMemLens-Snapshot-Schema: 3", server.rstrip("/") + PREFIX + path]
        return json.loads(self.run(args))

    def containers(self):
        raw = json.loads(self.host("crictl", "ps", "-o", "json"))
        selected = {}
        for item in raw["containers"]:
            labels = item.get("labels", {})
            component = labels.get("io.kubernetes.container.name")
            if labels.get("io.kubernetes.pod.namespace") != NAMESPACE or component not in {"node-context", "agent", "collector"}:
                continue
            require(component not in selected, "ambiguous live component")
            identity = item["id"]
            require(re.fullmatch("[a-f0-9]{64}", identity), "invalid runtime identity")
            data = json.loads(self.host("crictl", "inspect", "-o", "json", identity))
            pid = data["info"]["pid"]
            require(type(pid) is int and pid > 0, "missing container process")
            selected[component] = {"id": identity, "pid": pid}
            if component == "node-context":
                selected[component]["identityPath"] = projected_path(data)
        require({"agent", "collector"} <= selected.keys(), "missing running component")
        return selected

    def resources(self, key, pid, now):
        text = self.host("cat", f"/proc/{pid}/cgroup").strip()
        require(text.startswith("0::") and "\n" not in text, "cgroup v2 is required")
        root = cgroup_path(text[3:])
        cpu = self.host("cat", root + "/cpu.stat")
        counters = dict(line.split() for line in cpu.splitlines())
        usage = int(counters["usage_usec"])
        memory = int(self.host("cat", root + "/memory.current").strip())
        require(usage >= 0 and memory > 0, "invalid charged resource counters")
        previous = self.previous.get(key)
        self.previous[key] = (usage, now, pid)
        if previous is None:
            return None, memory
        require(pid == previous[2] and usage >= previous[0] and now > previous[1],
                "resource process changed or counter reset during measurement")
        return (usage - previous[0]) / ((now - previous[1]) * 1000), memory

    def kubelet(self, now):
        pid = int(self.host("systemctl", "show", "kubelet", "--property=MainPID", "--value").strip())
        require(pid > 0, "kubelet process unavailable")
        return self.resources("kubelet", pid, now)

    def component_metrics(self, container, port):
        return metrics(self.host("nsenter", "-t", str(container["pid"]), "-n", "--",
                                 "curl", "--fail", "--silent", "--show-error", "--max-time", "3",
                                 f"http://127.0.0.1:{port}/metrics"))

    def projected_identity(self, container):
        # Only a digest enters this process; the credential never leaves the Node.
        # Projected symlinks belong to the Pod user. Enter the parent as the host
        # observer, then drop to the chart's UID/GID before following the link.
        # This preserves fs.protected_symlinks rather than relaxing host policy.
        directory = str(PurePosixPath(container["identityPath"]).parent)
        value = self.host("sh", "-c", 'cd "$1" && exec setpriv --reuid 65532 --regid 65532 --clear-groups sha256sum token',
                          "sh", directory).split()[0]
        require(re.fullmatch("[a-f0-9]{64}", value), "projected identity observation failed")
        return value

    def workload(self):
        pods = json.loads(self.k("get", "pods", "-n", LOAD_NAMESPACE, "-o", "json"))["items"]
        ready = {(p["metadata"]["uid"], c["name"]) for p in pods
                 for c in p.get("status", {}).get("containerStatuses", []) if c.get("ready")}
        page = self.api("/containers?limit=500")
        require(not page.get("metadata", {}).get("continue"), "unexpected fixture pagination")
        mapped = {(s["podUID"], s["containerName"]) for i in page["items"]
                  for s in [i["snapshot"]] if s.get("namespace") == LOAD_NAMESPACE
                  and s.get("containerID") and s.get("podUID") and s.get("containerName")}
        return len(ready), len(ready & mapped)

    def stability(self):
        pods = json.loads(self.k("get", "pods", "--all-namespaces", "-o", "json"))["items"]
        statuses = [c for p in pods if p["metadata"]["namespace"] in {NAMESPACE, LOAD_NAMESPACE}
                    for c in p.get("status", {}).get("containerStatuses", [])]
        restarts = sum(c["restartCount"] for c in statuses)
        oom = sum(c.get("lastState", {}).get("terminated", {}).get("reason") == "OOMKilled" or
                  c.get("state", {}).get("terminated", {}).get("reason") == "OOMKilled" for c in statuses)
        return restarts, oom
