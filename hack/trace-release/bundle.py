"""Snapshot an untrusted local bundle into new private files before verification.

This performs bounded structural/content checks only. The consumer must still
verify the manifest's signature and every provenance/SBOM relationship. No source
file, bundled executable or image is ever executed by this module.
"""
import hashlib
import os
from pathlib import Path
import stat

from manifest import Manifest, ManifestError, require

MAX_SIGNATURE_BYTES = 4 << 20


def open_directory(path):
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    except OSError:
        raise ManifestError('bundle directory unavailable') from None
    return descriptor


def open_regular(directory, name, maximum):
    try:
        descriptor = os.open(name, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW,
                             dir_fd=directory)
    except OSError:
        raise ManifestError('bundle file unavailable') from None
    details = os.fstat(descriptor)
    if not stat.S_ISREG(details.st_mode) or not 0 < details.st_size <= maximum:
        os.close(descriptor)
        raise ManifestError('bundle file type or size outside bound')
    return os.fdopen(descriptor, 'rb')


def read_small(directory, name, maximum):
    with open_regular(directory, name, maximum) as source:
        data = source.read(maximum+1)
    require(0 < len(data) <= maximum, 'bundle file size outside bound')
    return data


def inventory(directory, expected):
    names = set()
    with os.scandir(directory) as entries:
        for entry in entries:
            require(len(names) < len(expected), 'extra bundle member')
            require(entry.name in expected and entry.is_file(follow_symlinks=False),
                    'unknown or non-regular bundle member')
            names.add(entry.name)
    require(names == set(expected), 'incomplete bundle inventory')


def write_new(destination, name, data):
    descriptor = os.open(destination/name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'wb') as output:
        output.write(data)


def copy_blob(directory, blob, destination):
    digest = hashlib.sha256()
    size = 0
    with open_regular(directory, blob.name, blob.size) as source:
        descriptor = os.open(destination/blob.name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'wb') as output:
            while True:
                block = source.read(min(1 << 20, blob.size-size+1))
                if not block:
                    break
                size += len(block)
                require(size <= blob.size, 'artefact size mismatch')
                digest.update(block)
                output.write(block)
    require(size == blob.size and digest.hexdigest() == blob.sha256,
            'artefact content mismatch')


def snapshot(source, destination):
    """Return an untrusted manifest only after copying the complete exact set.

    The destination must not exist. Failed partial snapshots remain unverified
    and cannot be reused as a successful result. Private output modes do not
    grant authenticity or runtime acceptance.
    """
    destination = Path(destination)
    directory = open_directory(source)
    try:
        raw = read_small(directory, 'trace-release.json', 65536)
        manifest = Manifest.read(raw)
        expected = manifest.asset_names()
        inventory(directory, expected)
        subjects = read_small(directory, 'release-subjects.txt', 8192)
        require(subjects == manifest.subjects(), 'subject list differs from manifest')
        signature = read_small(directory, 'trace-release.sigstore.json', MAX_SIGNATURE_BYTES)
        destination.mkdir(mode=0o700, parents=False, exist_ok=False)
        for blob in manifest.blobs():
            copy_blob(directory, blob, destination)
        write_new(destination, 'release-subjects.txt', subjects)
        write_new(destination, 'trace-release.sigstore.json', signature)
        # Write the manifest last; an interrupted snapshot has no complete inventory.
        write_new(destination, 'trace-release.json', raw)
        inventory(directory, expected)
        return manifest
    except OSError:
        raise ManifestError('bundle snapshot failed') from None
    finally:
        os.close(directory)
