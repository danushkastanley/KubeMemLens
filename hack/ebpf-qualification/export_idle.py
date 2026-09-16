"""Export a complete validated idle experiment without private runtime inputs."""

import argparse
import gzip
import json
import re
from pathlib import Path
import tempfile

from idle_evaluate import exact, require, strict_json, validate_window
from local_runtime import canonical, digest
from replay_idle import replay

CPU_KEYS = set("burst_usec nice_usec nr_bursts nr_periods nr_throttled system_usec throttled_usec usage_usec user_usec".split())
MEMORY_KEYS = set("""active_anon active_file anon anon_thp file file_dirty file_mapped file_thp
file_writeback inactive_anon inactive_file kernel kernel_stack pagetables percpu pgactivate
pgdeactivate pgdemote_direct pgdemote_khugepaged pgdemote_kswapd pgdemote_proactive pgfault
pglazyfree pglazyfreed pgmajfault pgrefill pgscan pgscan_direct pgscan_khugepaged pgscan_kswapd
pgscan_proactive pgsteal pgsteal_direct pgsteal_khugepaged pgsteal_kswapd pgsteal_proactive
pswpin pswpout sec_pagetables shmem shmem_thp slab slab_reclaimable slab_unreclaimable sock
swapcached swpin_zero swpout_zero thp_collapse_alloc thp_fault_alloc thp_swpout thp_swpout_fallback
unevictable vmalloc workingset_activate_anon workingset_activate_file workingset_nodereclaim
workingset_refault_anon workingset_refault_file workingset_restore_anon workingset_restore_file
zswap zswapped zswpin zswpout zswpwb""".split())
EVENT_KEYS = set("high low max oom oom_group_kill oom_kill sock_throttled".split())


def bounded(path, maximum):
    require(path.is_file() and not path.is_symlink(), "missing or symlinked evidence file")
    with path.open("rb") as stream:
        data = stream.read(maximum + 1)
    require(len(data) <= maximum, "evidence exceeds its byte bound")
    return data


def numeric_samples(data, enabled):
    rows = [strict_json(line) for line in data.splitlines()]
    validate_window(rows, enabled)
    for row in rows:
        for group in row["groups"].values():
            for name, allowed in (("cpu", CPU_KEYS), ("memory", MEMORY_KEYS), ("memoryEvents", EVENT_KEYS)):
                require(set(group[name]) <= allowed, "unreviewed counter name in public evidence")


def selected_names():
    names = ["freeze.json", "transitions.json"]
    for pair in range(1, 6):
        names.append(f"pair-{pair}-result.json")
        for phase in ("control", "enabled"):
            names += [f"pair-{pair}-{phase}.envelope.json", f"pair-{pair}-{phase}.jsonl"]
    return names


def snapshot(directory, destination, expected_source_sha256, expected_freeze_sha256):
    # Validate and export this immutable copy, never re-read mutable originals
    # after validation. Only this explicit allowlist can enter the public bundle.
    data = {}
    for name in selected_names():
        maximum = 8 * 1024 * 1024 if name.endswith(".jsonl") else 512 * 1024
        data[name] = bounded(directory / name, maximum)
        if name.endswith(".json"):
            strict_json(data[name])
        (destination / name).write_bytes(data[name])
    require(digest(data["freeze.json"]) == expected_freeze_sha256, "frozen candidate metadata pin mismatch")
    frozen = strict_json(data["freeze.json"])
    require(type(frozen["source"]) is dict and 1 <= len(frozen["source"]) <= 64, "unbounded source inventory")
    require(frozen["sourceSHA256"] == expected_source_sha256 and
            digest(canonical(frozen["source"])) == expected_source_sha256, "measurement source pin mismatch")
    sources = {}
    for name, expected in frozen["source"].items():
        relative = Path(name)
        require(not relative.is_absolute() and ".." not in relative.parts and
                (name.startswith("hack/ebpf-qualification/") or
                 name.startswith("prototype/trace/qualification/measure/")), "unapproved source path")
        path = directory / "source" / relative
        require(path.resolve().is_relative_to((directory / "source").resolve()), "source path escapes evidence")
        raw = bounded(path, 128 * 1024)
        require(digest(raw) == expected, "source digest changed")
        target = destination / "source" / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(raw)
        sources[name] = raw.decode("utf-8")
    for pair in range(1, 6):
        for phase in ("control", "enabled"):
            require(bounded(directory / f"pair-{pair}-{phase}.stderr", 4096) == b"",
                    "diagnostics require explicit privacy review before export")
            numeric_samples(data[f"pair-{pair}-{phase}.jsonl"], phase == "enabled")
    return data, sources


