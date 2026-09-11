"""Bounded primitives shared by Node qualification profiles and evidence."""

import hashlib
import json
import math
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "provider-profiles"))
from privacy_contract import reject_sensitive_content  # noqa: E402

MAX_BYTES = 512 * 1024
DIGEST = re.compile(r"^sha256:[a-f0-9]{64}$")
COMMIT = re.compile(r"^[a-f0-9]{40}$")


class ContractError(ValueError):
    pass


def exact(value, keys, label):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ContractError(f"{label} fields do not match schema")


def number(value, maximum=None):
    return type(value) in (int, float) and 0 <= value <= 2**63 - 1 and math.isfinite(value) and (maximum is None or value <= maximum)


def integer(value, minimum, maximum):
    return type(value) is int and minimum <= value <= maximum


def require(condition, message):
    if not condition:
        raise ContractError(message)


def choice(value, allowed, message):
    require(isinstance(value, str) and value in allowed, message)


def digest(value, field):
    content = dict(value)
    content.pop(field, None)
    data = json.dumps(content, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
    return "sha256:" + hashlib.sha256(data).hexdigest()


def instant(value):
    require(isinstance(value, str) and re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z", value), "timestamp must use UTC second precision")
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ContractError("invalid UTC timestamp") from error


def utc_now():
    return datetime.now(timezone.utc).replace(microsecond=0)


def utc_text():
    return utc_now().strftime("%Y-%m-%dT%H:%M:%SZ")


def bounded(value, depth=0):
    require(depth <= 12, "qualification nesting limit exceeded")
    if isinstance(value, dict):
        require(len(value) <= 64, "qualification object limit exceeded")
        for key, child in value.items():
            require(isinstance(key, str) and len(key) <= 64, "invalid qualification field")
            bounded(child, depth + 1)
    elif isinstance(value, list):
        require(len(value) <= 128, "qualification array limit exceeded")
        for child in value:
            bounded(child, depth + 1)
    elif isinstance(value, str):
        require(len(value) <= 256 and value.isascii() and all(32 <= ord(c) <= 126 for c in value), "invalid qualification text")
    elif type(value) is float:
        require(math.isfinite(value), "non-finite qualification number")


def privacy(value):
    bounded(value)
    reject_sensitive_content(value, ContractError)


def _object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate qualification field")
        result[key] = value
    return result


def load(path):
    try:
        with Path(path).open("rb") as source:
            data = source.read(MAX_BYTES + 1)
        require(len(data) <= MAX_BYTES, "qualification file exceeds byte limit")
        value = json.loads(data, object_pairs_hook=_object, parse_constant=lambda _: (_ for _ in ()).throw(ContractError("invalid JSON number")))
        bounded(value)
        return value
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise ContractError("cannot read bounded qualification JSON") from error


def write_new(path, value):
    import os
    import tempfile
    data = (json.dumps(value, indent=2, sort_keys=True, allow_nan=False) + "\n").encode()
    require(len(data) <= MAX_BYTES, "qualification output exceeds byte limit")
    fd, temporary = tempfile.mkstemp(prefix=".node-evidence-", dir=Path(path).parent)
    try:
        with os.fdopen(fd, "wb") as target:
            target.write(data)
            target.flush()
            os.fsync(target.fileno())
        os.link(temporary, path)
    finally:
        os.unlink(temporary)
