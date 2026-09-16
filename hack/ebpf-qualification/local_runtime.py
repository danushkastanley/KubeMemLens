"""Explicit local-only identities and UID-guarded optional-service transitions."""

import hashlib
import json
import re
import subprocess
import time

SERVICES = {"node": "binding-node", "api": "admission-api"}
MEASURE = "/usr/local/bin/kml-bpf008-measure"
CENSUS = "/usr/local/bin/kml-lifecycle-census"


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def command(args, data=None, timeout=10):
    result = subprocess.run(args, input=data, capture_output=True, timeout=timeout)
    if result.returncode or len(result.stdout) > 512 * 1024:
        raise RuntimeError("bounded local qualification operation failed")
    return result.stdout


def spec_digest(spec):
    value = dict(spec)
    value.pop("replicas", None)
    return digest(canonical(value))


class Runtime:
    def __init__(self, cfg):
        self.cfg = cfg
        if not cfg["context"].startswith("kind-") or not re.fullmatch(r"[a-z0-9-]+", cfg["node"]):
            raise ValueError("only explicitly named local kind nodes are allowed")
        if not re.fullmatch(r"[a-z0-9-]+", cfg["namespace"]):
            raise ValueError("invalid namespace")
        self.kube = ["kubectl", "--kubeconfig", cfg["kubeconfig"], "--context", cfg["context"]]
        endpoint = self.json(["config", "view", "--minify", "-o", "json"])["clusters"][0]["cluster"]["server"]
        if not re.fullmatch(r"https://(?:127\.0\.0\.1|localhost):[0-9]+", endpoint):
            raise ValueError("cluster endpoint is not loopback")
        node = self.json(["get", "node", cfg["node"], "-o", "json"])
        if node["metadata"]["uid"] != cfg["nodeUID"]:
            raise ValueError("Node lifetime changed")
        self.verify_tools()

    def json(self, args):
        return json.loads(command(self.kube + args))

    def get(self, kind, name):
        return self.json(["-n", self.cfg["namespace"], "get", kind, name, "-o", "json"])

    def exec(self, args, data=None, timeout=10):
        return command(["docker", "exec", "-i", self.cfg["node"], *args], data, timeout)

    def verify_tools(self):
        for path, key in ((MEASURE, "measureSHA256"), (CENSUS, "censusSHA256")):
            if self.exec(["sha256sum", path]).decode().split()[0] != self.cfg[key]:
                raise ValueError("observer binary changed")

    def deployment(self, role):
        obj = self.get("deployment", SERVICES[role])
        if (obj["metadata"]["uid"] != self.cfg["deploymentUIDs"][role] or
                spec_digest(obj["spec"]) != self.cfg["deploymentSpecSHA256"][role]):
            raise ValueError("optional installation changed")
        return obj

    def pods(self, role):
        return self.json(["-n", self.cfg["namespace"], "get", "pods", "-l", "app=" + SERVICES[role], "-o", "json"])["items"]

    def scale(self, role, replicas):
        if type(replicas) is not int or replicas not in (0, 1):
            raise ValueError("invalid replica count")
        obj = self.deployment(role)
        patch = [{"op": "test", "path": "/metadata/uid", "value": obj["metadata"]["uid"]},
                 {"op": "test", "path": "/metadata/resourceVersion", "value": obj["metadata"]["resourceVersion"]},
                 {"op": "replace", "path": "/spec/replicas", "value": replicas}]
        command(self.kube + ["-n", self.cfg["namespace"], "patch", "deployment", SERVICES[role],
                            "--type=json", "--patch-file=/dev/stdin"], canonical(patch))

    def absent(self):
        for role in SERVICES:
            if self.deployment(role)["spec"]["replicas"] != 0 or self.pods(role):
                raise ValueError("optional service still present")

    def wait_absent(self):
        deadline = time.monotonic() + 60
        while True:
            try:
                self.absent()
                return
            except ValueError:
                if time.monotonic() >= deadline:
                    raise
                time.sleep(1)

    def ready(self):
        for role in SERVICES:
            command(self.kube + ["-n", self.cfg["namespace"], "rollout", "status",
                                "deployment/" + SERVICES[role], "--timeout=60s"], timeout=65)
        deadline = time.monotonic() + 30
        while True:
            try:
                self.json(["get", "--raw", "/apis/tracing.kubememlens.io/v1alpha1"])
                return
            except RuntimeError:
                if time.monotonic() >= deadline:
                    raise
                time.sleep(1)

    def service(self, role):
        deployment = self.deployment(role)
        pods = self.pods(role)
        if deployment["spec"]["replicas"] != 1 or len(pods) != 1:
            raise ValueError("ambiguous service instance")
        pod = pods[0]
        if pod["metadata"].get("deletionTimestamp") or pod["spec"]["nodeName"] != self.cfg["node"]:
            raise ValueError("service placement/lifetime changed")
        if pod["spec"]["containers"][0]["image"] != self.cfg["image"]:
            raise ValueError("candidate image changed")
        if not any(x["type"] == "Ready" and x["status"] == "True" for x in pod["status"].get("conditions", [])):
            raise ValueError("service not Ready")
        owner = next(x for x in pod["metadata"]["ownerReferences"] if x["kind"] == "ReplicaSet" and x.get("controller"))
        replica = self.get("replicaset", owner["name"])
        if replica["metadata"]["uid"] != owner["uid"] or not any(
                x["kind"] == "Deployment" and x["uid"] == deployment["metadata"]["uid"] and x.get("controller")
                for x in replica["metadata"]["ownerReferences"]):
            raise ValueError("service controller changed")
        statuses = pod["status"]["containerStatuses"]
        if len(statuses) != 1 or "running" not in statuses[0]["state"] or statuses[0]["restartCount"] != 0:
            raise ValueError("container restarted or is not running")
        cid = statuses[0]["containerID"].removeprefix("containerd://")
        if not re.fullmatch("[a-f0-9]{64}", cid):
            raise ValueError("full runtime identity unavailable")
        cri = json.loads(self.exec(["crictl", "inspect", cid]))
        if cri["status"]["id"] != cid or cri["status"]["labels"].get("io.kubernetes.pod.uid") != pod["metadata"]["uid"]:
            raise ValueError("CRI Pod identity mismatch")
        pid = cri["info"]["pid"]
        if type(pid) is not int or pid <= 1:
            raise ValueError("invalid runtime process")
        flags = ["--parent-pid", str(pid), "--parent-sha256", self.cfg["nodeSHA256"], "--parent-container", cid]
        identity = json.loads(self.exec([CENSUS, "--mode", "identify", *flags]))
        flags += ["--parent-start", str(identity["parentStart"]), "--worker-sha256", self.cfg["workerSHA256"]]
        membership = self.exec(["cat", f"/proc/{pid}/cgroup"]).decode()
        if not membership.startswith("0::/") or membership.count("\n") != 1 or ".." in membership:
            raise ValueError("invalid cgroup membership")
        path = "/sys/fs/cgroup" + membership.strip()[3:]
        if not path.endswith("/cri-containerd-" + cid + ".scope"):
            raise ValueError("cgroup container identity mismatch")
        inode = int(self.exec(["stat", "-c", "%i", path]))
        snapshot = json.loads(self.exec([CENSUS, "--mode", "snapshot", *flags, "--target-cgroups", str(inode)]))
        if any(snapshot[k] != 0 for k in ("workers", "excludedWorkers", "activeControls")) or any(snapshot["objects"].values()):
            raise ValueError("service contains owned or unknown BPF activity")
        return {"group": {"role": role, "path": path, "inode": inode},
                "container": cid,
                "identity": digest(canonical([pod["metadata"]["uid"], cid, identity, inode])), "snapshot": snapshot}

    def stopped(self, previous):
        self.absent()
        for service in previous.values():
            cid = service["container"]
            containers = json.loads(self.exec(["crictl", "ps", "-a", "--id", cid, "-o", "json"]))["containers"]
            if any(x["id"] != cid or x["state"] != "CONTAINER_EXITED" for x in containers):
                raise ValueError("previous optional process remains running or ambiguous")

    def policy(self):
        node = self.json(["get", "node", self.cfg["node"], "-o", "json"])
        if node["metadata"]["uid"] != self.cfg["nodeUID"]:
            raise ValueError("Node lifetime changed")
        obj = self.get("configmap", self.cfg["policyConfigMap"])
        if digest(obj["data"]["policy.json"].encode()) != self.cfg["policySHA256"]:
            raise ValueError("candidate acceptance policy changed")
        policy = json.loads(obj["data"]["policy.json"])
        if (policy["programmeIndexSHA256"] != self.cfg["programmeIndexSHA256"] or
                policy["engine"]["workers"][node["status"]["nodeInfo"]["architecture"]] != self.cfg["workerSHA256"]):
            raise ValueError("programme or worker provenance mismatch")
