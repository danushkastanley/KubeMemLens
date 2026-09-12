"""Bind proposal source files to Git blobs, including the complete chart file set."""

from common import require


def bind_files(repository, commit, relative, command):
    listing = command(["git", "ls-tree", "-rz", "--long", commit, "--", relative], repository)
    entries = {}
    total = 0
    for row in listing.split(b"\0"):
        if not row:
            continue
        metadata, path = row.split(b"\t", 1)
        mode, kind, _, size = metadata.split()
        require(mode in {b"100644", b"100755"} and kind == b"blob", "proposal source must contain regular files only")
        size = int(size)
        total += size
        require(0 < size <= 2 * 1024 * 1024 and total <= 20 * 1024 * 1024,
                "proposal source exceeds its byte bound")
        entries[path.decode("utf-8")] = size
        require(len(entries) <= 256, "proposal source has too many files")
    require(entries, "source commit has no required proposal files")
    target = repository / relative
    observed = [target] if target.is_file() else [p for p in target.rglob("*") if p.is_file() or p.is_symlink()]
    require(all(not p.is_symlink() for p in observed), "proposal source may not contain symlinks")
    require({p.relative_to(repository).as_posix() for p in observed} == set(entries),
            "proposal source file set differs from the source commit")
    for path, size in entries.items():
        with (repository / path).open("rb") as source:
            current = source.read(size + 1)
        expected = command(["git", "show", commit + ":" + path], repository)
        require(len(current) == size and current == expected, "proposal source bytes differ from the source commit")
