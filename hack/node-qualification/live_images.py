"""Check actual Pod image identities against an already verified OCI graph."""

import json
import posixpath
import re

from common import DIGEST, require
from owned_resources import Resource


def image_id(value):
    require(isinstance(value, str), "runtime image identity is missing")
    matched = re.search(r"(?:^|@|://)(sha256:[a-f0-9]{64})$", value)
    require(matched is not None, "runtime image identity has no immutable digest")
    return matched.group(1)


def verify_container(pod, component, current, expected, reference, allowed_digests):
    require(pod["metadata"]["uid"] == current["podUID"] and not pod["metadata"].get("deletionTimestamp"),
            "image verification target was replaced")
    containers = [c for c in pod["spec"]["containers"] if c["name"] == component]
    statuses = [c for c in pod.get("status", {}).get("containerStatuses", []) if c["name"] == component]
    require(len(containers) == len(statuses) == 1, "image verification target is missing or ambiguous")
    container, status = containers[0], statuses[0]
    require(container["image"] == reference and container.get("command") == expected.get("command")
            and container.get("args", []) == expected.get("args", []), "runtime image command differs from the approved chart")
    binary = "memlens-" + component
    require(container.get("command") == ["/" + binary], "runtime does not execute the expected image binary")
    for mount in container.get("volumeMounts", []):
        path = posixpath.normpath(mount["mountPath"]).lstrip("/")
        require(path not in {"", binary}, "a runtime volume shadows the verified executable")
    require(status.get("ready") is True and "running" in status.get("state", {})
            and status.get("containerID", "").split("://")[-1] == current["id"], "runtime container changed during image verification")
    observed = image_id(status.get("imageID"))
    require(observed in allowed_digests, "runtime image is outside the verified OCI graph: observed=" + observed
            + "; expected=" + ",".join(sorted(allowed_digests)))


def verify(execution, proof, phase):
    require(phase in {"baseline", "enabled"}, "an explicit runtime image phase is required")
    components = ("agent", "collector") if phase == "baseline" else ("agent", "collector", "node-context")
    configuration = execution.bundle.configuration
    require(proof["imageDigest"] == configuration["imageDigest"]
            and proof["architecture"] == execution.binding["runtime"]["architecture"], "runtime image proof differs from the bound pool")
    allowed = {proof[key] for key in ("imageDigest", "archiveIndexDigest", "platformManifestDigest", "imageConfigDigest")}
    require(all(isinstance(value, str) and DIGEST.fullmatch(value) for value in allowed), "runtime image proof contains an invalid digest")
    execution.verify_binding()
    execution.ownership.verify(execution.installer.namespace)
    seen = set()
    reference = configuration["imageRepository"] + "@" + configuration["imageDigest"]
    for runtime in execution.runtimes:
        current = runtime.containers()
        for component in components:
            resource = Resource("apps/v1", "Deployment" if component == "collector" else "DaemonSet",
                                "kube-memlens-" + component, configuration["namespace"])
            desired = execution.installer.desired[phase][resource]["spec"]["template"]["spec"]["containers"]
            expected = next(c for c in desired if c["name"] == component)
            selected = current[component]
            pod = json.loads(runtime.k("get", "pod", selected["pod"], "-n", runtime.namespace, "-o", "json"))
            verify_container(pod, component, selected, expected, reference, allowed)
            seen.add(pod["metadata"]["uid"])
    execution.verify_binding()
    return {"imageDigest": configuration["imageDigest"], "architecture": proof["architecture"], "checkedPods": len(seen), "allMatched": True}
