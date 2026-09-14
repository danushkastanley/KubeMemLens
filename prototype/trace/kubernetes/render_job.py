"""Render one bounded administrator preflight Job; never call Kubernetes."""
import argparse
import json
import re


def job(node, image, name, seccomp="kube-memlens-trace/preflight.json"):
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?", node):
        raise ValueError("invalid node name")
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", name):
        raise ValueError("invalid job name")
    if not re.fullmatch(r"[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}", image):
        raise ValueError("an immutable image digest is required")
    if seccomp != "kube-memlens-trace/preflight.json":
        raise ValueError("unapproved security profile")
    paths = {
        "btf": "/sys/kernel/btf", "tracing": "/sys/kernel/tracing",
        "security": "/sys/kernel/security", "bpf": "/sys/fs/bpf",
    }
    return {
        "apiVersion": "batch/v1", "kind": "Job",
        "metadata": {"name": name, "namespace": "kube-memlens-trace-preflight"},
        "spec": {
            "backoffLimit": 0, "activeDeadlineSeconds": 30,
            "ttlSecondsAfterFinished": 300,
            "template": {"spec": {
                "nodeName": node, "restartPolicy": "Never",
                "serviceAccountName": "preflight", "automountServiceAccountToken": False,
                "hostPID": False, "hostNetwork": False, "hostIPC": False,
                "securityContext": {"seccompProfile": {"type": "Localhost", "localhostProfile": seccomp}},
                "containers": [{
                    "name": "preflight", "image": image, "imagePullPolicy": "Never",
                    "args": ["doctor", "--json", "--timeout", "10s"],
                    "securityContext": {
                        "privileged": False, "allowPrivilegeEscalation": False,
                        "readOnlyRootFilesystem": True, "runAsUser": 0, "runAsGroup": 0,
                        "capabilities": {"drop": ["ALL"], "add": ["BPF", "PERFMON"]},
                    },
                    "resources": {"requests": {"cpu": "100m", "memory": "64Mi"},
                                  "limits": {"cpu": "2", "memory": "256Mi"}},
                    "volumeMounts": [{"name": n, "mountPath": path, "readOnly": True}
                                     for n, path in paths.items()],
                }],
                "volumes": [{"name": n, "hostPath": {"path": path, "type": "Directory"}}
                            for n, path in paths.items()],
            }},
        },
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--node", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--name", required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(job(args.node, args.image, args.name), indent=2))
    except ValueError as error:
        parser.error(str(error))
