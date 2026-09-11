"""Strict Node qualification evidence; validation never grants qualification."""

from common import COMMIT, DIGEST, choice, digest, exact, instant, integer, number, privacy, require
from profiles import validate_profile

FIELDS = {"usage", "available", "workingSet", "rss", "swapUsage", "swapAvailable", "faults", "majorFaults", "psi", "systemContainers", "hugepages"}
SAMPLE_KEYS = {"elapsedSeconds", "producerCPUMilli", "producerMemoryBytes", "kubeletCPUMilli", "kubeletMemoryBytes", "agentScanSeconds", "sourceReads", "sourceFailures", "lastReadSeconds", "lastResponseBytes", "workloadContainers", "mappedContainers", "freshNodes", "producerReplicas", "unexpectedRestarts", "unexpectedOOMKills"}
EVENTS = {"sourceLoss", "agentRestart", "collectorRestart", "nodeIdentityReplacement", "providerNodeReplacement"}
PRIVACY = {"identifiersIncluded": False, "credentialsIncluded": False, "rawResponsesIncluded": False, "rawLogsIncluded": False}
ARTEFACTS = {"sourceCommit", "toolCommit", "sourceTreeDigest", "imageDigest", "chartDigest", "cliDigest", "producerDigest", "valuesDigest", "trustDigest", "audienceDigest", "sourceDirty"}
ENVIRONMENT = {"provider", "kubernetes", "kernel", "runtime", "nodeImage", "osImage", "architecture", "cgroupVersion", "cni", "linuxNodes", "providerReceiptDigest"}


