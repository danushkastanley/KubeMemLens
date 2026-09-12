"""Reduce private per-Node diagnostics to conservative, identifier-free field metadata."""

from common import require


def _group(value):
    require(value is None or isinstance(value, dict), "source field group is invalid")
    return {} if value is None else value


def fields(observation):
    stats = observation.get("stats")
    require(isinstance(stats, dict), "source diagnostic has no stats")
    memory, swap, context = _group(stats.get("memory")), _group(stats.get("swap")), _group(observation.get("context"))
    result = {k: "available" if memory.get(v) is not None else "unreported"
              for k, v in {"usage": "usageBytes", "available": "availableBytes", "workingSet": "workingSetBytes",
                           "rss": "rssBytes", "faults": "pageFaults", "majorFaults": "majorPageFaults", "psi": "psi"}.items()}
    for key, value in (("swapUsage", swap.get("usageBytes")), ("swapAvailable", swap.get("availableBytes")),
                       ("systemContainers", stats.get("systemContainers")), ("hugepages", context.get("hugepages"))):
        result[key] = "available" if value is not None else "unreported"
    provenance = stats.get("provenance")
    require(isinstance(provenance, str) and provenance in {"unknown", "cadvisor", "cri"}, "source provenance is invalid")
    return result, provenance


def summarise(observations, node_names):
    require(isinstance(observations, list) and isinstance(node_names, list) and 1 <= len(node_names) <= 10,
            "source summary requires a bounded Node pool")
    require(all(isinstance(n, str) and n for n in node_names) and len(set(node_names)) == len(node_names),
            "source summary Node identities are missing or duplicated")
    require(len(observations) == len(node_names) and all(isinstance(o, dict) for o in observations),
            "source diagnostics must cover every selected Node")
    observed_names = [o.get("nodeName") for o in observations]
    require(all(isinstance(n, str) for n in observed_names) and sorted(observed_names) == sorted(node_names),
            "source diagnostics do not match the selected Node pool")
    summaries = [fields(o) for o in observations]
    combined = {key: "available" if all(row[key] == "available" for row, _ in summaries) else "unreported"
                for key in summaries[0][0]}
    sources = {provenance for _, provenance in summaries}
    return {"fields": combined, "provenance": next(iter(sources)) if len(sources) == 1 else "unknown"}
