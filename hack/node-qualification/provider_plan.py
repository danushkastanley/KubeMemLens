"""Validate private inputs for an offline provider proposal, not a live result."""

import hashlib
import ipaddress
import re
import ssl
import stat
from pathlib import Path

from common import COMMIT, DIGEST, ContractError, bounded, exact, require
from profiles import validate_profile
from review import RECEIPT_PROFILES

POOL_LABELS = {
    "gke-standard": "cloud.google.com/gke-nodepool",
    "eks-managed-nodes": "eks.amazonaws.com/nodegroup",
    "aks-node-pools": "kubernetes.azure.com/agentpool",
}
CONFIG_KEYS = {
    "schemaVersion", "inventoryProfile", "namespace", "context", "kubeconfigPath",
    "imageRepository", "imageDigest", "sourceCommit", "chartArchive", "chartDigest",
    "cliBinary", "cliDigest", "producerBinary", "producerDigest", "kubeletCAFile",
    "kubeletAudience", "apiServerCIDRs", "nodeCIDRs", "poolName",
    "kubernetesVersion",
}


def file_digest(path, maximum):
    target = Path(path)
    require(target.is_file(), "a required local input file is missing")
    require(0 < target.stat().st_size <= maximum, "input file size is outside its bound")
    h = hashlib.sha256()
    total = 0
    with target.open("rb") as source:
        for block in iter(lambda: source.read(65536), b""):
            total += len(block)
            require(total <= maximum, "input grew beyond its size bound")
            h.update(block)
    return "sha256:" + h.hexdigest()


def host_routes(values, maximum):
    require(isinstance(values, list) and 1 <= len(values) <= maximum, "invalid route count")
    result = []
    for value in values:
        require(isinstance(value, str) and "%" not in value, "route must be an unscoped IP host CIDR")
        try:
            network = ipaddress.ip_network(value, strict=True)
        except ValueError as error:
            raise ContractError("route must be a canonical host CIDR") from error
        require(network.prefixlen == network.max_prefixlen and not network.network_address.is_unspecified
                and not network.network_address.is_multicast and not network.network_address.is_loopback
                and not network.network_address.is_link_local and not network.network_address.is_reserved,
                "only explicit API or Node host routes are allowed")
        require(str(network) == value and value not in result, "route is duplicated or non-canonical")
        result.append(value)
    return result


def serving_ca(path):
    with Path(path).open("rb") as source:
        data = source.read(256 * 1024 + 1)
    require(0 < len(data) <= 256 * 1024, "serving CA size is outside its bound")
    blocks = re.findall(rb"-----BEGIN CERTIFICATE-----\s+[A-Za-z0-9+/=\r\n]+-----END CERTIFICATE-----", data)
    require(blocks and b"".join(data.split()) == b"".join(b"".join(blocks).split()),
            "serving trust must contain public certificates only")
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    try:
        context.load_verify_locations(cadata=data.decode("ascii"))
    except (ssl.SSLError, UnicodeError) as error:
        raise ContractError("serving CA could not be parsed") from error
    require(context.get_ca_certs(), "serving trust contains no CA certificate")
    return data


def validate_config(profile, config):
    p = validate_profile(profile)
    bounded(config)
    require(p["profileClass"] == "provider", "provider preparation requires a provider profile")
    exact(config, CONFIG_KEYS, "private provider configuration")
    require(type(config["schemaVersion"]) is int and config["schemaVersion"] == 1, "invalid preparation schema")
    require(isinstance(config["inventoryProfile"], str) and config["inventoryProfile"] in RECEIPT_PROFILES[p["provider"]], "inventory profile does not match provider")
    require(isinstance(config["kubernetesVersion"], str) and re.fullmatch(r"v?1\.(36|37)\.\d+(?:[-+][A-Za-z0-9.-]+)?", config["kubernetesVersion"]), "an exact Kubernetes 1.36 or 1.37 version is required")
    require(isinstance(config["namespace"], str) and re.fullmatch(r"kube-memlens-qualification-[a-z0-9](?:[a-z0-9-]{0,34}[a-z0-9])?", config["namespace"]), "invalid disposable namespace")
    for key in ("context", "kubeletAudience"):
        require(isinstance(config[key], str) and config[key].strip() == config[key]
                and 0 < len(config[key]) <= 256 and config[key].isascii()
                and all(32 <= ord(c) < 127 for c in config[key]) and not config[key].startswith("-"),
                "context and audience must be explicit printable values")
    repository = config["imageRepository"]
    require(isinstance(repository, str) and re.fullmatch(r"[a-z0-9][a-z0-9.-]*(?::[0-9]{1,5})?/(?:[a-z0-9]+[a-z0-9._-]*/)*[a-z0-9]+[a-z0-9._-]*", repository), "invalid image repository")
    registry = repository.split("/", 1)[0]
    if ":" in registry:
        require(1 <= int(registry.rsplit(":", 1)[1]) <= 65535, "invalid registry port")
    require(isinstance(config["sourceCommit"], str) and COMMIT.fullmatch(config["sourceCommit"]), "invalid source commit")
    for key in ("imageDigest", "chartDigest", "cliDigest", "producerDigest"):
        require(isinstance(config[key], str) and DIGEST.fullmatch(config[key]), "invalid artefact digest")
    for key in ("kubeconfigPath", "chartArchive", "cliBinary", "producerBinary", "kubeletCAFile"):
        value = config[key]
        require(isinstance(value, str) and Path(value).is_absolute() and Path(value).is_file(), "input paths must name existing absolute files")
    require(stat.S_ISREG(Path(config["kubeconfigPath"]).stat().st_mode), "kubeconfig must be a regular file")
    for path, expected, bound in (("chartArchive", "chartDigest", 20 * 1024 * 1024),
                                  ("cliBinary", "cliDigest", 128 * 1024 * 1024),
                                  ("producerBinary", "producerDigest", 128 * 1024 * 1024)):
        require(file_digest(config[path], bound) == config[expected], "local artefact digest mismatch")
    host_routes(config["apiServerCIDRs"], 8)
    host_routes(config["nodeCIDRs"], p["workload"]["linuxNodes"])
    require(len(config["nodeCIDRs"]) == p["workload"]["linuxNodes"], "one Node host route is required per profile Node")
    pool = config["poolName"]
    if p["provider"] in POOL_LABELS:
        require(isinstance(pool, str) and re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?", pool), "managed provider pool must be explicit")
    else:
        require(pool is None, "self-managed preparation does not infer a managed pool")
    serving_ca(config["kubeletCAFile"])
    return config


def values(profile, config, enabled):
    selector = {"kubernetes.io/os": "linux"}
    if profile["provider"] in POOL_LABELS:
        selector[POOL_LABELS[profile["provider"]]] = config["poolName"]
    return {
        "namespace": {"name": config["namespace"]},
        "image": {"repository": config["imageRepository"], "digest": config["imageDigest"], "pullPolicy": "IfNotPresent"},
        "agent": {"nodeSelector": selector, "tokenExpirationSeconds": profile["measurement"]["projectedLifetimeSeconds"]},
        "nodeContext": {"enabled": enabled, "kubeletCAConfigMap": "node-context-trust",
                        "kubeletAudience": config["kubeletAudience"], "apiServerCIDRs": config["apiServerCIDRs"],
                        "nodeCIDRs": config["nodeCIDRs"]},
    }
