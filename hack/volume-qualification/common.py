"""Bounded, identity-free local volume qualification documents."""
import hashlib
import json
import math
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'provider-profiles'))
from privacy_contract import reject_sensitive_content

MAX_BYTES = 512 * 1024

class Invalid(ValueError):
    pass

# Compatibility name required by the reused read-only kind observer.
ContractError = Invalid

def require(condition, message):
    if not condition:
        raise Invalid(message)

def exact(value, fields, name):
    require(type(value) is dict and set(value) == set(fields.split()), name + ' fields differ from schema')

def number(value):
    return type(value) in (int, float) and math.isfinite(value) and 0 <= value <= 2**53

def integer(value, low, high):
    return type(value) is int and low <= value <= high

def digest(value):
    return 'sha256:' + hashlib.sha256(canonical(value)).hexdigest()

def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()

def sha(value):
    return isinstance(value, str) and re.fullmatch(r'sha256:[a-f0-9]{64}', value) is not None

def instant(value):
    require(isinstance(value, str) and re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z', value), 'invalid UTC time')
    try:
        return datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError as error:
        raise Invalid('invalid UTC time') from error

def now():
    return datetime.now(timezone.utc).isoformat(timespec='microseconds').replace('+00:00', 'Z')

def bounded(value, depth=0):
    require(depth <= 16, 'document nesting limit exceeded')
    if isinstance(value, dict):
        require(len(value) <= 64, 'object field limit exceeded')
        for key, child in value.items():
            require(isinstance(key, str) and len(key) <= 64, 'invalid field name')
            bounded(child, depth + 1)
    elif isinstance(value, list):
        require(len(value) <= 128, 'array limit exceeded')
        for child in value:
            bounded(child, depth + 1)
    elif isinstance(value, str):
        require(re.search(r'\bvol-[a-f0-9]{8,32}\b', value, re.IGNORECASE) is None, 'backend volume identity in evidence')
        require(len(value) <= 512 and value.isascii() and all(32 <= ord(c) <= 126 for c in value), 'invalid text')
    elif type(value) in (int, float):
        require(number(value), 'invalid number')
    else:
        require(value is None or type(value) is bool, 'invalid value')

def privacy(value):
    bounded(value)
    reject_sensitive_content(value, Invalid)

def pairs(values):
    result = {}
    for key, value in values:
        require(key not in result, 'duplicate JSON key')
        result[key] = value
    return result

def invalid_constant(_):
    raise Invalid("non-finite JSON")

def load(path):
    with Path(path).open('rb') as source:
        body = source.read(MAX_BYTES + 1)
    require(len(body) <= MAX_BYTES, 'document byte limit exceeded')
    try:
        value = json.loads(body, object_pairs_hook=pairs, parse_constant=invalid_constant)
    except (UnicodeError, json.JSONDecodeError) as error:
        raise Invalid('invalid JSON') from error
    privacy(value)
    return value

def write_new(path, value):
    privacy(value)
    body = json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n'
    require(len(body.encode()) <= MAX_BYTES, 'document byte limit exceeded')
    with Path(path).open('x') as target:
        target.write(body)
