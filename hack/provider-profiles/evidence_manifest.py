#!/usr/bin/env python3

import argparse
import hashlib
import json
import os
import re
import stat
import sys
from pathlib import Path

from privacy_contract import reject_sensitive_content


SCHEMA_VERSION = 1
MANIFEST_NAME = "evidence-manifest.json"
PENDING_NAME = "provider-qualification.pending.json"
FINAL_NAME = "provider-qualification.json"
EVIDENCE_FILES = (
    "doctor.json",
    "environment.json",
    "lifecycle.json",
    "provider-inventory.json",
    "qualification-summary.json",
    "recovery.json",
    "status.json",
)
MAX_EVIDENCE_FILE_BYTES = 2 * 1024 * 1024
MAX_MANIFEST_BYTES = 16 * 1024
SHA256_PATTERN = re.compile(r"sha256:[a-f0-9]{64}")
MANIFEST_KEYS = {"schemaVersion", "probeImageDigest", "files", "manifestDigest"}
FILE_KEYS = {"name", "digest"}


class ManifestError(ValueError):
    pass


def canonical_digest(value):
    content = dict(value)
    content.pop("manifestDigest", None)
    encoded = json.dumps(content, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()
    return "sha256:" + hashlib.sha256(encoded).hexdigest()


def require_digest(value, name):
    if not isinstance(value, str) or SHA256_PATTERN.fullmatch(value) is None:
        raise ManifestError(f"{name} must be an exact lowercase SHA-256 digest")


def require_bundle(bundle):
    path = Path(bundle)
    if path.is_symlink() or not path.is_dir():
        raise ManifestError("bundle must be a direct directory")
    return path


def require_exact_bundle_entries(bundle, required, optional=()):
    bundle = require_bundle(bundle)
    entries = list(bundle.iterdir())
    if any(entry.is_symlink() or not entry.is_file() for entry in entries):
        raise ManifestError("evidence bundle contains a non-regular entry")
    names = {entry.name for entry in entries}
    required = set(required)
    optional = set(optional)
    if not required.issubset(names) or not names.issubset(required | optional):
        raise ManifestError("evidence bundle file set is incomplete or contains an unexpected file")


def _open_regular(path, max_bytes, display_name):
    try:
        before = path.lstat()
    except OSError as error:
        raise ManifestError(f"required file is unavailable: {display_name}") from error
    if stat.S_ISLNK(before.st_mode) or not stat.S_ISREG(before.st_mode):
        raise ManifestError(f"required file is not a direct regular file: {display_name}")
    if before.st_size <= 0 or before.st_size > max_bytes:
        raise ManifestError(f"required file size is outside the bound: {display_name}")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ManifestError(f"required file could not be opened safely: {display_name}") from error
    try:
        opened = os.fstat(descriptor)
    except OSError as error:
        os.close(descriptor)
        raise ManifestError(f"required file could not be inspected safely: {display_name}") from error
    identity = (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
    opened_identity = (opened.st_dev, opened.st_ino, opened.st_size, opened.st_mtime_ns)
    if identity != opened_identity or not stat.S_ISREG(opened.st_mode):
        os.close(descriptor)
        raise ManifestError(f"required file changed before verification: {display_name}")
    return descriptor, identity


def hash_file(path, max_bytes=MAX_EVIDENCE_FILE_BYTES):
    display_name = path.name
    descriptor, identity = _open_regular(path, max_bytes, display_name)
    digest = hashlib.sha256()
    total = 0
    try:
        while True:
            chunk = os.read(descriptor, 64 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > max_bytes:
                raise ManifestError(f"required file exceeded the size bound: {display_name}")
            digest.update(chunk)
        after_open = os.fstat(descriptor)
    except OSError as error:
        raise ManifestError(f"required file could not be read safely: {display_name}") from error
    finally:
        os.close(descriptor)
    try:
        after_path = path.lstat()
    except OSError as error:
        raise ManifestError(f"required file disappeared during verification: {display_name}") from error
    after_identity = (after_open.st_dev, after_open.st_ino, after_open.st_size, after_open.st_mtime_ns)
    path_identity = (after_path.st_dev, after_path.st_ino, after_path.st_size, after_path.st_mtime_ns)
    if total != identity[2] or identity != after_identity or identity != path_identity:
        raise ManifestError(f"required file changed during verification: {display_name}")
    return "sha256:" + digest.hexdigest()


def read_json_file(path, max_bytes=MAX_EVIDENCE_FILE_BYTES):
    descriptor, identity = _open_regular(path, max_bytes, path.name)
    chunks = []
    total = 0
    try:
        while True:
            chunk = os.read(descriptor, 64 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > max_bytes:
                raise ManifestError(f"required file exceeded the size bound: {path.name}")
            chunks.append(chunk)
        after_open = os.fstat(descriptor)
    except OSError as error:
        raise ManifestError(f"required file could not be read safely: {path.name}") from error
    finally:
        os.close(descriptor)
    after_path = path.lstat()
    after_identity = (after_open.st_dev, after_open.st_ino, after_open.st_size, after_open.st_mtime_ns)
    path_identity = (after_path.st_dev, after_path.st_ino, after_path.st_size, after_path.st_mtime_ns)
    if total != identity[2] or identity != after_identity or identity != path_identity:
        raise ManifestError(f"required file changed during verification: {path.name}")
    try:
        value = json.loads(b"".join(chunks).decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ManifestError(f"required file is not valid UTF-8 JSON: {path.name}") from error
    reject_sensitive_content(value, ManifestError)
    return value


def build_manifest(bundle, probe_image_digest):
    bundle = require_bundle(bundle)
    require_digest(probe_image_digest, "probe image digest")
    files = []
    for name in EVIDENCE_FILES:
        read_json_file(bundle / name)
        files.append({"name": name, "digest": hash_file(bundle / name)})
    manifest = {
        "schemaVersion": SCHEMA_VERSION,
        "probeImageDigest": probe_image_digest,
        "files": files,
    }
    manifest["manifestDigest"] = canonical_digest(manifest)
    return manifest


def validate_manifest(manifest):
    if not isinstance(manifest, dict) or set(manifest) != MANIFEST_KEYS:
        raise ManifestError("manifest fields do not match schema version 1")
    if type(manifest["schemaVersion"]) is not int or manifest["schemaVersion"] != SCHEMA_VERSION:
        raise ManifestError("manifest schemaVersion must be integer 1")
    require_digest(manifest["probeImageDigest"], "probe image digest")
    require_digest(manifest["manifestDigest"], "manifest digest")
    files = manifest["files"]
    if not isinstance(files, list) or len(files) != len(EVIDENCE_FILES):
        raise ManifestError("manifest must contain the exact evidence file set")
    names = []
    for entry in files:
        if not isinstance(entry, dict) or set(entry) != FILE_KEYS:
            raise ManifestError("manifest file fields are invalid")
        if not isinstance(entry["name"], str):
            raise ManifestError("manifest file name is invalid")
        names.append(entry["name"])
        require_digest(entry["digest"], "evidence file digest")
    if tuple(names) != EVIDENCE_FILES:
        raise ManifestError("manifest file names or order differ from the exact evidence set")
    if manifest["manifestDigest"] != canonical_digest(manifest):
        raise ManifestError("manifestDigest does not match canonical manifest content")


def _read_manifest(path):
    descriptor, identity = _open_regular(path, MAX_MANIFEST_BYTES, MANIFEST_NAME)
    chunks = []
    total = 0
    try:
        while True:
            chunk = os.read(descriptor, 4096)
            if not chunk:
                break
            total += len(chunk)
            if total > MAX_MANIFEST_BYTES:
                raise ManifestError("manifest exceeded the size bound")
            chunks.append(chunk)
        after_open = os.fstat(descriptor)
    except OSError as error:
        raise ManifestError("manifest could not be read safely") from error
    finally:
        os.close(descriptor)
    try:
        after_path = path.lstat()
    except OSError as error:
        raise ManifestError("manifest disappeared during verification") from error
    after_identity = (after_open.st_dev, after_open.st_ino, after_open.st_size, after_open.st_mtime_ns)
    path_identity = (after_path.st_dev, after_path.st_ino, after_path.st_size, after_path.st_mtime_ns)
    if total != identity[2] or identity != after_identity or identity != path_identity:
        raise ManifestError("manifest changed during verification")
    try:
        value = json.loads(b"".join(chunks).decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ManifestError("manifest is not valid UTF-8 JSON") from error
    validate_manifest(value)
    return value


def verify_manifest(bundle):
    bundle = require_bundle(bundle)
    manifest = _read_manifest(bundle / MANIFEST_NAME)
    expected = {entry["name"]: entry["digest"] for entry in manifest["files"]}
    for name in EVIDENCE_FILES:
        read_json_file(bundle / name)
        if hash_file(bundle / name) != expected[name]:
            raise ManifestError(f"evidence file digest changed: {name}")
    return manifest


def create_manifest(bundle, probe_image_digest):
    bundle = require_bundle(bundle)
    output = bundle / MANIFEST_NAME
    if output.exists() or output.is_symlink():
        raise ManifestError("refusing to overwrite evidence-manifest.json")
    manifest = build_manifest(bundle, probe_image_digest)
    encoded = (json.dumps(manifest, ensure_ascii=False, indent=2) + "\n").encode()
    try:
        descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as error:
        raise ManifestError("refusing to overwrite evidence-manifest.json") from error
    try:
        with os.fdopen(descriptor, "wb") as destination:
            destination.write(encoded)
        return verify_manifest(bundle)
    except ManifestError:
        output.unlink(missing_ok=True)
        raise
    except OSError as error:
        output.unlink(missing_ok=True)
        raise ManifestError("manifest could not be written safely") from error


def main():
    parser = argparse.ArgumentParser(description="Create or verify a strict provider evidence manifest.")
    subparsers = parser.add_subparsers(dest="command", required=True)
    create = subparsers.add_parser("create")
    create.add_argument("--bundle", required=True)
    create.add_argument("--probe-image-digest", required=True)
    verify = subparsers.add_parser("verify")
    verify.add_argument("--bundle", required=True)
    arguments = parser.parse_args()
    try:
        if arguments.command == "create":
            result = create_manifest(arguments.bundle, arguments.probe_image_digest)
        else:
            result = verify_manifest(arguments.bundle)
    except ManifestError as error:
        print(f"provider evidence manifest error: {error}", file=sys.stderr)
        return 2
    print(json.dumps({"schemaVersion": 1, "result": "pass", "manifestDigest": result["manifestDigest"]},
                     separators=(",", ":"), sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
