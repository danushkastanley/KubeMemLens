"""Verify a published idle bundle and replay its bounded numeric evidence."""

import argparse
import gzip
import io
import json
from pathlib import Path
import tempfile

from export_idle import bounded, numeric_samples, public_metadata, selected_names
from idle_evaluate import exact, integer, require, strict_json
from local_runtime import canonical, digest
from replay_idle import replay


def decode(directory, name, entry):
    exact(entry, {"bytes", "sha256", "rawBytes", "rawSHA256", "encoding"})
    integer(entry["bytes"], 1, 16 * 1024 * 1024)
    limit = 512 * 1024
    if name.endswith(".jsonl.gz"):
        limit = 8 * 1024 * 1024
    if name == "measurement-source.json.gz":
        limit = 16 * 1024 * 1024
    integer(entry["rawBytes"], 1, limit)
    encoded = bounded(directory / name, entry["bytes"])
    require(len(encoded) == entry["bytes"] and digest(encoded) == entry["sha256"], "encoded evidence digest mismatch")
    require(entry["encoding"] == ("gzip" if name.endswith(".gz") else "identity"), "unexpected evidence encoding")
    raw = encoded
    if entry["encoding"] == "gzip":
        with gzip.GzipFile(fileobj=io.BytesIO(encoded)) as stream:
            raw = stream.read(entry["rawBytes"] + 1)
    require(len(raw) == entry["rawBytes"] and digest(raw) == entry["rawSHA256"], "raw evidence digest mismatch")
    return raw


def verify(directory, expected_source_sha256, expected_freeze_sha256):
    manifest = strict_json(bounded(directory / "manifest.json", 128 * 1024))
    exact(manifest, {"schemaVersion", "profile", "windowCount", "measurementSourceSHA256", "freezeSHA256", "replaySourceSHA256", "files"})
    require(type(manifest["schemaVersion"]) is int and manifest["schemaVersion"] == 1 and
            manifest["profile"] == "local-idle-v1" and type(manifest["windowCount"]) is int and
            manifest["windowCount"] == 10, "unknown export format")
    require(manifest["measurementSourceSHA256"] == expected_source_sha256, "unexpected measurement source")
    require(manifest["freezeSHA256"] == expected_freeze_sha256, "unexpected frozen candidate metadata")
    names = {name + ".gz" if name.endswith(".jsonl") else name for name in selected_names()}
    names |= {"measurement-source.json.gz", "replay-result.json"}
    exact(manifest["files"], names)
    data = {name: decode(directory, name, entry) for name, entry in manifest["files"].items()}
    with tempfile.TemporaryDirectory(prefix="kml-idle-verify-") as temporary:
        root = Path(temporary)
        for name in selected_names():
            encoded_name = name + ".gz" if name.endswith(".jsonl") else name
            (root / name).write_bytes(data[encoded_name])
        frozen = strict_json(data["freeze.json"])
        public_metadata(frozen)
        require(frozen["sourceSHA256"] == expected_source_sha256 and
                digest(canonical(frozen["source"])) == expected_source_sha256, "source manifest mismatch")
        source = strict_json(data["measurement-source.json.gz"])
        exact(source, frozen["source"])
        for name, content in source.items():
            relative = Path(name)
            require(not relative.is_absolute() and ".." not in relative.parts and
                    (name.startswith("hack/ebpf-qualification/") or
                     name.startswith("prototype/trace/qualification/measure/")), "unapproved source path")
            require(type(content) is str, "source content is not text")
            raw = content.encode("utf-8")
            require(len(raw) <= 128 * 1024 and digest(raw) == frozen["source"][name], "source file mismatch")
            target = root / "source" / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(raw)
        for pair in range(1, 6):
            for phase in ("control", "enabled"):
                numeric_samples(data[f"pair-{pair}-{phase}.jsonl.gz"], phase == "enabled")
        result = replay(root, expected_source_sha256, expected_freeze_sha256=expected_freeze_sha256)
        archived = strict_json(data["replay-result.json"])
        require(archived["replaySourceSHA256"] == manifest["replaySourceSHA256"], "archived verifier identity mismatch")
        for field in ("pairs", "idleBudgetPassed", "measurementSourceSHA256", "freezeSHA256", "qualification"):
            require(archived[field] == result[field], "published result differs from replay")
        return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--expected-source-sha256", required=True)
    parser.add_argument("--expected-freeze-sha256", required=True)
    args = parser.parse_args()
    print(json.dumps(verify(args.directory, args.expected_source_sha256, args.expected_freeze_sha256), indent=2))
