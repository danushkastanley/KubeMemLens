#!/usr/bin/env python3

import hashlib
import re
import subprocess
import sys
from pathlib import Path


HACK = Path(__file__).resolve().parent.parent
REPOSITORY = HACK.parent
sys.path.insert(0, str(HACK))

from verify_chart_archive import ArchiveError, verify_archive, verify_chart_metadata  # noqa: E402


DIGEST_PATTERN = re.compile(r"sha256:[a-f0-9]{64}")
COMMIT_PATTERN = re.compile(r"[a-f0-9]{40}")


def run(command):
    try:
        result = subprocess.run(command, check=False, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ValueError(f"{command[0]} artefact verification did not complete") from error
    if result.returncode != 0:
        raise ValueError(f"{command[0]} artefact verification failed")
    return result.stdout


def digest_file(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as source:
        while chunk := source.read(64 * 1024):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def verify_binding(source_commit, chart_archive, chart_digest):
    if COMMIT_PATTERN.fullmatch(source_commit or "") is None:
        raise ValueError("source commit must be 40 lowercase hexadecimal characters")
    if DIGEST_PATTERN.fullmatch(chart_digest or "") is None:
        raise ValueError("chart digest must be an exact lowercase SHA-256 digest")
    if run(["git", "rev-parse", "HEAD"]).strip() != source_commit:
        raise ValueError("source commit is not checked out")
    if run(["git", "status", "--porcelain", "--untracked-files=all"]).strip():
        raise ValueError("repository must be clean before observing unsupported evidence")
    archive = Path(chart_archive)
    if digest_file(archive) != chart_digest:
        raise ValueError("chart archive digest does not match")
    try:
        verify_archive(archive, REPOSITORY / "charts" / "kube-memlens")
        verify_chart_metadata(archive, REPOSITORY / "charts" / "kube-memlens")
    except ArchiveError as error:
        raise ValueError(str(error)) from error
    return {"sourceCommit": source_commit, "chartDigest": chart_digest}
