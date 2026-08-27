#!/usr/bin/env python3

import argparse
import hashlib
import json
import re
import stat
import tarfile
from pathlib import Path, PurePosixPath


DIGEST = re.compile(r"^sha256:([a-f0-9]{64})$")
INDEX_MEDIA_TYPE = "application/vnd.oci.image.index.v1+json"
MANIFEST_MEDIA_TYPE = "application/vnd.oci.image.manifest.v1+json"
ATTESTATION_TYPE = "attestation-manifest"
PREDICATES = {"https://spdx.dev/Document", "https://slsa.dev/provenance/v1"}
MAX_ARCHIVE_BYTES = 512 * 1024 * 1024
MAX_MEMBERS = 1024
MAX_JSON_BYTES = 2 * 1024 * 1024


class ArchiveError(ValueError):
    pass


def _json_member(archive, members, name):
    member = members.get(name)
    if member is None or member.size <= 0 or member.size > MAX_JSON_BYTES:
        raise ArchiveError(f"OCI JSON member is missing or outside its size bound: {name}")
    stream = archive.extractfile(member)
    if stream is None:
        raise ArchiveError(f"OCI JSON member cannot be read: {name}")
    content = stream.read(MAX_JSON_BYTES + 1)
    if len(content) != member.size:
        raise ArchiveError(f"OCI JSON member size differs from its header: {name}")
    try:
        return json.loads(content), content
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ArchiveError(f"OCI JSON member is invalid: {name}") from error


def _descriptor_json(archive, members, descriptor):
    match = DIGEST.fullmatch(descriptor.get("digest", ""))
    if match is None or type(descriptor.get("size")) is not int:
        raise ArchiveError("OCI descriptor digest or size is invalid")
    name = f"blobs/sha256/{match.group(1)}"
    document, content = _json_member(archive, members, name)
    if len(content) != descriptor["size"] or hashlib.sha256(content).hexdigest() != match.group(1):
        raise ArchiveError(f"OCI descriptor content does not match: {name}")
    return document


