"""Assemble provider observations without granting cleanup or review approval."""

import copy
import hashlib

from common import digest, require, utc_text
from evidence import PRIVACY, validate_evidence
from review import bind_receipt
from source_summary import summarise


def values_digest(bundle):
    # Bind both phases and every probe/observer to the exact reviewed private
    # files. Their content and identifiers do not enter the public record.
    return digest({"files": bundle.plan["files"], "observation": bundle.plan["observation"]}, "valuesDigest")


def execution_values_digest(execution):
    value = values_digest(execution.bundle)
    if execution.replacement is None:
        return value
    return digest({"initialValuesDigest": value, "replacement": execution.replacement}, "valuesDigest")


def bind_artifacts(execution, proof):
    bundle, configuration = execution.bundle, execution.bundle.configuration
    candidate, image = proof["candidate"], proof["image"]
    for key in ("sourceCommit", "imageDigest", "chartDigest", "cliDigest"):
        require(candidate[key] == configuration[key], "candidate evidence differs from the execution")
    require(image["imageDigest"] == configuration["imageDigest"]
            and image["architecture"] == execution.binding["runtime"]["architecture"]
            and image["binaries"]["memlens-node-context"] == configuration["producerDigest"],
            "producer evidence differs from the execution")
    require(execution.image_proof == image, "execution used a different image proof")
    require(set(execution.image_checks) == {"baseline", "enabled"}, "both live image verification phases are required")
    count = bundle.profile["workload"]["linuxNodes"]
    for phase, pods in (("baseline", count + 1), ("enabled", 2 * count + 1)):
        require(execution.image_checks[phase] == {"imageDigest": configuration["imageDigest"],
                "architecture": image["architecture"], "checkedPods": pods, "allMatched": True},
                "live image verification does not cover every component")
    require(bundle.plan["toolSourceDirty"] is False, "provider evidence requires clean qualification tools")
    return {"sourceCommit": configuration["sourceCommit"], "toolCommit": bundle.plan["qualificationToolCommit"],
            "sourceTreeDigest": proof["sourceTreeDigest"], "imageDigest": configuration["imageDigest"],
            "chartDigest": configuration["chartDigest"], "cliDigest": configuration["cliDigest"],
            "producerDigest": configuration["producerDigest"], "valuesDigest": execution_values_digest(execution),
            "trustDigest": bundle.plan["servingTrustDigest"],
            "audienceDigest": "sha256:" + hashlib.sha256(configuration["kubeletAudience"].encode()).hexdigest(),
            "sourceDirty": False}


def network_state(result, count):
    if result is None:
        return "not-qualified"
    expected = {"allowedBefore": True, "blocked": True, "allowedAfter": True}
    require(result.get("method") == "controlled-ingress-producer-egress-v1"
            and result.get("qualified") is False and result.get("ownNodeIsolation") == "not-claimed",
            "network evidence uses a different observation protocol")
    nodes = result.get("nodes", [])
    passed = (result.get("passed") is True and result.get("freshSourceRetained") is True
              and nodes == [{"slot": slot, "ingress": expected, "egress": expected} for slot in range(count)])
    return "passed" if passed else "failed"


def assemble(execution, proof, receipt, started_at, lifecycle, network, outcome, completed_at=None):
    p, b = execution.bundle.profile, execution.initial_binding
    require(p["profileClass"] == "provider", "provider records require a provider profile")
    measured = execution.measurements()
    require(measured["schemaVersion"] == 2 and len(measured["nodes"]) == p["workload"]["linuxNodes"],
            "provider records require measurements from every Node")
    count = p["workload"]["linuxNodes"]
    require(len(execution.observations) == count, "provider transport probes do not cover every Node")
    summary = summarise(execution.observations, [n["name"] for n in b["nodes"]])
    replacement = execution.replacement
    if lifecycle["providerNodeReplacement"]["state"] == "passed":
        require(replacement is not None and "liveImages" in replacement and "sourceSummary" in replacement,
                "passing replacement requires its bound observations")
        images = replacement["liveImages"]
        require(images == {"imageDigest": proof["image"]["imageDigest"], "architecture": b["runtime"]["architecture"],
                           "checkedPods": 2 * count + 1, "allMatched": True}, "replacement images are incomplete")
        after = replacement["sourceSummary"]
        summary = {"fields": {key: "available" if value == after["fields"][key] == "available" else "unreported"
                              for key, value in summary["fields"].items()},
                   "provenance": summary["provenance"] if summary["provenance"] == after["provenance"] else "unknown"}
    e = {"schemaVersion": 2, "profile": {"id": p["id"], "digest": p["profileDigest"]},
         "startedAt": started_at, "completedAt": completed_at or utc_text(), "orchestration": outcome,
         "artefacts": bind_artifacts(execution, proof),
         "environment": {"provider": p["provider"], **b["runtime"], "nodeImage": receipt["nodeImage"],
                         "cni": receipt["cniName"], "cgroupVersion": "v2", "linuxNodes": count,
                         "providerReceiptDigest": receipt["receiptDigest"]},
         "transport": {"result": "passed", "reason": "none", "directTLS": True, "podBoundIdentity": True,
                       "statsOnlyRBAC": True, "proxyAccess": False, "servingTrust": "provider-verified",
                       "networkPolicy": network_state(network, count)},
         **summary,
         **copy.deepcopy(measured), "lifecycle": copy.deepcopy(lifecycle),
         "cleanup": {"workloadsRemoved": False, "rbacRemoved": False, "cloudResources": "pending"},
         "privacy": dict(PRIVACY)}
    e["recordDigest"] = digest(e, "recordDigest")
    validate_evidence(p, e)
    bind_receipt(e, receipt)
    return e


def cleanup_cluster(execution, evidence):
    """Remove only this execution's owned resources; provider cleanup stays pending."""
    validate_evidence(execution.bundle.profile, evidence)
    require(evidence["artefacts"]["valuesDigest"] == execution_values_digest(execution)
            and evidence["artefacts"]["toolCommit"] == execution.bundle.plan["qualificationToolCommit"],
            "cleanup evidence belongs to a different execution")
    require(evidence["cleanup"] == {"workloadsRemoved": False, "rbacRemoved": False, "cloudResources": "pending"},
            "cluster cleanup requires an unconfirmed observation record")
    require(execution.installer.namespace in execution.ownership.owned, "execution has no owned namespace")
    execution.cleanup()
    result = copy.deepcopy(evidence)
    result["cleanup"].update(workloadsRemoved=True, rbacRemoved=True)
    result["recordDigest"] = digest(result, "recordDigest")
    return validate_evidence(execution.bundle.profile, result)
