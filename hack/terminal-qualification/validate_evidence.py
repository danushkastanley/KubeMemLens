#!/usr/bin/env python3
"""Validate a published terminal qualification bundle."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path

EXPECTED_ROWS = {
    "apple-terminal",
    "ghostty-macos",
    "iterm2-macos",
    "warp-macos",
    "xterm-linux",
    "kitty-linux",
    "alacritty-linux",
    "tmux-linux",
    "ssh-linux",
}
QUALIFIED_ROWS = {"xterm-linux", "kitty-linux", "alacritty-linux", "tmux-linux", "ssh-linux"}
MACOS_ROWS = {"apple-terminal", "ghostty-macos", "iterm2-macos", "warp-macos"}
REQUIRED_SIZES = {"80x24", "120x30", "180x50"}
SHA256 = re.compile(r"^[a-f0-9]{64}$")
COMMIT = re.compile(r"^[a-f0-9]{40}$")
FORBIDDEN_TEXT = re.compile(
    r"/Users/|/tmp/|kind-kube|127\.0\.0\.1|client-certificate-data|client-key-data|BEGIN (?:OPENSSH |RSA |EC )?PRIVATE KEY",
    re.IGNORECASE,
)
MAX_JSON_BYTES = 1024 * 1024
MAX_PNG_BYTES = 8 * 1024 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("bundle", type=Path)
    return parser.parse_args()


def load_json(path: Path) -> dict[str, object]:
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"evidence must be a regular file: {path}")
    if path.stat().st_size > MAX_JSON_BYTES:
        raise ValueError(f"JSON evidence exceeds size bound: {path}")
    text = path.read_text(encoding="utf-8")
    if FORBIDDEN_TEXT.search(text):
        raise ValueError(f"JSON evidence contains private runtime data: {path}")
    value = json.loads(text)
    if not isinstance(value, dict):
        raise ValueError(f"JSON evidence must be an object: {path}")
    validate_privacy_flags(value, path)
    return value


def validate_privacy_flags(value: object, path: Path) -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            if key in {"rawTerminalOutputRetained", "credentialPathsRetained", "clusterIdentifiersRetained"} and item is not False:
                raise ValueError(f"privacy flag {key} must be false: {path}")
            validate_privacy_flags(item, path)
    elif isinstance(value, list):
        for item in value:
            validate_privacy_flags(item, path)


def relative_file(bundle: Path, value: object, suffix: str | None = None) -> Path:
    if not isinstance(value, str) or value.startswith("/") or ".." in Path(value).parts:
        raise ValueError(f"invalid evidence path: {value!r}")
    path = bundle / value
    if suffix and path.suffix.lower() != suffix:
        raise ValueError(f"unexpected evidence type: {value}")
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"referenced evidence is missing: {value}")
    return path


def validate_result(bundle: Path, value: object) -> dict[str, object]:
    path = relative_file(bundle, value, ".json")
    result = load_json(path)
    if result.get("outcome") != "passed":
        raise ValueError(f"terminal result did not pass: {value}")
    return result


def validate_png(bundle: Path, value: object) -> None:
    path = relative_file(bundle, value, ".png")
    size = path.stat().st_size
    if size < 5000 or size > MAX_PNG_BYTES:
        raise ValueError(f"PNG size is outside the evidence bound: {value}")
    with path.open("rb") as handle:
        if handle.read(8) != b"\x89PNG\r\n\x1a\n":
            raise ValueError(f"invalid PNG signature: {value}")


def validate_bundle(bundle: Path) -> dict[str, object]:
    if bundle.is_symlink() or not bundle.is_dir():
        raise ValueError("bundle must be a directory")
    matrix = load_json(bundle / "terminal-matrix.json")
    if matrix.get("schemaVersion") != 1:
        raise ValueError("terminal matrix schemaVersion must be 1")
    if not COMMIT.fullmatch(str(matrix.get("applicationSourceCommit", ""))):
        raise ValueError("applicationSourceCommit must be a full commit")
    binaries = matrix.get("binaries")
    if not isinstance(binaries, dict) or set(binaries) != {"darwinArm64Sha256", "linuxArm64Sha256"}:
        raise ValueError("terminal matrix must bind both candidate binaries")
    if not all(SHA256.fullmatch(str(value)) for value in binaries.values()):
        raise ValueError("candidate binary digest is invalid")

    rows = matrix.get("rows")
    if not isinstance(rows, list):
        raise ValueError("terminal matrix rows must be a list")
    by_id = {row.get("id"): row for row in rows if isinstance(row, dict)}
    if set(by_id) != EXPECTED_ROWS:
        raise ValueError("terminal matrix row set is incomplete")
    for row_id in QUALIFIED_ROWS:
        row = by_id[row_id]
        if row.get("outcome") != "qualified":
            raise ValueError(f"required Linux or remote row is not qualified: {row_id}")
        evidence = row.get("evidence")
        if not isinstance(evidence, list) or not evidence:
            raise ValueError(f"qualified row has no evidence: {row_id}")
        for value in evidence:
            validate_result(bundle, value)
    for row_id in MACOS_ROWS:
        if by_id[row_id].get("outcome") != "unqualified":
            raise ValueError(f"unexercised macOS row must remain unqualified: {row_id}")
    for row_id in {"xterm-linux", "kitty-linux", "alacritty-linux"}:
        if set(by_id[row_id].get("sizes", [])) != REQUIRED_SIZES:
            raise ValueError(f"emulator size matrix is incomplete: {row_id}")

    soak = validate_result(bundle, matrix.get("ptySoakEvidence"))
    if soak.get("run", {}).get("requestedDurationSeconds") != 1800 or not all(soak.get("checks", {}).values()):
        raise ValueError("PTY soak does not prove the 30-minute lifecycle")
    emulator_soak = validate_result(bundle, matrix.get("emulatorSoakEvidence"))
    if emulator_soak.get("run", {}).get("requestedDurationSeconds") != 1800:
        raise ValueError("emulator soak does not prove 30 minutes")
    smoke = validate_result(bundle, matrix.get("liveSmokeEvidence"))
    if set(smoke.get("terminalSizesCoveredByLivePTY", [])) != REQUIRED_SIZES:
        raise ValueError("live PTY smoke size matrix is incomplete")

    screenshots = matrix.get("screenshots")
    if not isinstance(screenshots, dict) or set(screenshots) != REQUIRED_SIZES:
        raise ValueError("representative screenshots must cover all supported sizes")
    for value in screenshots.values():
        validate_png(bundle, value)
    digest_path = bundle / "bundle.sha256"
    if digest_path.is_symlink() or not digest_path.is_file():
        raise ValueError("bundle.sha256 is missing")
    if digest_path.read_text(encoding="ascii").strip() != bundle_digest(bundle):
        raise ValueError("bundle.sha256 does not match the evidence bundle")
    return matrix


def bundle_digest(bundle: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(path for path in bundle.rglob("*") if path.is_file() and path.name != "bundle.sha256"):
        digest.update(path.relative_to(bundle).as_posix().encode())
        digest.update(b"\0")
        digest.update(path.read_bytes())
    return digest.hexdigest()


def main() -> int:
    args = parse_args()
    try:
        validate_bundle(args.bundle)
    except (OSError, UnicodeError, json.JSONDecodeError, ValueError) as error:
        print(f"terminal evidence validation failed: {error}", file=sys.stderr)
        return 1
    print(f"terminal evidence validation passed: {bundle_digest(args.bundle)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
