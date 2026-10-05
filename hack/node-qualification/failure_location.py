"""Identify the nearest qualification failure site without exporting error content."""

import hashlib
from pathlib import Path
import traceback

ROOT = Path(__file__).resolve().parent


def locate(error):
    frames = list(traceback.walk_tb(error.__traceback__))
    for frame, line in reversed(frames):
        try:
            source = Path(frame.f_code.co_filename).resolve()
            if source.parent != ROOT or source.suffix != ".py" or source.name == "common.py":
                continue
            with source.open("rb") as stream:
                raw = stream.read(512 * 1024 + 1)
        except (OSError, RuntimeError):
            return None
        if len(raw) > 512 * 1024 or not 1 <= line <= len(raw.splitlines()):
            return None
        return {"moduleDigest": "sha256:" + hashlib.sha256(raw).hexdigest(), "line": line}
    return None
