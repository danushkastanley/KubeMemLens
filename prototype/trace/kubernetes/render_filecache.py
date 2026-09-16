"""Render the candidate file/cache deployment; never create policy or deploy it."""
import argparse
import json
import re

from render_admission import render


def configure(items, policy_configmap, confirmed_paths=False, max_node_traces=1):
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", policy_configmap):
        raise ValueError("invalid independently installed policy ConfigMap name")
    if type(max_node_traces) is not int or max_node_traces not in (1, 2):
        raise ValueError("node trace concurrency must be 1 or 2")
    found = set()
    for item in items:
        if item["kind"] != "Deployment":
            continue
        name = item["metadata"]["name"]
        if name not in {"binding-node", "admission-api"} or name in found:
            raise ValueError("unexpected candidate deployment")
        found.add(name)
        pod = item["spec"]["template"]["spec"]
        container = pod["containers"][0]
        container["args"] += ["--acceptance-policy", "/acceptance/policy.json"]
        container["volumeMounts"].append({"name": "acceptance", "mountPath": "/acceptance", "readOnly": True})
        pod["volumes"].append({"name": "acceptance", "configMap": {
            "name": policy_configmap, "defaultMode": 0o444,
            "items": [{"key": "policy.json", "path": "policy.json"}],
        }})
        if name == "binding-node":
            pod["securityContext"]["seccompProfile"] = {
                "type": "Localhost", "localhostProfile": "kube-memlens-trace/filecache-node.json"}
        else:
            if confirmed_paths:
                container["args"].append("--allow-confirmed-paths")
            if max_node_traces == 2:
                container["args"] += ["--max-node-traces", "2"]
    if found != {"binding-node", "admission-api"}:
        raise ValueError("missing candidate deployment")
    return items


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("image", "node", "node-uid", "namespace", "kubelet-cgroup-root", "certificate-directory", "policy-configmap"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--allow-confirmed-paths", action="store_true")
    parser.add_argument("--max-node-traces", type=int, choices=(1, 2), default=1)
    args = parser.parse_args()
    try:
        output = render(args.image, args.node, args.node_uid, args.namespace,
                        args.kubelet_cgroup_root, args.certificate_directory)
        output["items"] = configure(output["items"], args.policy_configmap, args.allow_confirmed_paths, args.max_node_traces)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    print(json.dumps(output, indent=2))