def write_file(directory, name, raw, compressed=False):
    path = directory / name
    encoded = gzip.compress(raw, compresslevel=9, mtime=0) if compressed else raw
    with path.open("xb") as stream:
        stream.write(encoded)
    return {"bytes": len(encoded), "sha256": digest(encoded),
            "rawBytes": len(raw), "rawSHA256": digest(raw), "encoding": "gzip" if compressed else "identity"}


def public_metadata(frozen):
    hashes = {"nodeSHA256", "workerSHA256", "measureSHA256", "censusSHA256", "policySHA256",
              "sourceSHA256", "engineSHA256", "programmeIndexSHA256", "profileSHA256", "privateConfigurationSHA256"}
    exact(frozen, hashes | {"image", "candidateCommit", "frozenAt", "profile", "source", "environment"})
    for field in hashes | {"candidateCommit"}:
        length = 40 if field == "candidateCommit" else 64
        require(type(frozen[field]) is str and re.fullmatch("[a-f0-9]{" + str(length) + "}", frozen[field]) is not None,
                "invalid public artifact identity")
    environment = frozen["environment"]
    exact(environment, {"kernelVersion", "osImage", "containerRuntimeVersion", "kubeletVersion",
                        "architecture", "operatingSystem", "sharedKindKernel"})
    require(environment["operatingSystem"] == "linux" and environment["architecture"] in ("arm64", "amd64") and
            environment["sharedKindKernel"] is True, "unreviewed execution environment")
    for field in ("kernelVersion", "osImage", "containerRuntimeVersion", "kubeletVersion"):
        require(type(environment[field]) is str and re.fullmatch(r"[A-Za-z0-9 .():/+_-]{1,96}", environment[field]) is not None,
                "unreviewed environment metadata")


def export(directory, destination, expected_source_sha256, expected_freeze_sha256):
    require(not destination.exists(), "export destination already exists")
    with tempfile.TemporaryDirectory(prefix="kml-idle-export-") as temporary:
        root = Path(temporary)
        data, sources = snapshot(directory, root, expected_source_sha256, expected_freeze_sha256)
        result = replay(root, expected_source_sha256, expected_freeze_sha256=expected_freeze_sha256)
        # Replay validates source identity, numeric windows, ownership, candidate,
        # chronology and exact saved results. Metadata extras are never copied.
        frozen = strict_json(data["freeze.json"])
        public_metadata(frozen)
        destination.mkdir(mode=0o755, parents=True)
        entries = {}
        for name, raw in data.items():
            compressed = name.endswith(".jsonl")
            public_name = name + ".gz" if compressed else name
            entries[public_name] = write_file(destination, public_name, raw, compressed)
        entries["measurement-source.json.gz"] = write_file(destination, "measurement-source.json.gz", canonical(sources), True)
        entries["replay-result.json"] = write_file(destination, "replay-result.json", canonical(result))
        manifest = {"schemaVersion": 1, "profile": "local-idle-v1", "windowCount": 10,
                    "measurementSourceSHA256": result["measurementSourceSHA256"],
                    "freezeSHA256": expected_freeze_sha256,
                    "replaySourceSHA256": result["replaySourceSHA256"], "files": entries}
        write_file(destination, "manifest.json", canonical(manifest))
        return manifest


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--expected-source-sha256", required=True)
    parser.add_argument("--expected-freeze-sha256", required=True)
    args = parser.parse_args()
    print(json.dumps(export(args.directory, args.destination, args.expected_source_sha256, args.expected_freeze_sha256), indent=2))
