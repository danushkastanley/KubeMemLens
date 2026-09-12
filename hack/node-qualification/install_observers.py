"""Install only the fixed observer footprint in a verified owned namespace."""

import json
import time

from common import require
from observer_specs import HOST_OBSERVER, ephemeral_observer, host_observer, host_policy


def install_host(runtime, image, selector, tolerations=()):
    runtime.verify_namespace()
    created = []
    for manifest in (host_policy(runtime.namespace), host_observer(runtime.namespace, image, selector, tolerations)):
        result = json.loads(runtime.k("create", "-f", "-", "-o", "json", data=json.dumps(manifest).encode()))
        require(result["metadata"].get("uid"), "created observer has no identity")
        created.append({"kind": result["kind"], "name": result["metadata"]["name"], "uid": result["metadata"]["uid"]})
    runtime.k("rollout", "status", "daemonset/" + HOST_OBSERVER, "-n", runtime.namespace, "--timeout=120s", timeout=125)
    return created


def attach(runtime, image, component, audience=None, lifetime=600, timeout=120):
    require(component in {"agent", "node-context"}, "unsupported observer target")
    require(0 < timeout <= 120, "observer startup deadline is invalid")
    runtime.verify_namespace()
    current = runtime.containers()[component]
    pod = json.loads(runtime.k("get", "pod", current["pod"], "-n", runtime.namespace, "-o", "json"))
    require(pod["metadata"]["uid"] == current["podUID"], "target Pod identity changed")
    require(pod["spec"]["nodeName"] == runtime.node, "observer target moved to another Node")
    security = pod["spec"].get("securityContext", {})
    require(security.get("runAsUser") == 65532 and security.get("runAsGroup") == 65532,
            "target does not use the qualified non-root identity")
    require(not pod["spec"].get("ephemeralContainers"), "refusing to adopt or replace an existing ephemeral observer")
    if component == "node-context":
        volumes = [v for v in pod["spec"].get("volumes", []) if v["name"] == "kubelet-token"]
        require(len(volumes) == 1, "producer projection is missing or ambiguous")
        sources = volumes[0].get("projected", {}).get("sources", [])
        tokens = [s["serviceAccountToken"] for s in sources if "serviceAccountToken" in s]
        require(len(tokens) == 1 and tokens[0].get("path") == "token"
                and tokens[0].get("expirationSeconds") == lifetime and tokens[0].get("audience") == audience,
                "producer projection differs from approved settings")
    observer = ephemeral_observer(image, component)
    pod["spec"]["ephemeralContainers"] = [observer]
    # ResourceVersion and UID from the fresh GET make a concurrent replacement or
    # modification fail, rather than silently overwriting another operator's work.
    path = f"/api/v1/namespaces/{runtime.namespace}/pods/{current['pod']}/ephemeralcontainers"
    runtime.k("replace", "--raw", path, "-f", "-", data=json.dumps(pod).encode())
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        runtime.verify_namespace()
        observed = json.loads(runtime.k("get", "pod", current["pod"], "-n", runtime.namespace, "-o", "json"))
        require(observed["metadata"]["uid"] == current["podUID"], "target Pod was replaced during observer startup")
        running = [c for c in observed.get("status", {}).get("ephemeralContainerStatuses", [])
                   if c["name"] == observer["name"] and "running" in c.get("state", {})]
        if running:
            require(runtime.containers()[component]["id"] == current["id"], "target container restarted during observer startup")
            return {"pod": current["pod"], "podUID": current["podUID"], "container": observer["name"]}
        time.sleep(1)
    raise ValueError("ephemeral observer did not start within its deadline")