def validate_evidence(p, e):
    validate_profile(p)
    exact(e, {"schemaVersion", "profile", "startedAt", "completedAt", "orchestration", "artefacts", "environment", "transport", "fields", "provenance", "samples", "rotation", "lifecycle", "cleanup", "privacy", "recordDigest"}, "evidence")
    privacy(e)
    require(type(e["schemaVersion"]) is int and e["schemaVersion"] == 1 and e["profile"] == {"id": p["id"], "digest": p["profileDigest"]}, "evidence profile mismatch")
    require(instant(e["completedAt"]) >= instant(e["startedAt"]), "reversed qualification window")
    choice(e["orchestration"], {"completed", "failed"}, "invalid orchestration outcome")
    exact(e["artefacts"], ARTEFACTS, "artefacts")
    for key, value in e["artefacts"].items():
        if key == "sourceDirty":
            require(type(value) is bool, "invalid source cleanliness")
        else:
            pattern = COMMIT if key.endswith("Commit") else DIGEST
            require(isinstance(value, str) and pattern.fullmatch(value), "invalid artefact identity")
    env = e["environment"]
    exact(env, ENVIRONMENT, "environment")
    require(env["provider"] == p["provider"], "environment provider does not match profile")
    choice(env["architecture"], {"amd64", "arm64", "unreported"}, "invalid architecture observation")
    choice(env["cgroupVersion"], {"v1", "v2", "unreported"}, "invalid cgroup observation")
    import re
    require(isinstance(env["kubernetes"], str) and re.fullmatch(r"v?1\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?", env["kubernetes"]), "invalid Kubernetes observation")
    for key in ("kernel", "runtime", "nodeImage", "osImage", "cni"):
        require(isinstance(env[key], str) and 1 <= len(env[key]) <= 160, "missing exact environment field")
    require(integer(env["linuxNodes"], 1, 10) and env["linuxNodes"] == p["workload"]["linuxNodes"], "Node topology differs from profile")
    receipt = env["providerReceiptDigest"]
    if p["profileClass"] == "local-kind":
        require(env["nodeImage"] == p["nodeImage"] and receipt is None, "local environment image or receipt mismatch")
        require(env["kubernetes"] == p["nodeImage"].split("@")[0].rsplit(":", 1)[-1], "local Kubernetes version differs from pinned image")
    else:
        require(isinstance(receipt, str) and DIGEST.fullmatch(receipt), "provider-owned receipt is required")
    exact(e["transport"], {"result", "reason", "directTLS", "podBoundIdentity", "statsOnlyRBAC", "proxyAccess", "networkPolicy", "servingTrust"}, "transport")
    choice(e["transport"]["result"], {"passed", "failed", "not-run"}, "invalid transport result")
    choice(e["transport"]["reason"], {"none", "untrusted-tls", "access-denied", "unreachable", "unsupported", "invalid-response", "timeout"}, "invalid transport reason")
    require((e["transport"]["result"] == "failed") == (e["transport"]["reason"] != "none"), "transport result/reason mismatch")
    for key in ("directTLS", "podBoundIdentity", "statsOnlyRBAC", "proxyAccess"):
        require(type(e["transport"][key]) is bool, "invalid transport observation")
    choice(e["transport"]["networkPolicy"], {"passed", "failed", "not-qualified"}, "invalid network-policy observation")
    trust = "fixture-ca" if p["profileClass"] == "local-kind" else "provider-verified"
    require(e["transport"]["servingTrust"] == trust, "serving trust does not match profile class")
    exact(e["fields"], FIELDS, "source fields")
    require(all(isinstance(v, str) and v in {"available", "unreported"} for v in e["fields"].values()), "invalid source field availability")
    choice(e["provenance"], {"unknown", "cadvisor", "cri"}, "invalid source provenance")
    exact(e["samples"], {"baseline", "enabled"}, "samples")
    for phase in ("baseline", "enabled"):
        validate_samples(e["samples"][phase], phase)
    exact(e["rotation"], {"observed", "sameProducer", "continuedAcquisition", "elapsedSeconds"}, "rotation")
    require(all(type(e["rotation"][k]) is bool for k in ("observed", "sameProducer", "continuedAcquisition")), "invalid rotation observation")
    require(e["rotation"]["elapsedSeconds"] is None or number(e["rotation"]["elapsedSeconds"]), "invalid rotation duration")
    exact(e["lifecycle"], EVENTS, "lifecycle")
    for event in e["lifecycle"].values():
        exact(event, {"state", "elapsedSeconds", "identityVerified", "freshEvidence", "staleRetained"}, "lifecycle event")
        choice(event["state"], {"passed", "failed", "not-run", "not-applicable"}, "invalid event outcome")
        require(event["elapsedSeconds"] is None or number(event["elapsedSeconds"]), "invalid event duration")
        require(all(type(event[k]) is bool for k in ("identityVerified", "freshEvidence", "staleRetained")), "invalid event observations")
    exact(e["cleanup"], {"workloadsRemoved", "rbacRemoved", "cloudResources"}, "cleanup")
    require(type(e["cleanup"]["workloadsRemoved"]) is bool and type(e["cleanup"]["rbacRemoved"]) is bool and isinstance(e["cleanup"]["cloudResources"], str) and e["cleanup"]["cloudResources"] in {"confirmed", "pending", "not-applicable"}, "invalid cleanup observation")
    require(e["privacy"] == PRIVACY and all(type(v) is bool for v in e["privacy"].values()), "qualification privacy assertions are missing")
    require(isinstance(e["recordDigest"], str) and DIGEST.fullmatch(e["recordDigest"]) and e["recordDigest"] == digest(e, "recordDigest"), "evidence digest mismatch")
    return e


def validate_samples(samples, phase):
    require(isinstance(samples, list) and len(samples) <= 128, "invalid sample bounds")
    previous = None
    for s in samples:
        exact(s, SAMPLE_KEYS, "sample")
        require(all(v is None or number(v) for v in s.values()), "invalid sample measurement")
        require(number(s["elapsedSeconds"]) and (previous is None or s["elapsedSeconds"] > previous), "sample times must increase")
        for key in ("producerMemoryBytes", "kubeletMemoryBytes", "sourceReads", "sourceFailures", "lastResponseBytes", "workloadContainers", "mappedContainers", "freshNodes", "producerReplicas", "unexpectedRestarts", "unexpectedOOMKills"):
            require(s[key] is None or integer(s[key], 0, 2**63 - 1), "invalid integer measurement")
        if phase == "baseline":
            require(all(s[k] is None for k in ("producerCPUMilli", "producerMemoryBytes", "sourceReads", "sourceFailures", "lastReadSeconds", "lastResponseBytes")), "disabled baseline must not invent producer measurements")
        previous = s["elapsedSeconds"]
