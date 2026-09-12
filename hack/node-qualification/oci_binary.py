"""Bind candidate producer bytes to an already trusted, reproducible OCI image index."""

import gzip
import hashlib
import sys
import tarfile
import tempfile
from pathlib import Path, PurePosixPath

from candidate_manifest import MAX_BINARY, snapshot
from archive_index import wrapper_digest
from common import DIGEST, require
from prepare_provider import REPOSITORY, local_command
from provider_plan import file_digest

sys.path.insert(0, str(REPOSITORY / "hack/release"))
from validate_oci_archive import MAX_ARCHIVE_BYTES, _descriptor_json, _json_member, validate_archive  # noqa: E402

BINARIES = {"kubectl-memlens", "memlens-agent", "memlens-collector", "memlens-node-context", "memlens-cert-bootstrap"}
LAYER_TYPES = {"application/vnd.oci.image.layer.v1.tar", "application/vnd.oci.image.layer.v1.tar+gzip"}
MAX_LAYER = 256 * 1024 * 1024


def blob_member(archive, members, descriptor):
    value = descriptor.get("digest")
    require(isinstance(value, str) and DIGEST.fullmatch(value), "image layer digest is invalid")
    member = members.get("blobs/sha256/" + value.removeprefix("sha256:"))
    require(member is not None and member.isfile() and type(descriptor.get("size")) is int
            and 0 < member.size == descriptor["size"] <= MAX_ARCHIVE_BYTES, "image layer size differs from its descriptor")
    digest = hashlib.sha256()
    with archive.extractfile(member) as stream:
        for block in iter(lambda: stream.read(65536), b""):
            digest.update(block)
    require("sha256:" + digest.hexdigest() == value, "image layer bytes differ from their digest")
    return member


def layer_binaries(archive, member, media_type, diff_id):
    require(media_type in LAYER_TYPES, "candidate image layer compression is unsupported")
    require(isinstance(diff_id, str) and DIGEST.fullmatch(diff_id), "candidate image diff ID is invalid")
    with archive.extractfile(member) as blob, tempfile.TemporaryFile() as expanded:
        decoded = gzip.GzipFile(fileobj=blob) if media_type.endswith("+gzip") else blob
        total, digest = 0, hashlib.sha256()
        with decoded:
            for block in iter(lambda: decoded.read(65536), b""):
                total += len(block)
                require(total <= MAX_LAYER, "image layer exceeds its expanded bound")
                digest.update(block)
                expanded.write(block)
        require("sha256:" + digest.hexdigest() == diff_id, "image layer does not match the runtime diff ID")
        expanded.seek(0)
        result, seen = {}, set()
        with tarfile.open(fileobj=expanded, mode="r:") as layer:
            for item in layer:
                path = PurePosixPath(item.name)
                require(not path.is_absolute() and ".." not in path.parts and bool(path.parts), "image layer path is invalid")
                name = path.as_posix()
                require(name not in seen and len(seen) < 128, "image layer inventory is duplicated or excessive")
                seen.add(name)
                require(item.isdir() or item.isfile(), "candidate image contains an unsupported filesystem entry")
                if item.isdir():
                    continue
                require(name in BINARIES and 0 < item.size <= MAX_BINARY and item.mode & 0o111,
                        "candidate image differs from the scratch executable layout")
                value, count = hashlib.sha256(), 0
                with layer.extractfile(item) as binary:
                    for block in iter(lambda: binary.read(65536), b""):
                        count += len(block)
                        require(count <= item.size, "image binary exceeds its header size")
                        value.update(block)
                require(count == item.size, "image binary is truncated")
                result[name] = "sha256:" + value.hexdigest()
        return result


def inspect_archive(path, image_digest, version, commit, build_date, architecture):
    require(architecture in {"amd64", "arm64"}, "unsupported provider image architecture")
    require(validate_archive(path, version, commit, build_date, require_attestations=False) == image_digest,
            "OCI archive differs from the signed candidate image index")
    with tarfile.open(path, "r:") as archive:
        members = {PurePosixPath(m.name).as_posix(): m for m in archive.getmembers()}
        root, root_bytes = _json_member(archive, members, "index.json")
        require(root["manifests"][0]["digest"] == image_digest, "OCI root index changed")
        index = _descriptor_json(archive, members, root["manifests"][0])
        selected = [m for m in index["manifests"] if m.get("platform", {}).get("os") == "linux"
                    and m.get("platform", {}).get("architecture") == architecture]
        require(len(selected) == 1, "candidate image platform is missing or ambiguous")
        descriptor = selected[0]
        require(descriptor["platform"].get("variant", "") in ({"", "v8"} if architecture == "arm64" else {"", "v1"}),
                "candidate image CPU variant is outside the qualified architecture")
        manifest = _descriptor_json(archive, members, descriptor)
        config = _descriptor_json(archive, members, manifest["config"])
        require(config.get("os") == "linux" and config.get("architecture") == architecture, "image configuration platform differs")
        layers, rootfs = manifest.get("layers"), config.get("rootfs", {})
        require(isinstance(layers, list) and 1 <= len(layers) <= 16 and rootfs.get("type") == "layers"
                and isinstance(rootfs.get("diff_ids"), list) and len(rootfs["diff_ids"]) == len(layers), "image root filesystem is invalid")
        binaries = {}
        for layer, diff_id in zip(layers, rootfs["diff_ids"]):
            member = blob_member(archive, members, layer)
            observed = layer_binaries(archive, member, layer["mediaType"], diff_id)
            require(not binaries.keys() & observed.keys(), "candidate image overwrites an executable in a later layer")
            binaries.update(observed)
        require(set(binaries) == BINARIES, "candidate image executable inventory is incomplete")
        return {"imageDigest": image_digest, "archiveIndexDigest": wrapper_digest(root_bytes, image_digest), "platformManifestDigest": descriptor["digest"],
                "imageConfigDigest": manifest["config"]["digest"], "architecture": architecture, "binaries": binaries}


def verify(path, candidate, configuration, architecture):
    require(candidate["sourceCommit"] == configuration["sourceCommit"] and candidate["imageDigest"] == configuration["imageDigest"],
            "candidate authority differs from the approved image or source")
    date = local_command(["git", "show", "-s", "--format=%cI", configuration["sourceCommit"]]).decode().strip()
    version = candidate["candidateTag"].split("-rc.", 1)[0]
    with tempfile.TemporaryDirectory(prefix="node-image-") as private:
        local = Path(private) / "image.tar"
        snapshot(Path(path), local, MAX_ARCHIVE_BYTES)
        result = inspect_archive(local, candidate["imageDigest"], version, configuration["sourceCommit"], date, architecture)
    require(result["binaries"]["memlens-node-context"] == configuration["producerDigest"]
            == file_digest(configuration["producerBinary"], MAX_BINARY), "runtime producer differs from the proposed binary")
    return result
