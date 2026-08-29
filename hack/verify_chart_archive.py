#!/usr/bin/env python3

import argparse
import subprocess
import sys
import stat
import tarfile
from pathlib import Path, PurePosixPath

from release.package_chart import chart_fields


class ArchiveError(ValueError):
    pass


MAX_ARCHIVE_BYTES = 20 * 1024 * 1024
MAX_ARCHIVE_MEMBERS = 256
MAX_MEMBER_BYTES = 5 * 1024 * 1024
MAX_UNCOMPRESSED_BYTES = 20 * 1024 * 1024


def source_files(chart_directory):
    return {
        path.relative_to(chart_directory).as_posix(): path
        for path in chart_directory.rglob("*")
        if path.is_file()
    }


def verify_archive(archive_path, chart_directory):
    archive_path = Path(archive_path)
    chart_directory = Path(chart_directory).resolve()
    try:
        expected_root = chart_fields(chart_directory / "Chart.yaml")["name"]
    except (OSError, ValueError) as error:
        raise ArchiveError("checked-out chart metadata is invalid") from error
    expected = source_files(chart_directory)
    observed = {}
    try:
        archive_stat = archive_path.lstat()
        if not stat.S_ISREG(archive_stat.st_mode) or archive_stat.st_size <= 0 \
                or archive_stat.st_size > MAX_ARCHIVE_BYTES:
            raise ArchiveError("chart archive size or file type is outside the safety bound")
        archive = tarfile.open(archive_path, "r:gz")
    except (OSError, tarfile.TarError) as error:
        raise ArchiveError("chart archive is not a readable gzip tarball") from error
    with archive:
        member_count = 0
        uncompressed_bytes = 0
        for member in archive:
            member_count += 1
            if member_count > MAX_ARCHIVE_MEMBERS:
                raise ArchiveError("chart archive contains too many entries")
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts or not path.parts or path.parts[0] != expected_root:
                raise ArchiveError("chart archive contains an unsafe or unexpected path")
            if member.isdir():
                continue
            if not member.isfile():
                raise ArchiveError("chart archive contains a non-regular entry")
            if member.size <= 0 or member.size > MAX_MEMBER_BYTES:
                raise ArchiveError("chart archive member size is outside the safety bound")
            uncompressed_bytes += member.size
            if uncompressed_bytes > MAX_UNCOMPRESSED_BYTES:
                raise ArchiveError("chart archive uncompressed size exceeds the safety bound")
            relative = PurePosixPath(*path.parts[1:]).as_posix()
            if not relative or relative in observed:
                raise ArchiveError("chart archive contains a duplicate or invalid file")
            extracted = archive.extractfile(member)
            if extracted is None:
                raise ArchiveError("chart archive file could not be read")
            content = extracted.read(MAX_MEMBER_BYTES + 1)
            if len(content) != member.size:
                raise ArchiveError("chart archive member size differs from its header")
            observed[relative] = content
    if set(observed) != set(expected):
        raise ArchiveError("chart archive file set differs from the checked-out chart")
    for relative, source in expected.items():
        if relative == "Chart.yaml":
            continue
        if observed[relative] != source.read_bytes():
            raise ArchiveError(f"chart archive content differs from source: {relative}")


def verify_chart_metadata(archive_path, chart_directory):
    outputs = []
    for target in (archive_path, chart_directory):
        try:
            result = subprocess.run(
                ["helm", "show", "chart", str(target)], check=False, capture_output=True, timeout=30,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise ArchiveError("Helm could not inspect chart metadata") from error
        if result.returncode != 0:
            raise ArchiveError("Helm rejected chart metadata")
        outputs.append(result.stdout)
    if outputs[0] != outputs[1]:
        raise ArchiveError("chart archive metadata differs from checked-out source")


def main():
    parser = argparse.ArgumentParser(description="Bind a packaged Helm chart to its checked-out source.")
    parser.add_argument("archive")
    parser.add_argument("chart_directory")
    arguments = parser.parse_args()
    try:
        verify_archive(arguments.archive, arguments.chart_directory)
        verify_chart_metadata(arguments.archive, arguments.chart_directory)
    except ArchiveError as error:
        print(f"chart archive verification error: {error}", file=sys.stderr)
        return 2
    print("chart archive matches checked-out source")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
