"""Deterministic public payload archives; never follow links or replace outputs."""
import os
from pathlib import Path
import stat
import tarfile

from manifest import ManifestError, require


def pack_tree(source, destination, epoch, *, maximum=512 << 20, members=10000):
    source, destination = Path(source), Path(destination)
    require(type(epoch) is int and 0 <= epoch <= 2**32-1, 'invalid archive timestamp')
    require(source.is_dir() and not source.is_symlink(), 'public source directory required')
    paths = []
    total = 10240
    for path in sorted(source.rglob('*')):
        require(not path.is_symlink(), 'archive links forbidden')
        if path.is_dir():
            continue
        info = path.stat()
        require(stat.S_ISREG(info.st_mode) and 0 <= info.st_size <= 128 << 20,
                'archive member type or size outside bound')
        name = path.relative_to(source).as_posix()
        require(len(name) <= 1024 and all(part not in ('', '.', '..') for part in name.split('/')) and
                all(32 < ord(character) < 127 for character in name) and '\\' not in name,
                'invalid archive member name')
        require(info.st_mode & 0o7022 == 0, 'unsafe archive member permissions')
        total += 512 + ((info.st_size + 511)//512)*512
        require(len(paths) < members and total <= maximum, 'archive inventory outside bound')
        paths.append((path, name, info))
    require(paths, 'empty public archive')
    with destination.open('xb') as output:
        with tarfile.open(fileobj=output, mode='w', format=tarfile.USTAR_FORMAT) as archive:
            for path, name, info in paths:
                descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
                with os.fdopen(descriptor, 'rb') as stream:
                    current = os.fstat(stream.fileno())
                    require(os.path.samestat(info, current) and current.st_size == info.st_size and
                            current.st_mode == info.st_mode, 'archive source changed')
                    member = tarfile.TarInfo(name)
                    member.size, member.mode, member.mtime = info.st_size, stat.S_IMODE(info.st_mode), epoch
                    member.uid = member.gid = 0
                    member.uname = member.gname = ''
                    archive.addfile(member, stream)
    require(destination.stat().st_size <= maximum, 'archive exceeds byte bound')


def copy_public_tree(source, destination):
    """Copy only regular files/directories from caller-owned build inputs.

    This is not a secret detector or a trust decision. The assembler chooses
    explicit public roots, and the payload inspector validates the final layout.
    """
    source, destination = Path(source), Path(destination)
    require(source.is_dir() and not source.is_symlink(), 'public input directory required')
    destination.mkdir(mode=0o755)
    import shutil
    for item in sorted(source.iterdir()):
        require(not item.is_symlink(), 'public input links forbidden')
        target = destination/item.name
        if item.is_dir():
            copy_public_tree(item, target)
        else:
            require(item.is_file(), 'public input is not regular')
            shutil.copyfile(item, target)
            target.chmod(0o444)
