#!/usr/bin/env python3
"""Create a byte-reproducible Helm chart archive from a prepared chart tree."""

from __future__ import annotations

import argparse
import gzip
import io
from pathlib import Path
import re
import tarfile


FIELD_PATTERN = re.compile(r"^(name|version|appVersion):\s*[\"']?([^\"'\s]+)", re.MULTILINE)


def chart_fields(chart_yaml: Path) -> dict[str, str]:
    values = dict(FIELD_PATTERN.findall(chart_yaml.read_text(encoding="utf-8")))
    missing = {"name", "version", "appVersion"} - values.keys()
    if missing:
        raise ValueError(f"Chart.yaml is missing required fields: {', '.join(sorted(missing))}")
    return values


def archive_members(source: Path) -> list[Path]:
    members = sorted(source.rglob("*"), key=lambda path: path.relative_to(source).as_posix())
    for member in members:
        if member.is_symlink() or not (member.is_dir() or member.is_file()):
            raise ValueError(f"unsupported chart member: {member}")
    return members


def tar_info(name: str, *, epoch: int, directory: bool, size: int = 0) -> tarfile.TarInfo:
    info = tarfile.TarInfo(name=name)
    info.mtime = epoch
    info.uid = 0
    info.gid = 0
    info.uname = "root"
    info.gname = "root"
    info.mode = 0o755 if directory else 0o644
    info.type = tarfile.DIRTYPE if directory else tarfile.REGTYPE
    info.size = size
    return info


def package_chart(source: Path, destination: Path, epoch: int) -> None:
    fields = chart_fields(source / "Chart.yaml")
    root = fields["name"]
    destination.parent.mkdir(parents=True, exist_ok=True)

    with destination.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, compresslevel=9, mtime=epoch) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                archive.addfile(tar_info(root, epoch=epoch, directory=True))
                for member in archive_members(source):
                    relative = member.relative_to(source).as_posix()
                    name = f"{root}/{relative}"
                    if member.is_dir():
                        archive.addfile(tar_info(name, epoch=epoch, directory=True))
                        continue
                    data = member.read_bytes()
                    archive.addfile(
                        tar_info(name, epoch=epoch, directory=False, size=len(data)),
                        io.BytesIO(data),
                    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--app-version", required=True)
    parser.add_argument("--epoch", required=True, type=int)
    args = parser.parse_args()

    if args.epoch < 0:
        parser.error("--epoch must be a non-negative Unix timestamp")
    fields = chart_fields(args.source / "Chart.yaml")
    if fields["version"] != args.version:
        parser.error(f"Chart.yaml version is {fields['version']}, expected {args.version}")
    if fields["appVersion"] != args.app_version:
        parser.error(f"Chart.yaml appVersion is {fields['appVersion']}, expected {args.app_version}")
    package_chart(args.source, args.destination, args.epoch)


if __name__ == "__main__":
    main()
