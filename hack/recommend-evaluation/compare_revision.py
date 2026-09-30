"""Evaluate one corpus and runner against base and current production rules."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
HARNESS = Path("hack/recommend-evaluation")


def compare(before, after):
    for field in ("schemaVersion", "taxonomyVersion", "corpusSHA256", "sources"):
        if before[field] != after[field]:
            raise ValueError("reports do not use the same evaluation contract")
    old = {case["id"]: case for case in before["cases"]}
    new = {case["id"]: case for case in after["cases"]}
    if len(old) != len(before["cases"]) or len(new) != len(after["cases"]) or old.keys() != new.keys():
        raise ValueError("case inventory changed")
    fields = ("diagnosisMatched", "confidenceMatched", "abstentionMatched", "abstained",
              "falsePositives", "falseNegatives", "prohibitedAdvice", "unreviewedAdvice",
              "missingSafetyGuard", "unsupportedEvidence", "copyMatched", "passed")
    changes = []
    for key in sorted(old):
        changed = {field: {"before": old[key][field], "after": new[key][field]}
                   for field in fields if old[key][field] != new[key][field]}
        if changed:
            changes.append({"id": key, "changes": changed})
    return {"schemaVersion": 1, "corpusSHA256": after["corpusSHA256"],
            "beforePassed": before["passed"], "afterPassed": after["passed"],
            "changes": changes, "rulesBefore": before["rules"], "rulesAfter": after["rules"]}


def evaluate(directory, output, name):
    target = output / (name + ".json")
    with target.open("xb") as stdout, (output / (name + ".stderr")).open("xb") as stderr:
        result = subprocess.run(["go", "run", "./" + str(HARNESS)], cwd=directory,
                                stdout=stdout, stderr=stderr, timeout=120)
    # A failing evaluation still emits a complete report. Compilation/runtime
    # failures lack that report and must fail this comparison, never become zero.
    if target.stat().st_size > 1 << 20:
        raise ValueError("evaluation output exceeds bound")
    report = json.loads(target.read_bytes())
    if result.returncode != 0 and report["passed"]:
        raise ValueError("failed process claimed a passing evaluation")
    return report


def run(base, output):
    if not re.fullmatch(r"[0-9a-f]{40}", base):
        raise ValueError("an exact base commit is required")
    subprocess.run(["git", "merge-base", "--is-ancestor", base, "HEAD"], cwd=ROOT, check=True)
    output.mkdir(mode=0o700)
    # A public-source archive, not a Git checkout; no private workspace files.
    with tempfile.TemporaryDirectory(prefix="kml-recommend-evaluation-") as temp:
        work = Path(temp)
        archive = work / "base.tar"
        subprocess.run(["git", "archive", "--format=tar", "--output", str(archive), base],
                       cwd=ROOT, check=True)
        reference = work / "base"
        reference.mkdir()
        with tarfile.open(archive) as bundle:
            bundle.extractall(reference, filter="data")
        if (reference / HARNESS).exists():
            shutil.rmtree(reference / HARNESS)  # Only this owned temporary archive.
        shutil.copytree(ROOT / HARNESS, reference / HARNESS,
                        ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        files = sorted(p for p in (ROOT / HARNESS).rglob("*")
                       if p.is_file() and "__pycache__" not in p.parts)
        manifest = {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
        before = evaluate(reference, output, "before")
        after = evaluate(ROOT, output, "after")
        result = compare(before, after)
        result["baseCommit"] = base
        result["headCommit"] = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        result["harnessSource"] = manifest
        (output / "comparison.json").write_text(json.dumps(result, indent=2) + "\n")
        if not result["afterPassed"]:
            raise ValueError("current recommendation gate failed; evidence retained")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    result = run(args.base, args.output.resolve())
    print(f"Evaluated the same corpus against both revisions; {len(result['changes'])} case changes.")
