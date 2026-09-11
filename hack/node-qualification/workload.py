"""Render the fixed local workload without identifiers in shared evidence."""

import argparse
import json

from common import load, require
from kind_runtime import LOAD_NAMESPACE
from profiles import validate_profile


def manifest(profile, node):
    p = validate_profile(profile)
    require(p["profileClass"] == "local-kind", "local workload needs a kind profile")
    w = p["workload"]
    containers = [{"name": f"hold-{i}", "image": w["image"],
                   "command": ["sh", "-c", "dd if=/dev/zero of=/memory/buffer bs=1M count=1 2>/dev/null; sleep 7200"],
                   "resources": {"requests": {"cpu": "1m", "memory": "4Mi"}, "limits": {"memory": "16Mi"}},
                   "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
                                       "capabilities": {"drop": ["ALL"]}},
                   "volumeMounts": [{"name": f"memory-{i}", "mountPath": "/memory"}]}
                  for i in range(w["containersPerPod"])]
    spec = {"nodeName": node, "automountServiceAccountToken": False,
            "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532,
                                "fsGroup": 65532, "seccompProfile": {"type": "RuntimeDefault"}},
            "containers": containers,
            "volumes": [{"name": f"memory-{i}", "emptyDir": {"medium": "Memory", "sizeLimit": "2Mi"}}
                        for i in range(w["containersPerPod"])]}
    return {"apiVersion": "v1", "kind": "List", "items": [
        {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": LOAD_NAMESPACE}},
        {"apiVersion": "apps/v1", "kind": "Deployment",
         "metadata": {"name": "qualification-load", "namespace": LOAD_NAMESPACE},
         "spec": {"replicas": w["containers"] // w["containersPerPod"],
                  "selector": {"matchLabels": {"app": "qualification-load"}},
                  "template": {"metadata": {"labels": {"app": "qualification-load"}}, "spec": spec}}}]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", required=True)
    parser.add_argument("--node", required=True)
    args = parser.parse_args()
    print(json.dumps(manifest(load(args.profile), args.node)))
