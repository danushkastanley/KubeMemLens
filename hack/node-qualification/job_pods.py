"""Bind a generated probe Pod to its exact Job and selected node."""

from common import require


def controlled_pod(parent, candidates, node):
    live = [p for p in candidates if not p["metadata"].get("deletionTimestamp")]
    require(len(live) <= 1, "network probe has multiple live Pods")
    if not live:
        return None
    pod = live[0]
    controller = {"apiVersion": "batch/v1", "kind": "Job", "controller": True,
                  "name": parent["metadata"]["name"], "uid": parent["metadata"]["uid"]}
    owners = [r for r in pod["metadata"].get("ownerReferences", []) if r.get("controller") is True]
    require(len(owners) == 1 and controller.items() <= owners[0].items()
            and pod["metadata"]["namespace"] == parent["metadata"]["namespace"],
            "probe Pod has a different controller")
    require(pod["spec"]["nodeName"] == node and pod["spec"].get("hostNetwork", False) is False,
            "probe Pod moved outside the bound network")
    return pod

