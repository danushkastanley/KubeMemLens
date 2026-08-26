#!/usr/bin/env python3

from evidence_manifest import (
    EVIDENCE_FILES,
    FINAL_NAME,
    MANIFEST_NAME,
    PENDING_NAME,
    ManifestError,
    read_json_file,
    require_bundle,
    require_exact_bundle_entries,
    verify_manifest,
)
from profile_contract import CHECK_IDS


def _non_negative_integers(value, names):
    return isinstance(value, dict) and all(
        type(value.get(name)) is int and value[name] >= 0 for name in names
    )


def validate_supported_bundle(bundle, pending, receipt):
    bundle = require_bundle(bundle)
    require_exact_bundle_entries(
        bundle, {*EVIDENCE_FILES, MANIFEST_NAME, PENDING_NAME}, {FINAL_NAME},
    )
    manifest = verify_manifest(bundle)
    values = {name: read_json_file(bundle / name) for name in EVIDENCE_FILES}
    if read_json_file(bundle / PENDING_NAME) != pending:
        raise ManifestError("bundled pending evidence differs from the reviewed input")
    if values["provider-inventory.json"] != receipt:
        raise ManifestError("review receipt differs from manifested provider-inventory.json")
    expected_environment = {"schemaVersion": 1, **pending["environment"]}
    if values["environment.json"] != expected_environment:
        raise ManifestError("manifested environment differs from pending evidence")
    summary = values["qualification-summary.json"]
    valid_summary = isinstance(summary, dict) and summary.get("schemaVersion") == 1 \
        and summary.get("outcome") == "passed"
    valid_summary = valid_summary and summary.get("image") == {
        "repository": "redacted", "digest": pending["artefacts"]["imageDigest"],
    }
    valid_summary = valid_summary and summary.get("checks") == {
        "networkPolicy": "enforced", "plaintextServiceExposure": "closed",
    }
    measurement = summary.get("measurements", {}) if isinstance(summary, dict) else {}
    valid_summary = valid_summary and _non_negative_integers(
        measurement, ("installToFirstValidExplanationSeconds",),
    )
    if not valid_summary:
        raise ManifestError("manifested qualification summary does not prove a passing run")
    recovery = values["recovery.json"]
    recovery_names = (
        "installToFirstValidExplanationSeconds", "agentRestartRecoverySeconds",
        "collectorRestartRecoverySeconds", "nodeReplacementRecoverySeconds",
    )
    if not isinstance(recovery, dict) or set(recovery) != {"schemaVersion", *recovery_names} \
            or recovery.get("schemaVersion") != 1 or not _non_negative_integers(recovery, recovery_names):
        raise ManifestError("manifested recovery evidence is invalid")
    if recovery["installToFirstValidExplanationSeconds"] \
            != measurement["installToFirstValidExplanationSeconds"]:
        raise ManifestError("summary and recovery installation measurements differ")
    lifecycle = values["lifecycle.json"]
    if not isinstance(lifecycle, dict) or set(lifecycle) != {"schemaVersion", "releaseIdentity", "checks"} \
            or lifecycle.get("schemaVersion") != 1:
        raise ManifestError("manifested lifecycle evidence fields are invalid")
    expected_identity = {
        name: pending["artefacts"][name]
        for name in ("imageDigest", "chartDigest", "valuesDigest", "probeImageDigest", "sourceCommit")
    }
    if lifecycle["releaseIdentity"] != expected_identity:
        raise ManifestError("manifested lifecycle release identity differs from pending evidence")
    lifecycle_checks = lifecycle["checks"]
    if not isinstance(lifecycle_checks, dict) or set(lifecycle_checks) != set(CHECK_IDS):
        raise ManifestError("manifested lifecycle check set is incomplete")
    required_passes = set(CHECK_IDS) - {"mixedOSScheduling"}
    if any(not isinstance(lifecycle_checks[name], dict) \
           or lifecycle_checks[name].get("passed") is not True for name in required_passes):
        raise ManifestError("manifested lifecycle contains a non-passing required check")
    doctor = values["doctor.json"]
    checks = doctor.get("checks") if isinstance(doctor, dict) else None
    mapping = doctor.get("mapping", {}) if isinstance(doctor, dict) else {}
    nodes = doctor.get("nodes") if isinstance(doctor, dict) else None
    valid_doctor = isinstance(doctor, dict) and doctor.get("connection") == "redacted" \
        and isinstance(checks, list) and checks
    valid_doctor = valid_doctor and all(
        isinstance(check, dict) and check.get("status") == "pass" for check in checks
    )
    valid_doctor = valid_doctor and isinstance(nodes, list) \
        and len(nodes) == pending["environment"]["linuxNodeCount"]
    valid_doctor = valid_doctor and _non_negative_integers(
        mapping, ("containers", "mapped", "unmapped"),
    ) and mapping["containers"] > 0 and mapping["mapped"] == mapping["containers"] \
        and mapping["unmapped"] == 0
    if not valid_doctor:
        raise ManifestError("manifested strict doctor evidence is invalid")
    readiness = lifecycle_checks["readiness"]
    valid_readiness = readiness.get("linuxNodes") == pending["environment"]["linuxNodeCount"] \
        and readiness.get("desiredAgents") == readiness.get("linuxNodes") \
        and readiness.get("readyAgents") == readiness.get("desiredAgents") \
        and readiness.get("collectorReady") is True
    collection = lifecycle_checks["collection"]
    valid_collection = collection.get("cgroupVersion") == pending["environment"]["cgroupVersion"] \
        and collection.get("doctorChecks") == len(checks) \
        and collection.get("mappedContainers") == mapping["mapped"]
    mixed = lifecycle_checks["mixedOSScheduling"]
    expected_mixed = "pass" if pending["environment"]["windowsNodeCount"] > 0 else "not_run"
    valid_mixed = mixed.get("passed") is True and mixed.get("result") == expected_mixed \
        and mixed.get("linuxNodes") == pending["environment"]["linuxNodeCount"] \
        and mixed.get("windowsNodes") == pending["environment"]["windowsNodeCount"]
    if not valid_readiness or not valid_collection or not valid_mixed:
        raise ManifestError("manifested lifecycle environment facts are invalid")
    for name, measurement_name in (
        ("agentRestart", "agentRestartRecoverySeconds"),
        ("collectorRestart", "collectorRestartRecoverySeconds"),
        ("nodeReplacement", "nodeReplacementRecoverySeconds"),
    ):
        if lifecycle_checks[name].get("recoverySeconds") != recovery[measurement_name]:
            raise ManifestError("manifested lifecycle recovery measurement differs")
    network = lifecycle_checks["networkPolicy"]
    prerequisites = lifecycle_checks["prerequisites"]
    install = lifecycle_checks["helmInstall"]
    mounts = lifecycle_checks["mounts"]
    security = lifecycle_checks["securityContext"]
    api = lifecycle_checks["api"]
    tui = lifecycle_checks["tui"]
    upgrade = lifecycle_checks["upgrade"]
    uninstall = lifecycle_checks["uninstall"]
    strict_facts = (
        prerequisites.get("profileCanonical") is True and prerequisites.get("sourceClean") is True
        and prerequisites.get("providerReceiptBound") is True and install.get("revision") == 1
        and network.get("cniName") == pending["environment"]["cniName"]
        and network.get("servicePort") == 443 and network.get("allowedControls") == 2
        and network.get("deniedControls") == 1 and network.get("deniedProbeExecuted") is True
        and mounts.get("cgroupPath") == "/sys/fs/cgroup" and mounts.get("readOnly") is True
        and mounts.get("projectedToken") is True and security.get("nonRoot") is True
        and security.get("readOnlyRootFilesystem") is True
        and security.get("privilegeEscalation") is False
        and security.get("capabilitiesDropped") is True and api.get("statusHealthy") is True
        and api.get("explanationMetadata") is True and api.get("metricsAvailable") is True
        and tui.get("columns") == 80 and tui.get("rows") == 24 and tui.get("cleanExit") is True
        and upgrade.get("upgradeRevision") == 2 and upgrade.get("rollbackRevision") == 3
        and upgrade.get("postRollbackDoctor") is True
        and lifecycle_checks["nodeReplacement"].get("providerReceiptRefreshed") is True
        and uninstall.get("clusterResourcesRemaining") == 0 and uninstall.get("namespaceRemoved") is True
    )
    if not strict_facts:
        raise ManifestError("manifested lifecycle strict facts are invalid")
    status = values["status.json"]
    connection = status.get("connection", {}) if isinstance(status, dict) else {}
    store = status.get("store", {}) if isinstance(status, dict) else {}
    data = status.get("data", {}) if isinstance(status, dict) else {}
    valid_status = isinstance(status, dict) and connection.get("collector") == "redacted" \
        and connection.get("description") == "redacted" and connection.get("healthy") is True
    valid_status = valid_status and status.get("error") in (None, "") \
        and type(store.get("nodeRecords")) is int \
        and store["nodeRecords"] == pending["environment"]["linuxNodeCount"] \
        and data.get("status") == "healthy"
    if not valid_status:
        raise ManifestError("manifested status evidence is invalid")
    if manifest["manifestDigest"] != pending["artefacts"]["evidenceManifestDigest"]:
        raise ManifestError("manifest digest differs from pending evidence")
    if manifest["probeImageDigest"] != pending["artefacts"]["probeImageDigest"]:
        raise ManifestError("manifest probe image digest differs from pending evidence")
    verify_manifest(bundle)
    return manifest
