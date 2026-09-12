"""Assemble bounded public observations from private owned-kind measurements."""

import argparse
import hashlib
import json
import subprocess
from pathlib import Path

from common import digest, load, require, utc_text, write_new
from evidence import PRIVACY, validate_evidence
from evaluate import evaluate
from profiles import validate_profile
from source_summary import summarise
from window_contract import join_windows


def sha(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def assemble(root, profile):
    p = validate_profile(profile)
    source = load(root / "source.json")
    node_object = json.loads((root / "node.json").read_text())
    node = node_object["status"]["nodeInfo"]
    observation = json.loads((root / "allowed.log").read_text())["observation"]
    source_summary = summarise([observation], [node_object["metadata"]["name"]])
    baseline, enabled = [load(root / ("qualification-" + phase + ".json")) for phase in ("baseline", "enabled")]
    binding = {"id": p["id"], "digest": p["profileDigest"]}
    require(baseline["profile"] == binding and enabled["profile"] == binding,
            "measurement profile changed during the run")
    measurements = {"samples": {"baseline": baseline.get("samples"), "enabled": enabled.get("samples")},
                    "rotation": enabled.get("rotation")}
    values = (root / "qualification-values.json").read_bytes()
    if baseline.get("schemaVersion") == 2 or enabled.get("schemaVersion") == 2:
        measurements = join_windows(p, baseline, enabled)
        values += b"\0" + (root / "qualification-observer-settings.json").read_bytes()
    chart = b"".join(str(path).encode() + b"\0" + path.read_bytes() + b"\0"
                     for path in sorted(Path("charts/kube-memlens").rglob("*")) if path.is_file())
    e = {"schemaVersion": 1, "profile": {"id": p["id"], "digest": p["profileDigest"]},
         "startedAt": (root / "qualification-start").read_text(), "completedAt": utc_text(), "orchestration": "completed",
         "artefacts": {"sourceCommit": source["sourceCommit"],
                       "toolCommit": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
                       "sourceTreeDigest": "sha256:" + source["sourceTreeSHA256"],
                       "sourceDirty": source["sourceDirty"], "imageDigest": (root / "image-id").read_text().strip(),
                       "chartDigest": sha(chart), "cliDigest": sha((root / "image/kubectl-memlens").read_bytes()),
                       "producerDigest": sha((root / "image/producer").read_bytes()),
                       "valuesDigest": sha(values),
                       "trustDigest": sha((root / "serving-ca.crt").read_bytes()),
                       "audienceDigest": sha((root / "qualification-audience").read_bytes())},
         "environment": {"provider": "kind", "kubernetes": node["kubeletVersion"], "kernel": node["kernelVersion"],
                         "runtime": node["containerRuntimeVersion"], "nodeImage": p["nodeImage"], "osImage": node["osImage"],
                         "architecture": node["architecture"], "cgroupVersion": "v2", "cni": "kindnet",
                         "linuxNodes": 1, "providerReceiptDigest": None},
         "transport": {"result": "passed", "reason": "none", "directTLS": True, "podBoundIdentity": True,
                       "statsOnlyRBAC": True, "proxyAccess": False, "networkPolicy": "not-qualified", "servingTrust": "fixture-ca"},
         **source_summary,
         **measurements, "lifecycle": load(root / "qualification-lifecycle.json"),
         "cleanup": {"workloadsRemoved": False, "rbacRemoved": False, "cloudResources": "not-applicable"},
         "privacy": dict(PRIVACY)}
    e["recordDigest"] = digest(e, "recordDigest")
    return validate_evidence(p, e)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", required=True)
    parser.add_argument("--work-dir")
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--cleanup-confirmed", action="store_true")
    args = parser.parse_args()
    p, output = load(args.profile), Path(args.output_dir)
    if not args.cleanup_confirmed:
        write_new(output / "qualification-observations.json", assemble(Path(args.work_dir), p))
        return 0
    # The owner invokes this only after verifying cluster and image removal.
    e = load(output / "qualification-observations.json")
    e["cleanup"].update(workloadsRemoved=True, rbacRemoved=True)
    e["recordDigest"] = digest(e, "recordDigest")
    result = evaluate(p, e)
    write_new(output / "qualification.json", e)
    write_new(output / "qualification-evaluation.json", result)
    print("local qualification measurements: " + result["outcome"] + "; independent review pending")
    return 0 if result["outcome"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
