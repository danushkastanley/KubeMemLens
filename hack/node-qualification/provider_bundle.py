"""Revalidate a private proposal before any provider or Kubernetes command."""

import hashlib
import json
import stat
from dataclasses import dataclass
from pathlib import Path

from common import digest, exact, instant, load, require, utc_now
from observer_specs import ephemeral_observer, host_observer, host_policy
from prepare_provider import REPOSITORY, local_command
from provider_plan import file_digest, serving_ca, validate_config, values
from provider_source import bind_files
from provider_probes import identities, pod as probe_pod
from verify_chart_archive import verify_archive, verify_chart_metadata
from workload import deployment

PLAN_KEYS = {"schemaVersion", "state", "qualified", "providerRunStarted", "preparedAt", "profile",
             "configurationDigest", "qualificationToolCommit", "toolSourceDirty", "servingTrustDigest",
             "rendered", "files", "measurement", "budgets", "observation", "verificationStillRequired",
             "cleanupRequirements", "planDigest"}
FILES = {"baseline-values.json", "enabled-values.json", "baseline.preview.yaml", "enabled.preview.yaml",
         "serving-trust.json", "workload.json", "host-observers.json", "ephemeral-observers.json",
         "probe-identities.json", "probe-pods.preview.json", "configuration.private.json"}


@dataclass(frozen=True)
class Bundle:
    directory: Path
    profile: dict
    configuration: dict
    plan: dict


def private_file(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_mode & 0o077 == 0, "proposal files must be private regular files")


def validate_bundle(directory, profile, acknowledged_digest, repository=REPOSITORY, command=local_command, now=None):
    root = Path(directory)
    require(root.is_absolute() and not root.is_symlink() and root.is_dir()
            and root.stat().st_mode & 0o077 == 0, "proposal directory must be absolute and private")
    require({p.name for p in root.iterdir()} == FILES | {"plan.private.json"}, "proposal file set changed")
    for name in FILES | {"plan.private.json"}:
        private_file(root / name)
    plan = load(root / "plan.private.json")
    exact(plan, PLAN_KEYS, "provider plan")
    require(type(plan["schemaVersion"]) is int and plan["schemaVersion"] == 2
            and plan["state"] == "prepared-not-approved" and plan["qualified"] is False
            and plan["providerRunStarted"] is False and plan["toolSourceDirty"] is False,
            "a clean, unexecuted version-2 proposal is required")
    require(plan["planDigest"] == acknowledged_digest == digest(plan, "planDigest"), "acknowledgement does not bind this proposal")
    require(instant(plan["preparedAt"]) <= (now or utc_now()), "proposal preparation time is in the future")
    exact(plan["files"], FILES, "proposal file digests")
    for name, expected in plan["files"].items():
        require(file_digest(root / name, 2 * 1024 * 1024) == expected, "proposal file bytes changed")
    c = validate_config(profile, load(root / "configuration.private.json"))
    require(plan["configurationDigest"] == digest(c, "configurationDigest"), "proposal configuration changed")
    require(plan["profile"] == {"id": profile["id"], "digest": profile["profileDigest"]}, "proposal profile changed")
    for key in ("measurement", "budgets"):
        require(plan[key] == profile[key], "proposal measurement protocol changed")
    require(plan["observation"] == {"method": "kubernetes-probes-v1", "image": profile["workload"]["image"]},
            "proposal observation protocol changed")
    head = command(["git", "rev-parse", "HEAD"], repository).decode().strip()
    require(head == plan["qualificationToolCommit"], "qualification tool changed since preparation")
    require(not command(["git", "status", "--porcelain", "--untracked-files=all"], repository).strip(),
            "qualification requires a clean checkout")
    relative = "hack/node-qualification/profiles/" + profile["id"] + ".json"
    require(profile == load(repository / relative), "qualification requires a canonical profile")
    for commit, path in ((head, relative), (head, "hack/provider-profiles/" + c["inventoryProfile"] + ".json"),
                         (c["sourceCommit"], "charts/kube-memlens"),
                         (c["sourceCommit"], "hack/provider-values/" + c["inventoryProfile"] + ".yaml")):
        bind_files(repository, commit, path, command)
    verify_archive(c["chartArchive"], repository / "charts/kube-memlens")
    verify_chart_metadata(c["chartArchive"], repository / "charts/kube-memlens")
    validate_resources(root, profile, c, plan)
    return Bundle(root, profile, c, plan)


def validate_resources(root, profile, config, plan):
    for phase in ("baseline", "enabled"):
        require(load(root / (phase + "-values.json")) == values(profile, config, phase == "enabled"),
                "proposal values differ from the fixed generator")
    exact(plan["rendered"], {"baselinePreviewDigest", "enabledPreviewDigest"}, "preview digests")
    for phase in ("baseline", "enabled"):
        require(plan["rendered"][phase + "PreviewDigest"] == plan["files"][phase + ".preview.yaml"], "preview digest mismatch")
    ca = serving_ca(config["kubeletCAFile"])
    require(plan["servingTrustDigest"] == "sha256:" + hashlib.sha256(ca).hexdigest(), "serving trust changed")
    namespace = config["namespace"]
    trust = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "node-context-trust", "namespace": namespace},
             "data": {"ca.crt": ca.decode("ascii")}}
    # PEM text belongs only in this private file, not in the short-string public
    # evidence decoder. Compare the generator's exact bounded encoding instead.
    expected_trust = (json.dumps(trust, indent=2, sort_keys=True, allow_nan=False) + "\n").encode()
    require((root / "serving-trust.json").read_bytes() == expected_trust, "proposal serving trust differs from configuration")
    selector = values(profile, config, True)["agent"]["nodeSelector"]
    image = profile["workload"]["image"]
    expected = {
        "workload.json": deployment(profile["workload"], namespace, {"nodeSelector": selector}),
        "host-observers.json": {"apiVersion": "v1", "kind": "List", "items": [host_policy(namespace), host_observer(namespace, image, selector)]},
        "ephemeral-observers.json": {component: ephemeral_observer(image, component) for component in ("agent", "node-context")},
        "probe-identities.json": {"apiVersion": "v1", "kind": "List", "items": identities(namespace)},
        "probe-pods.preview.json": {"reviewOnly": True, "items": [
            probe_pod(config, "<selected-node-0>", 0, case, "<selected-node-1>" if case == "wrong-node" else "<selected-node-0>")
            for case in ("allowed", "denied", "bad-ca", "wrong-node")]},
    }
    for name, document in expected.items():
        require(load(root / name) == document, "proposal resources differ from the fixed generator")
