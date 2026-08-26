#!/usr/bin/env python3

import json

from evidence_manifest import create_manifest


def lifecycle_document(pending, measurement=12):
    environment = pending["environment"]
    artefacts = pending["artefacts"]
    mixed_result = "pass" if environment["windowsNodeCount"] > 0 else "not_run"
    return {
        "schemaVersion": 1,
        "releaseIdentity": {
            name: artefacts[name]
            for name in ("imageDigest", "chartDigest", "valuesDigest", "probeImageDigest", "sourceCommit")
        },
        "checks": {
            "prerequisites": {"passed": True, "profileCanonical": True, "sourceClean": True,
                              "providerReceiptBound": True},
            "helmInstall": {"passed": True, "revision": 1},
            "readiness": {"passed": True, "linuxNodes": environment["linuxNodeCount"],
                          "desiredAgents": environment["linuxNodeCount"],
                          "readyAgents": environment["linuxNodeCount"], "collectorReady": True},
            "mounts": {"passed": True, "cgroupPath": "/sys/fs/cgroup", "readOnly": True,
                       "projectedToken": True},
            "securityContext": {"passed": True, "nonRoot": True, "readOnlyRootFilesystem": True,
                                "privilegeEscalation": False, "capabilitiesDropped": True},
            "mixedOSScheduling": {"passed": True, "result": mixed_result,
                                  "linuxNodes": environment["linuxNodeCount"],
                                  "windowsNodes": environment["windowsNodeCount"]},
            "collection": {"passed": True, "cgroupVersion": environment["cgroupVersion"],
                           "doctorChecks": 1, "mappedContainers": 1},
            "api": {"passed": True, "statusHealthy": True, "explanationMetadata": True,
                    "metricsAvailable": True},
            "tui": {"passed": True, "columns": 80, "rows": 24, "cleanExit": True},
            "networkPolicy": {"passed": True, "cniName": environment["cniName"],
                              "servicePort": 443, "allowedControls": 2, "deniedControls": 1,
                              "deniedProbeExecuted": True},
            "agentRestart": {"passed": True, "recoverySeconds": 8},
            "collectorRestart": {"passed": True, "recoverySeconds": 9},
            "nodeReplacement": {"passed": True, "recoverySeconds": 20,
                                "providerReceiptRefreshed": True},
            "upgrade": {"passed": True, "upgradeRevision": 2, "rollbackRevision": 3,
                        "postRollbackDoctor": True},
            "uninstall": {"passed": True, "clusterResourcesRemaining": 0,
                          "namespaceRemoved": True},
        },
    }


def write_supported_evidence_files(root, pending, receipt):
    environment = pending["environment"]
    measurement = 12
    files = {
        "provider-inventory.json": receipt,
        "environment.json": {"schemaVersion": 1, **environment},
        "qualification-summary.json": {
            "schemaVersion": 1, "outcome": "passed", "completedAt": pending["completedAt"],
            "image": {"repository": "redacted", "digest": pending["artefacts"]["imageDigest"]},
            "checks": {"networkPolicy": "enforced", "plaintextServiceExposure": "closed"},
            "measurements": {"installToFirstValidExplanationSeconds": measurement},
            "caveats": ["Identifiers are deliberately omitted"],
        },
        "recovery.json": {
            "schemaVersion": 1, "installToFirstValidExplanationSeconds": measurement,
            "agentRestartRecoverySeconds": 8, "collectorRestartRecoverySeconds": 9,
            "nodeReplacementRecoverySeconds": 20,
        },
        "doctor.json": {
            "build": {}, "connection": "redacted",
            "checks": [{"name": "strict", "status": "pass", "summary": "verified"}],
            "nodes": [{"stale": False} for _ in range(environment["linuxNodeCount"])],
            "mapping": {"containers": 1, "mapped": 1, "unmapped": 0, "coverage": 1.0},
        },
        "status.json": {
            "connection": {"mode": "kubernetes-api", "collector": "redacted",
                           "healthy": True, "description": "redacted"},
            "store": {"nodeRecords": environment["linuxNodeCount"]},
            "data": {"status": "healthy"},
        },
        "lifecycle.json": lifecycle_document(pending, measurement),
    }
    for name, value in files.items():
        (root / name).write_text(json.dumps(value), encoding="utf-8")
    manifest = create_manifest(root, pending["artefacts"]["probeImageDigest"])
    pending["artefacts"]["evidenceManifestDigest"] = manifest["manifestDigest"]
    return manifest
