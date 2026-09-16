"""Recalculate all five idle pairs and verify their frozen evidence bindings."""

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re

from idle_evaluate import EXPECTED_PROFILE, evaluate_pair, exact, integer, read_samples, require, strict_json
from local_runtime import canonical, digest
from idle_campaign import source_manifest


def document_bytes(path):
    with path.open("rb") as stream:
        raw = stream.read(512 * 1024 + 1)
    require(len(raw) <= 512 * 1024, "envelope exceeds bound")
    return raw


def load(path):
    return strict_json(document_bytes(path))


def date(value):
    result = datetime.fromisoformat(value)
    require(result.tzinfo is not None, "timestamp lacks timezone")
    return result


def nanos(value):
    delta = date(value) - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (delta.days * 86400 + delta.seconds) * 10**9 + delta.microseconds * 1000


def idle_proof(value):
    exact(value, {"identity", "snapshot"})
    require(re.fullmatch("[a-f0-9]{64}", value["identity"]) is not None, "invalid instance proof")
    snapshot = value["snapshot"]
    exact(snapshot, {"workers", "excludedWorkers", "activeControls", "objects", "kernelMapBytes", "userMapBytes", "clock"})
    for field in ("workers", "excludedWorkers", "activeControls", "kernelMapBytes", "userMapBytes"):
        require(type(snapshot[field]) is int and snapshot[field] == 0, "non-idle or unknown owned state")
    exact(snapshot["objects"], {"link", "map", "prog"})
    require(all(v == [] for v in snapshot["objects"].values()), "residual owned objects")
    exact(snapshot["clock"], {"monotonicNanos", "wallNanos", "uncertaintyNanos"})
    integer(snapshot["clock"]["monotonicNanos"], 1)
    integer(snapshot["clock"]["wallNanos"], 1)
    integer(snapshot["clock"]["uncertaintyNanos"], 0, 5_000_000)


def replay(directory, expected_source_sha256=None, *, expected_freeze_sha256):
    raw_freeze = document_bytes(directory / "freeze.json")
    require(type(expected_freeze_sha256) is str and re.fullmatch("[a-f0-9]{64}", expected_freeze_sha256) is not None,
            "invalid expected freeze pin")
    require(digest(raw_freeze) == expected_freeze_sha256, "frozen candidate metadata pin mismatch")
    frozen = strict_json(raw_freeze)
    require(frozen["profile"] == EXPECTED_PROFILE, "changed protocol")
    replay_source = digest(canonical(source_manifest()))
    expected_source = replay_source if expected_source_sha256 is None else expected_source_sha256
    require(re.fullmatch("[a-f0-9]{64}", expected_source) is not None, "invalid expected source pin")
    require(frozen["sourceSHA256"] == expected_source, "measurement source does not match independently supplied pin")
    require(digest(canonical(frozen["source"])) == frozen["sourceSHA256"], "source manifest changed")
    for relative, expected in frozen["source"].items():
        path = Path(relative)
        require(not path.is_absolute() and ".." not in path.parts, "invalid source path")
        require(digest((directory / "source" / path).read_bytes()) == expected, "frozen source changed")
    require(digest((directory / "source/hack/ebpf-qualification/idle_profile.json").read_bytes()) == frozen["profileSHA256"],
            "profile identity changed")
    require(re.fullmatch(r"[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}", frozen["image"]) is not None, "image is not immutable")
    transitions = load(directory / "transitions.json")
    require(len(transitions) == 10, "missing startup/teardown transitions")
    pairs = []
    previous_end = date(frozen["frozenAt"])
    previous_raw_end = 0
    seen_samples = set()
    previous_lifetimes = {}
    for pair in range(1, 6):
        windows = {}
        envelopes = {}
        for phase in ("control", "enabled"):
            stem = f"pair-{pair}-{phase}"
            envelope = load(directory / (stem + ".envelope.json"))
            exact(envelope, {"startedAt", "completedAt", "phase", "pair", "samplesSHA256", "image",
                             "sourceSHA256", "before", "after", "optionalServicesAbsent"})
            require(envelope["pair"] == pair and envelope["phase"] == phase, "wrong pair or phase")
            require(envelope["image"] == frozen["image"] and envelope["sourceSHA256"] == frozen["sourceSHA256"], "candidate/source changed")
            started, completed = date(envelope["startedAt"]), date(envelope["completedAt"])
            require(previous_end <= started < completed and 900 <= (completed - started).total_seconds() <= 930,
                    "window duration or ordering mismatch")
            previous_end = completed
            transition = transitions[(pair - 1) * 2 + (phase == "enabled")]
            require(transition["pair"] == pair and transition["phase"] == ("start" if phase == "enabled" else "stop"), "transition mismatch")
            require(date(transition["startedAt"]) <= date(transition["completedAt"]) <= started, "transition reversed")
            require((started - date(transition["completedAt"])).total_seconds() >= 60, "warm-up shortened")
            roles = {"node", "api"} if phase == "enabled" else set()
            exact(envelope["before"], roles)
            exact(envelope["after"], roles)
            require(envelope["optionalServicesAbsent"] is (phase == "control"), "control presence proof missing")
            for role in roles:
                idle_proof(envelope["before"][role])
                idle_proof(envelope["after"][role])
                require(envelope["before"][role]["identity"] == envelope["after"][role]["identity"], "process lifetime changed")
            path = directory / (stem + ".jsonl")
            require(digest(path.read_bytes()) == envelope["samplesSHA256"], "sample digest mismatch")
            require(envelope["samplesSHA256"] not in seen_samples, "raw window reused")
            seen_samples.add(envelope["samplesSHA256"])
            windows[phase] = read_samples(path)
            envelopes[phase] = envelope
        result = evaluate_pair(windows["control"], windows["enabled"])
        for phase in ("control", "enabled"):
            rows, envelope = windows[phase], envelopes[phase]
            first, last = rows[0]["wallNanos"], rows[-1]["wallNanos"]
            require(nanos(envelope["startedAt"]) - 100_000_000 <= first < last <=
                    nanos(envelope["completedAt"]) + 100_000_000, "raw epoch does not match command envelope")
            require(first > previous_raw_end, "raw windows overlap or were relabelled")
            previous_raw_end = last
            for role in envelope["before"]:
                before, after = envelope["before"][role], envelope["after"][role]
                require(before["identity"] != previous_lifetimes.get(role), "service lifetime reused across replacement")
                previous_lifetimes[role] = before["identity"]
                require(before["snapshot"]["clock"]["wallNanos"] <= first + 100_000_000 and
                        after["snapshot"]["clock"]["wallNanos"] >= last - 100_000_000,
                        "ownership census does not bracket measurement")
        result["pair"] = pair
        require(result == load(directory / f"pair-{pair}-result.json"), "saved pair differs from recomputation")
        pairs.append(result)
    return {"pairs": pairs, "idleBudgetPassed": all(p["idleBudgetPassed"] for p in pairs),
            "measurementSourceSHA256": frozen["sourceSHA256"], "replaySourceSHA256": replay_source,
            "freezeSHA256": expected_freeze_sha256,
            "qualification": "idle-only; independent review and remaining gates not established"}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--expected-source-sha256", help="independently recorded historical measurement source pin")
    parser.add_argument("--expected-freeze-sha256", required=True, help="independently recorded candidate metadata pin")
    args = parser.parse_args()
    print(json.dumps(replay(args.directory, args.expected_source_sha256, expected_freeze_sha256=args.expected_freeze_sha256), indent=2))
