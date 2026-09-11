"""Canonical, predeclared Node qualification measurements and budgets."""

from common import DIGEST, choice, digest, exact, integer, number, privacy, require

PROVIDERS = {"kind", "gke-standard", "eks-managed-nodes", "aks-node-pools", "self-managed"}
PROFILE_KEYS = {"schemaVersion", "id", "provider", "profileClass", "nodeImage", "workload", "measurement", "budgets", "requalificationDays", "profileDigest"}
MEASUREMENT_KEYS = {"baselineSeconds", "enabledSeconds", "settleSeconds", "sampleIntervalSeconds", "projectedLifetimeSeconds"}
BUDGET_KEYS = {"producerMeanCPUMilli", "producerPeakMemoryBytes", "kubeletMeanCPUIncreaseMilli", "kubeletPeakMemoryIncreaseBytes", "readP95Seconds", "responseMaxBytes", "agentScanP99Seconds", "recoverySeconds"}


def validate_profile(p):
    exact(p, PROFILE_KEYS, "profile")
    privacy(p)
    require(type(p["schemaVersion"]) is int and p["schemaVersion"] == 1, "unsupported profile schema")
    choice(p["provider"], PROVIDERS, "unknown qualification provider")
    choice(p["profileClass"], {"local-kind", "provider"}, "invalid profile class")
    require((p["provider"] == "kind") == (p["profileClass"] == "local-kind"), "profile class/provider mismatch")
    import re
    require(isinstance(p["id"], str) and re.fullmatch(r"[a-z][a-z0-9-]{1,63}", p["id"]), "invalid profile id")
    if p["profileClass"] == "local-kind":
        require(isinstance(p["nodeImage"], str) and re.fullmatch(r"kindest/node:v1\.(36|37)\.\d+@sha256:[a-f0-9]{64}", p["nodeImage"]), "local profile requires a pinned supported Node image")
    else:
        require(p["nodeImage"] == "provider-receipt", "provider image must come from verified inventory")
    exact(p["workload"], {"linuxNodes", "containers", "containersPerPod", "image"}, "workload")
    w = p["workload"]
    require(integer(w["linuxNodes"], 1, 10) and integer(w["containers"], 2, 2000) and integer(w["containersPerPod"], 1, 100) and w["containers"] % w["containersPerPod"] == 0, "invalid workload bounds")
    require(isinstance(w["image"], str) and re.fullmatch(r"[A-Za-z0-9./_-]+@sha256:[a-f0-9]{64}", w["image"]), "workload image must be pinned")
    exact(p["measurement"], MEASUREMENT_KEYS, "measurement")
    m = p["measurement"]
    require(integer(m["sampleIntervalSeconds"], 5, 30), "invalid sample interval")
    require(integer(m["baselineSeconds"], 120, 600) and integer(m["enabledSeconds"], 660, 1800), "insufficient or excessive measurement duration")
    require(integer(m["settleSeconds"], 30, 120) and integer(m["projectedLifetimeSeconds"], 600, 600), "invalid settling or projected lifetime")
    require(m["enabledSeconds"] / m["sampleIntervalSeconds"] <= 128, "profile exceeds sample capacity")
    exact(p["budgets"], BUDGET_KEYS, "budgets")
    b = p["budgets"]
    require(all(number(v) and v > 0 for v in b.values()), "budgets must be finite positive numbers")
    require(b["readP95Seconds"] <= 5 and b["responseMaxBytes"] <= 4 * 1024 * 1024 and b["recoverySeconds"] <= 300, "budgets exceed source or recovery contract")
    require(b["producerPeakMemoryBytes"] <= 64 * 1024 * 1024 and b["agentScanP99Seconds"] <= 4, "budgets exceed component contract")
    require(integer(p["requalificationDays"], 1, 90), "invalid requalification interval")
    require(isinstance(p["profileDigest"], str) and DIGEST.fullmatch(p["profileDigest"]) and p["profileDigest"] == digest(p, "profileDigest"), "profile digest mismatch")
    return p
