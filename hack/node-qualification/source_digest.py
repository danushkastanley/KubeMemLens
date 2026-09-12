"""Hash the production Go source at the approved commit using bounded local Git reads."""

import hashlib
import os

from common import COMMIT, require
from prepare_provider import REPOSITORY, local_command
from process import execute

MAX_SOURCE_BYTES = 64 * 1024 * 1024


def production_source(commit, command=local_command, run=execute):
    require(isinstance(commit, str) and COMMIT.fullmatch(commit), "an exact source commit is required")
    listing = command(["git", "ls-tree", "-rz", "--long", commit, "--", "go.mod", "go.sum", "cmd", "internal"], REPOSITORY)
    files, total = [], 0
    for row in listing.split(b"\0"):
        if not row:
            continue
        metadata, path = row.split(b"\t", 1)
        if path not in {b"go.mod", b"go.sum"} and (not path.endswith(b".go") or path.endswith(b"_test.go")):
            continue
        mode, kind, oid, size = metadata.split()
        require(mode in {b"100644", b"100755"} and kind == b"blob", "production source must contain regular files")
        size = int(size)
        total += size
        require(0 < size <= 2 * 1024 * 1024 and total <= MAX_SOURCE_BYTES and len(files) < 1024,
                "production source exceeds its bound")
        files.append((path, oid, size))
    require({b"go.mod", b"go.sum"} <= {p for p, _, _ in files}, "source commit is missing module metadata")
    files.sort()
    output = run(["git", "-C", str(REPOSITORY), "cat-file", "--batch"], data=b"\n".join(oid for _, oid, _ in files) + b"\n",
                 maximum=MAX_SOURCE_BYTES + 128 * len(files),
                 environment=dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_TERMINAL_PROMPT="0")).encode("utf-8")
    position, digest = 0, hashlib.sha256()
    for path, oid, size in files:
        end = output.find(b"\n", position)
        require(end >= position and output[position:end] == oid + b" blob " + str(size).encode(), "source blob identity or size differs")
        start = end + 1
        require(start + size < len(output) and output[start + size:start + size + 1] == b"\n", "source blob was truncated")
        digest.update(path + b"\0" + output[start:start + size] + b"\0")
        position = start + size + 1
    require(position == len(output), "source read contained unexpected trailing data")
    return "sha256:" + digest.hexdigest()
