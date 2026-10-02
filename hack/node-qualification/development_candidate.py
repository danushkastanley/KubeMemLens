"""Verify explicitly pinned unpublished inputs without granting release authority."""

from datetime import datetime
import json
import os
from pathlib import Path
import re
import tempfile

from candidate_manifest import MAX_BINARY, cli_archive_digest, snapshot
from common import ContractError, DIGEST, exact, require
from oci_binary import inspect_archive
from prepare_provider import local_command
from process import execute
from provider_plan import file_digest
from source_digest import production_source

PLATFORMS = {"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"}
MANIFEST = "development-manifest.json"
SIGNATURE = "development-manifest.sigstore.json"


def pairs(items):
    result = {}
    for key, value in items:
        require(key not in result, "duplicate development manifest field")
        result[key] = value
    return result


def manifest(data, source_commit):
    try:
        value = json.loads(data, object_pairs_hook=pairs,
                           parse_constant=lambda _: require(False, "non-finite development manifest"))
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ContractError("invalid development manifest") from error
    exact(value, {"schemaVersion", "authority", "sourceCommit", "sourceTreeDigest", "version", "buildDate",
                  "imageDigest", "chart", "imageArchive", "cliArchives"}, "development manifest")
    require(type(value["schemaVersion"]) is int and value["schemaVersion"] == 1
            and value["authority"] == "local-development" and value["version"] == "dev",
            "unpublished development authority is required")
    require(value["sourceCommit"] == source_commit and re.fullmatch(r"[a-f0-9]{40}", source_commit),
            "development source differs from the proposal")
    for name in ("sourceTreeDigest", "imageDigest"):
        require(isinstance(value[name], str) and DIGEST.fullmatch(value[name]), "invalid development digest")
    require(isinstance(value["buildDate"], str) and re.fullmatch(
        r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2})", value["buildDate"]), "invalid development build date")
    require(isinstance(value["cliArchives"], dict) and {"linux_amd64", "linux_arm64"} <= value["cliArchives"].keys()
            and value["cliArchives"].keys() <= PLATFORMS, "development CLI platform inventory differs")
    names = set()
    for item, maximum in [(value["chart"], 20 << 20), (value["imageArchive"], 4 << 30)] + [
            (item, MAX_BINARY) for item in value["cliArchives"].values()]:
        exact(item, {"name", "size", "sha256"}, "development payload")
        require(isinstance(item["name"], str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", item["name"])
                and item["name"] not in {MANIFEST, SIGNATURE, "verification.pub", "cache"} and item["name"] not in names,
                "development payload name is unsafe or duplicated")
        require(type(item["size"]) is int and 0 < item["size"] <= maximum
                and isinstance(item["sha256"], str) and DIGEST.fullmatch(item["sha256"]), "invalid development payload bound")
        names.add(item["name"])
    return value


def verify_signature(private, tool, tool_digest, trust, trust_digest, run):
    require(file_digest(tool, 256 << 20) == tool_digest and os.access(tool, os.X_OK), "development verifier identity differs")
    key = private / "verification.pub"
    require(snapshot(Path(trust), key, 16384) == trust_digest, "development trust key differs")
    raw = key.read_bytes()
    require(b"-----BEGIN PUBLIC KEY-----" in raw and b"PRIVATE" not in raw, "public development verification key required")
    # This explicit local signature has no public transparency claim. The
    # existing signed-RC verifier never uses this development-only option.
    run([str(tool), "verify-blob", "--timeout=30s", "--bundle", str(private / SIGNATURE),
         "--key", str(key), "--insecure-ignore-tlog=true", str(private / MANIFEST)],
        timeout=35, maximum=65536,
        environment={"PATH": "/usr/bin:/bin", "TMPDIR": str(private),
                     "XDG_CACHE_HOME": str(private / "cache"), "GOMAXPROCS": "2"})


def copy_payload(root, private, entry):
    source, destination = root / entry["name"], private / entry["name"]
    require(source.stat().st_size == entry["size"]
            and snapshot(source, destination, entry["size"]) == entry["sha256"], "development payload bytes differ")
    return destination


def verify(bundle, args, host, run=execute):
    configuration = bundle.configuration
    root = Path(args.development_bundle)
    require(root.is_absolute() and root.is_dir() and not root.is_symlink(), "absolute development bundle required")
    require(host in PLATFORMS, "unsupported development host platform")
    for value in (args.development_manifest_digest, args.development_key_digest, args.cosign_digest):
        require(isinstance(value, str) and DIGEST.fullmatch(value), "independently pinned development digests required")
    tool, key = Path(args.cosign), Path(args.development_key)
    require(tool.is_absolute() and key.is_absolute() and not tool.is_symlink(), "absolute caller-owned verifier and key required")
    with tempfile.TemporaryDirectory(prefix="node-development-") as temporary:
        private = Path(temporary)
        signed = snapshot(root / MANIFEST, private / MANIFEST, 65536)
        require(signed == args.development_manifest_digest, "development manifest differs from approved pin")
        snapshot(root / SIGNATURE, private / SIGNATURE, 4 << 20)
        value = manifest((private / MANIFEST).read_bytes(), configuration["sourceCommit"])
        require(host in value["cliArchives"], "development host CLI is missing")
        expected = {MANIFEST, SIGNATURE, value["chart"]["name"], value["imageArchive"]["name"],
                    *(item["name"] for item in value["cliArchives"].values())}
        seen = set()
        for path in root.iterdir():
            require(path.name in expected and len(seen) < len(expected), "undeclared development bundle file")
            seen.add(path.name)
        require(seen == expected, "development bundle files missing")
        verify_signature(private, tool, args.cosign_digest, key, args.development_key_digest, run)
        source_tree = production_source(configuration["sourceCommit"])
        require(source_tree == value["sourceTreeDigest"], "development source tree differs")
        committed = int(local_command(["git", "show", "-s", "--format=%ct", configuration["sourceCommit"]]).decode().strip())
        built = datetime.fromisoformat(value["buildDate"].replace("Z", "+00:00"))
        require(built.tzinfo is not None and built.timestamp() == committed, "development build date differs from source")
        require(value["imageDigest"] == configuration["imageDigest"], "development image differs from proposal")
        chart = copy_payload(root, private, value["chart"])
        require(file_digest(chart, 20 << 20) == configuration["chartDigest"]
                == file_digest(configuration["chartArchive"], 20 << 20), "development chart differs from proposal")
        archives = {platform: copy_payload(root, private, entry) for platform, entry in value["cliArchives"].items()}
        cli_digests = {platform: cli_archive_digest(path) for platform, path in archives.items()}
        cli = cli_digests[host]
        require(cli == configuration["cliDigest"] == file_digest(configuration["cliBinary"], MAX_BINARY),
                "development host CLI differs from proposal")
        image = copy_payload(root, private, value["imageArchive"])
        images = {architecture: inspect_archive(image, value["imageDigest"], value["version"],
                  value["sourceCommit"], value["buildDate"], architecture) for architecture in ("amd64", "arm64")}
        for architecture, proof in images.items():
            require(proof["binaries"]["kubectl-memlens"] == cli_digests["linux_" + architecture],
                    "development image and CLI archive disagree")
        selected = images[args.architecture]
        require(selected["binaries"]["memlens-node-context"] == configuration["producerDigest"]
                == file_digest(configuration["producerBinary"], MAX_BINARY), "development producer differs from proposal")
    candidate = {"sourceCommit": configuration["sourceCommit"], "manifestDigest": signed,
                 "imageDigest": value["imageDigest"], "chartDigest": configuration["chartDigest"],
                 "cliDigest": cli, "hostPlatform": host}
    return {"authority": "local-development", "trustKeyDigest": args.development_key_digest,
            "candidate": candidate, "image": selected, "sourceTreeDigest": source_tree,
            "releaseQualificationGranted": False}
