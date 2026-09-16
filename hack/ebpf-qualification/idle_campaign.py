"""Run the frozen five-pair local idle experiment; never infer full qualification."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import time

from idle_evaluate import evaluate_pair, load_profile, read_samples, validate_window
from local_runtime import MEASURE, SERVICES, Runtime, canonical, digest

ROOT = Path(__file__).resolve().parents[2]
PROFILE = Path(__file__).with_name("idle_profile.json")


def instant():
    return datetime.now(timezone.utc).isoformat()


def source_files():
    files = []
    for directory in (ROOT / "hack/ebpf-qualification", ROOT / "prototype/trace/qualification/measure"):
        files += [p for p in directory.iterdir() if p.is_file() and p.suffix in {".go", ".py", ".json", ".md"}]
    return sorted(files)


def source_manifest():
    return {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in source_files()}


def write(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, indent=2)


class Campaign:
    def __init__(self, cfg, output):
        self.cfg, self.output = cfg, output
        self.profile = load_profile(PROFILE)
        self.source = source_manifest()
        if digest(canonical(self.source)) != cfg["sourceSHA256"]:
            raise ValueError("source differs from the predeclared manifest")
        self.runtime = Runtime(cfg)
        self.runtime.policy()
        self.output.mkdir(mode=0o700)
        for source in source_files():
            target = self.output / "source" / source.relative_to(ROOT)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, target)
        fields = ("image", "nodeSHA256", "workerSHA256", "measureSHA256", "censusSHA256",
                  "policySHA256", "sourceSHA256", "engineSHA256", "programmeIndexSHA256", "candidateCommit")
        frozen = {k: cfg[k] for k in fields}
        frozen.update({"frozenAt": instant(), "profile": self.profile, "source": self.source,
                       "profileSHA256": digest(PROFILE.read_bytes()), "privateConfigurationSHA256": digest(canonical(cfg))})
        node = self.runtime.json(["get", "node", cfg["node"], "-o", "json"])["status"]["nodeInfo"]
        frozen["environment"] = {k: node[k] for k in ("kernelVersion", "osImage", "containerRuntimeVersion", "kubeletVersion", "architecture", "operatingSystem")}
        frozen["environment"]["sharedKindKernel"] = True
        write(self.output / "freeze.json", frozen)

    def progress(self, state, pair):
        path = self.output / "progress.json"
        temporary = self.output / "progress.tmp"
        temporary.write_text(json.dumps({"state": state, "pair": pair, "updatedAt": instant()}))
        temporary.replace(path)

    def invariant(self):
        if source_manifest() != self.source:
            raise ValueError("measurement source changed during the experiment")
        self.runtime.verify_tools()
        self.runtime.policy()

    def services(self):
        return {role: self.runtime.service(role) for role in SERVICES}

    def window(self, pair, enabled, previous):
        phase = "enabled" if enabled else "control"
        self.invariant()
        before = self.services() if enabled else {}
        if not enabled:
            self.runtime.stopped(previous)
        self.progress(phase + "-sampling", pair)
        stem = f"pair-{pair}-{phase}"
        config = {"seconds": 900, "groups": [s["group"] for s in before.values()]}
        remote = f"/tmp/kml-idle-{self.cfg['sourceSHA256'][:16]}-{stem}.json"
        # Fixed shell program; the path is a positional argument, never shell text.
        self.runtime.exec(["sh", "-ec", "umask 077; set -C; cat > \"$1\"", "--", remote], canonical(config))
        target = self.output / (stem + ".jsonl")
        started = instant()
        try:
            with target.open("xb") as stream, (self.output / (stem + ".stderr")).open("xb") as errors:
                result = subprocess.run(["docker", "exec", self.cfg["node"], MEASURE, "--config", remote],
                                        stdout=stream, stderr=errors, timeout=930)
            if result.returncode:
                raise RuntimeError("sampler failed; raw incomplete window retained")
        finally:
            self.runtime.exec(["rm", "--", remote])
        completed = instant()
        self.invariant()
        after = self.services() if enabled else {}
        if not enabled:
            self.runtime.stopped(previous)
        if enabled and any(before[r]["identity"] != after[r]["identity"] for r in SERVICES):
            raise ValueError("service instance changed within the window")
        rows = read_samples(target)
        validate_window(rows, enabled)
        envelope = {"startedAt": started, "completedAt": completed, "phase": phase, "pair": pair,
                    "samplesSHA256": digest(target.read_bytes()), "image": self.cfg["image"],
                    "sourceSHA256": self.cfg["sourceSHA256"],
                    "before": {r: {k: s[k] for k in ("identity", "snapshot")} for r, s in before.items()},
                    "after": {r: {k: s[k] for k in ("identity", "snapshot")} for r, s in after.items()},
                    "optionalServicesAbsent": not enabled}
        write(self.output / (stem + ".envelope.json"), envelope)
        return rows

    def run(self):
        pairs = []
        transitions = []
        try:
            for pair in range(1, 6):
                self.invariant()
                previous = self.services()
                started = instant()
                self.runtime.scale("api", 0)
                self.runtime.scale("node", 0)
                self.runtime.wait_absent()
                self.runtime.stopped(previous)
                transitions.append({"pair": pair, "phase": "stop", "startedAt": started, "completedAt": instant()})
                self.progress("control-warmup", pair)
                time.sleep(60)
                control = self.window(pair, False, previous)
                started = instant()
                self.runtime.scale("node", 1)
                self.runtime.scale("api", 1)
                self.runtime.ready()
                transitions.append({"pair": pair, "phase": "start", "startedAt": started, "completedAt": instant()})
                self.progress("enabled-warmup", pair)
                time.sleep(60)
                enabled = self.window(pair, True, previous)
                result = evaluate_pair(control, enabled)
                result["pair"] = pair
                pairs.append(result)
                write(self.output / f"pair-{pair}-result.json", result)
            result = {"schemaVersion": 1, "pairs": pairs, "idleBudgetPassed": all(p["idleBudgetPassed"] for p in pairs),
                      "qualification": "idle-only; other performance and review gates remain unqualified"}
            write(self.output / "idle-result.json", result)
            self.progress("idle-complete", 5)
        except BaseException:
            self.progress("interrupted-or-invalid; evidence retained", len(pairs) + 1)
            raise
        finally:
            write(self.output / "transitions.json", transitions)
            # Restore only the same immutable owned deployments. Do not overwrite
            # concurrent changes if an invariant no longer holds.
            for role in SERVICES:
                if self.runtime.deployment(role)["spec"]["replicas"] == 0:
                    self.runtime.scale(role, 1)
            self.runtime.ready()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--acknowledge-local-idle-campaign", action="store_true", required=True)
    args = parser.parse_args()
    os.umask(0o077)
    if args.config.stat().st_size > 16384:
        parser.error("configuration exceeds bound")
    cfg = json.loads(args.config.read_text())
    def interrupted(_signal, _frame):
        raise InterruptedError("campaign interrupted; restore owned optional services")
    signal.signal(signal.SIGTERM, interrupted)
    Campaign(cfg, args.output).run()


if __name__ == "__main__":
    main()
