"""Retarget one kubelet host route in the run's exact owned producer policy."""

import copy
import ipaddress

from common import require
from owned_mutations import _patch
from owned_resources import Resource
from provider_plan import host_routes


def spec(value):
    return {"ingress": [], "egress": [], **value}


def replace_route(ownership, namespace, expected, old_route, new_route):
    resource = Resource("networking.k8s.io/v1", "NetworkPolicy", "kube-memlens-node-context", namespace)
    actual = ownership.verify(resource)
    wanted = spec(expected)
    require(spec(actual["spec"]) == wanted, "producer policy changed before Node replacement")
    require(wanted["podSelector"] == {"matchLabels": {"app.kubernetes.io/name": "kube-memlens-node-context"}}
            and wanted["policyTypes"] == ["Ingress", "Egress"] and wanted["ingress"] == []
            and len(wanted["egress"]) == 2, "producer policy differs from the replacement contract")
    peers = wanted["egress"][1]["to"]
    routes = [peer["ipBlock"]["cidr"] for peer in peers]
    require(peers == [{"ipBlock": {"cidr": route}} for route in routes] and routes.count(old_route) == 1,
            "replacement must select one existing kubelet host route")
    host_routes(routes, 10)
    host_routes([new_route], 1)
    require(ipaddress.ip_network(old_route).version == ipaddress.ip_network(new_route).version,
            "replacement host route changed address family")
    new_routes = [new_route if route == old_route else route for route in routes]
    host_routes(new_routes, 10)
    if old_route == new_route:
        return actual["spec"]
    egress = copy.deepcopy(wanted["egress"])
    egress[1]["to"] = [{"ipBlock": {"cidr": route}} for route in new_routes]
    updated = _patch(ownership, resource, actual, [{"op": "replace", "path": "/spec/egress", "value": egress}])
    require(spec(updated["spec"]) == {**wanted, "egress": egress}, "replacement policy mutation changed unrelated rules")
    return updated["spec"]
