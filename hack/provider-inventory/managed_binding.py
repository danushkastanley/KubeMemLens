#!/usr/bin/env python3

import json
import re
import subprocess
from urllib.parse import urlsplit


MAX_COMMAND_OUTPUT_BYTES = 2 * 1024 * 1024
CONTEXT_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/@=+-]{0,255}")
POOL_LABELS = {
    "gke": "cloud.google.com/gke-nodepool",
    "eks": "eks.amazonaws.com/nodegroup",
    "aks": "kubernetes.azure.com/agentpool",
}


def _run_json(command, runner, error_type):
    try:
        result = runner(command, check=False, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise error_type(f"{command[0]} live binding command did not complete") from error
    if result.returncode != 0:
        raise error_type(f"{command[0]} live binding command failed")
    if not isinstance(result.stdout, str) or len(result.stdout.encode()) > MAX_COMMAND_OUTPUT_BYTES:
        raise error_type(f"{command[0]} live binding response was invalid or too large")
    try:
        value = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise error_type(f"{command[0]} live binding response was not JSON") from error
    if not isinstance(value, dict):
        raise error_type(f"{command[0]} live binding response was not an object")
    return value


def _context(environment, error_type):
    context = environment.get("QUALIFY_CONTEXT", "")
    if not isinstance(context, str) or CONTEXT_PATTERN.fullmatch(context) is None:
        raise error_type("QUALIFY_CONTEXT is required for managed provider binding")
    return context


def _hostname(value):
    if not isinstance(value, str) or not value:
        return None
    parsed = urlsplit(value if "://" in value else "https://" + value)
    if parsed.scheme != "https" or parsed.username or parsed.password or parsed.query or parsed.fragment:
        return None
    return parsed.hostname.casefold().rstrip(".") if parsed.hostname else None


def _provider_hosts(kind, cluster):
    if kind == "gke":
        endpoints = [cluster.get("endpoint")]
        endpoint_config = cluster.get("controlPlaneEndpointsConfig", {})
        if isinstance(endpoint_config, dict):
            dns_config = endpoint_config.get("dnsEndpointConfig", {})
            if isinstance(dns_config, dict):
                endpoints.append(dns_config.get("endpoint"))
        private_config = cluster.get("privateClusterConfig", {})
        if isinstance(private_config, dict):
            endpoints.append(private_config.get("privateEndpoint"))
        return {_hostname(value) for value in endpoints if _hostname(value)}
    if kind == "eks":
        host = _hostname(cluster.get("endpoint"))
        return {host} if host else set()
    if kind == "aks":
        return {
            host for host in (_hostname(cluster.get("fqdn")), _hostname(cluster.get("privateFqdn")))
            if host
        }
    return set()


def _live_server_host(config, error_type):
    clusters = config.get("clusters")
    if not isinstance(clusters, list) or len(clusters) != 1:
        raise error_type("kubectl minified context did not expose one cluster")
    cluster = clusters[0].get("cluster", {}) if isinstance(clusters[0], dict) else {}
    host = _hostname(cluster.get("server")) if isinstance(cluster, dict) else None
    if not host:
        raise error_type("kubectl minified context server was invalid")
    return host


def _selected_linux_nodes(nodes, label, pool_name, expectations, error_type):
    items = nodes.get("items")
    if not isinstance(items, list):
        raise error_type("kubectl Node inventory was invalid")
    selected = []
    for item in items:
        metadata = item.get("metadata", {}) if isinstance(item, dict) else {}
        labels = metadata.get("labels", {}) if isinstance(metadata, dict) else {}
        if isinstance(labels, dict) and labels.get(label) == pool_name:
            selected.append(item)
    if not selected:
        raise error_type("selected provider node pool has no live Nodes")
    fields = {
        "osImage": "osImagePattern",
        "containerRuntimeVersion": "runtimePattern",
        "architecture": "architecturePattern",
    }
    for item in selected:
        labels = item.get("metadata", {}).get("labels", {})
        info = item.get("status", {}).get("nodeInfo", {})
        if labels.get("kubernetes.io/os") != "linux" or not isinstance(info, dict):
            raise error_type("selected provider node pool is not an all-Linux runtime profile")
        for field, pattern_name in fields.items():
            value = info.get(field)
            if not isinstance(value, str) or re.fullmatch(expectations[pattern_name], value) is None:
                raise error_type("selected provider node pool does not match the canonical runtime profile")


def verify_managed_binding(kind, environment, runner, cluster, pool_name, expectations, error_type):
    context = _context(environment, error_type)
    base = ["kubectl", "--context", context]
    config = _run_json([*base, "config", "view", "--minify", "-o", "json"], runner, error_type)
    provider_hosts = _provider_hosts(kind, cluster)
    if not provider_hosts or _live_server_host(config, error_type) not in provider_hosts:
        raise error_type("live Kubernetes context does not match the provider-owned cluster endpoint")
    nodes = _run_json([*base, "get", "nodes", "-o", "json"], runner, error_type)
    _selected_linux_nodes(nodes, POOL_LABELS[kind], pool_name, expectations, error_type)
