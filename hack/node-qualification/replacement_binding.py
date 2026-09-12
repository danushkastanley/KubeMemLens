"""Validate one operator-performed machine replacement without changing resources."""

import copy
import ipaddress
import uuid

from common import load, require
from prepare_provider import REPOSITORY
from provider_inventory import bind_nodes
from provider_plan import host_routes


def linux_nodes(document):
    nodes = document.get("items")
    require(isinstance(nodes, list) and len(nodes) <= 128, "replacement Node inventory is invalid")
    selected = [node for node in nodes if node.get("metadata", {}).get("labels", {}).get("kubernetes.io/os") == "linux"]
    uids = [node["metadata"]["uid"] for node in selected]
    require(all(isinstance(uid, str) and uid for uid in uids) and len(set(uids)) == len(uids),
            "replacement Node identities are missing or duplicated")
    return {node["metadata"]["uid"]: node for node in selected}


def machine(node):
    values = {}
    for key in ("systemUUID", "bootID"):
        raw = node.get("status", {}).get("nodeInfo", {}).get(key)
        require(isinstance(raw, str) and 0 < len(raw) <= 64, "Node machine identity is unavailable")
        try:
            value = uuid.UUID(raw)
        except ValueError:
            require(False, "Node machine identity is not a UUID")
        require(value.int not in {0, (1 << 128) - 1}, "Node machine identity is a placeholder")
        values[key] = str(value)
    return values


def capture(binding, document):
    nodes = linux_nodes(document)
    require(set(nodes) == {n["uid"] for n in binding["nodes"]}, "machine capture differs from the bound pool")
    for expected in binding["nodes"]:
        node = nodes[expected["uid"]]
        require(node["metadata"]["name"] == expected["name"]
                and node.get("spec", {}).get("providerID", "") == expected["providerID"],
                "machine capture differs from the bound Node identity")
    return {uid: machine(node) for uid, node in nodes.items()}


def detect(binding, document, approved_uid):
    """Detect registration before readiness; recovery timing starts at this point."""
    before = {n["uid"] for n in binding["nodes"]}
    require(approved_uid in before, "replacement target is outside the bound pool")
    after = set(linux_nodes(document))
    if after == before or len(after) != len(before):
        return None
    removed, added = before - after, after - before
    require(removed == {approved_uid} and len(added) == 1, "an unapproved Node identity changed")
    return next(iter(added))


def rebind(bundle, before, machines, document, original_receipt, fresh_receipt, approved_uid):
    new_uid = detect(before, document, approved_uid)
    require(new_uid is not None, "one replacement Node has not registered")
    for key in ("profile", "qualificationToolCommit", "provider", "nodeImage", "cniName", "controlPlaneVersion", "proofSource"):
        require(fresh_receipt[key] == original_receipt[key], "provider inventory changed during replacement")
    nodes = linux_nodes(document)
    expected = next(n for n in before["nodes"] if n["uid"] == approved_uid)
    new_machine = machine(nodes[new_uid])
    require(all(new_machine[key] != machines[approved_uid][key] for key in ("systemUUID", "bootID")),
            "Node registration or reboot does not prove machine replacement")
    for uid in nodes.keys() - {new_uid}:
        require(machine(nodes[uid]) == machines[uid], "the retained machine changed during replacement")
    family = ipaddress.ip_network(expected["route"]).version
    addresses = {ipaddress.ip_address(a["address"]) for a in nodes[new_uid]["status"].get("addresses", [])
                 if a.get("type") == "InternalIP" and "%" not in a.get("address", "")}
    routes = [str(address) + ("/32" if family == 4 else "/128") for address in addresses if address.version == family]
    require(len(routes) == 1, "replacement Node does not expose one host route in the approved address family")
    configuration = copy.deepcopy(bundle.configuration)
    configuration["nodeCIDRs"] = [routes[0] if route == expected["route"] else route for route in configuration["nodeCIDRs"]]
    host_routes(configuration["nodeCIDRs"], len(before["nodes"]))
    inventory = load(REPOSITORY / "hack/provider-profiles" / (configuration["inventoryProfile"] + ".json"))
    bound = bind_nodes(bundle.profile, configuration, inventory, fresh_receipt, document)
    require(bound["runtime"] == before["runtime"], "replacement changed the exact runtime profile")
    by_uid = {n["uid"]: n for n in bound["nodes"]}
    for old in before["nodes"]:
        if old["uid"] != approved_uid:
            require(by_uid[old["uid"]] == old, "the retained Node binding changed")
    # Preserve measurement slots even when the new Node's name sorts differently.
    bound["nodes"] = [by_uid[new_uid if n["uid"] == approved_uid else n["uid"]] for n in before["nodes"]]
    return bound, configuration["nodeCIDRs"]