def validate_archive(path, version, commit, build_date, require_attestations=True):
    path = Path(path)
    details = path.lstat()
    if not stat.S_ISREG(details.st_mode) or details.st_size <= 0 or details.st_size > MAX_ARCHIVE_BYTES:
        raise ArchiveError("OCI archive file type or size is outside the safety bound")
    try:
        archive = tarfile.open(path, "r")
    except (OSError, tarfile.TarError) as error:
        raise ArchiveError("OCI archive is not a readable tar file") from error

    with archive:
        members = {}
        for member in archive:
            if len(members) >= MAX_MEMBERS:
                raise ArchiveError("OCI archive contains too many entries")
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not name.parts:
                raise ArchiveError("OCI archive contains an unsafe path")
            normalised = name.as_posix()
            if normalised in members or not (member.isdir() or member.isfile()):
                raise ArchiveError("OCI archive contains a duplicate or non-regular entry")
            members[normalised] = member

        layout, _ = _json_member(archive, members, "oci-layout")
        if layout != {"imageLayoutVersion": "1.0.0"}:
            raise ArchiveError("OCI layout version is unsupported")
        root, _ = _json_member(archive, members, "index.json")
        root_manifests = root.get("manifests")
        if root.get("schemaVersion") != 2 or root.get("mediaType") != INDEX_MEDIA_TYPE \
                or not isinstance(root_manifests, list) or len(root_manifests) != 1:
            raise ArchiveError("OCI root index must contain one image index")
        descriptor = root_manifests[0]
        if descriptor.get("mediaType") != INDEX_MEDIA_TYPE:
            raise ArchiveError("OCI root descriptor is not an image index")
        image_index = _descriptor_json(archive, members, descriptor)

        image_manifests = {}
        attestations = {}
        manifests = image_index.get("manifests")
        if image_index.get("schemaVersion") != 2 or image_index.get("mediaType") != INDEX_MEDIA_TYPE \
                or not isinstance(manifests, list):
            raise ArchiveError("OCI image index is invalid")
        for item in manifests:
            platform = item.get("platform", {})
            key = (platform.get("os"), platform.get("architecture"))
            annotations = item.get("annotations", {})
            if annotations.get("vnd.docker.reference.type") == ATTESTATION_TYPE:
                subject = annotations.get("vnd.docker.reference.digest")
                if subject in attestations:
                    raise ArchiveError("OCI image has duplicate attestations")
                attestations[subject] = _descriptor_json(archive, members, item)
                continue
            if key in image_manifests:
                raise ArchiveError("OCI image has duplicate platform manifests")
            image_manifests[key] = (item, _descriptor_json(archive, members, item))

        expected_platforms = {("linux", "amd64"), ("linux", "arm64")}
        if set(image_manifests) != expected_platforms:
            raise ArchiveError("OCI image must contain exactly amd64 and arm64 platform manifests")
        if require_attestations and len(attestations) != 2:
            raise ArchiveError("OCI image must contain one attestation per platform")
        if not require_attestations and attestations:
            raise ArchiveError("reproducible OCI image must not contain inline attestations")

        expected_labels = {
            "org.opencontainers.image.version": version,
            "org.opencontainers.image.revision": commit,
            "org.opencontainers.image.created": build_date,
            "org.opencontainers.image.source": "https://github.com/danushkastanley/KubeMemLens",
            "org.opencontainers.image.licenses": "Apache-2.0",
        }
        for item, manifest in image_manifests.values():
            if item.get("mediaType") != MANIFEST_MEDIA_TYPE or manifest.get("mediaType") != MANIFEST_MEDIA_TYPE:
                raise ArchiveError("OCI platform descriptor is not an image manifest")
            config = _descriptor_json(archive, members, manifest.get("config", {}))
            runtime = config.get("config", {})
            if runtime.get("User") != "65532:65532" or runtime.get("Entrypoint") != ["/kubectl-memlens"]:
                raise ArchiveError("OCI image runtime identity is invalid")
            labels = runtime.get("Labels", {})
            if any(labels.get(name) != value for name, value in expected_labels.items()):
                raise ArchiveError("OCI image labels do not match the release identity")
            if not require_attestations:
                continue
            digest = item["digest"]
            attestation = attestations.get(digest)
            if attestation is None:
                raise ArchiveError("OCI platform is missing its attestation manifest")
            predicates = {
                layer.get("annotations", {}).get("in-toto.io/predicate-type")
                for layer in attestation.get("layers", [])
                if layer.get("mediaType") == "application/vnd.in-toto+json"
            }
            if predicates != PREDICATES:
                raise ArchiveError("OCI platform must include SPDX SBOM and SLSA provenance attestations")
            for layer in attestation["layers"]:
                statement = _descriptor_json(archive, members, layer)
                predicate = layer["annotations"]["in-toto.io/predicate-type"]
                if statement.get("_type") != "https://in-toto.io/Statement/v0.1" \
                        or statement.get("predicateType") != predicate:
                    raise ArchiveError("OCI attestation statement does not match its predicate descriptor")
                if predicate == "https://spdx.dev/Document" \
                        and statement.get("predicate", {}).get("spdxVersion") != "SPDX-2.3":
                    raise ArchiveError("OCI image SBOM is not SPDX 2.3")

        return descriptor["digest"]


def main():
    parser = argparse.ArgumentParser(description="Validate the build-once KubeMemLens OCI image archive.")
    parser.add_argument("archive")
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--build-date", required=True)
    parser.add_argument("--attestations", choices=("required", "none"), default="required")
    arguments = parser.parse_args()
    try:
        digest = validate_archive(
            arguments.archive,
            arguments.version,
            arguments.commit,
            arguments.build_date,
            require_attestations=arguments.attestations == "required",
        )
    except (OSError, ArchiveError) as error:
        print(f"OCI archive verification error: {error}", file=__import__("sys").stderr)
        return 2
    print(digest)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
