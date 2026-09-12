"""Verify the import wrapper's single link to the approved image index or manifest."""

import hashlib
import json
import tarfile

from common import DIGEST, require

INDEX = "application/vnd.oci.image.index.v1+json"
MANIFEST = "application/vnd.oci.image.manifest.v1+json"


def wrapper_digest(raw, image_digest):
    require(0 < len(raw) <= 2 * 1024 * 1024 and DIGEST.fullmatch(image_digest), "image wrapper input is invalid")
    document = json.loads(raw)
    descriptors = document.get("manifests")
    require(document.get("schemaVersion") == 2 and document.get("mediaType") == INDEX
            and isinstance(descriptors, list) and len(descriptors) == 1, "image wrapper must contain exactly one descriptor")
    descriptor = descriptors[0]
    require(descriptor.get("digest") == image_digest and descriptor.get("mediaType") in {INDEX, MANIFEST},
            "image wrapper does not reference the approved image")
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def from_archive(path, image_digest):
    with tarfile.open(path, "r:") as archive:
        members = []
        for count, member in enumerate(archive):
            require(count < 1024, "image archive inventory exceeds its bound")
            if member.name == "index.json":
                members.append(member)
        require(len(members) == 1 and members[0].isfile() and 0 < members[0].size <= 2 * 1024 * 1024,
                "image archive wrapper is missing, duplicated or invalid")
        with archive.extractfile(members[0]) as stream:
            return wrapper_digest(stream.read(2 * 1024 * 1024 + 1), image_digest)
