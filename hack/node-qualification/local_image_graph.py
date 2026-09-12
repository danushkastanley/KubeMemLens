"""Read the image descriptor graph from the already owned kind image store."""

import hashlib
import json

from common import DIGEST, require
from process import execute

INDEX = "application/vnd.oci.image.index.v1+json"
MANIFEST = "application/vnd.oci.image.manifest.v1+json"


def read(node, digest, run):
    require(isinstance(digest, str) and DIGEST.fullmatch(digest), "local image descriptor digest is invalid")
    raw = run(["docker", "exec", node, "ctr", "-n", "k8s.io", "content", "get", digest], maximum=2 * 1024 * 1024)
    require("sha256:" + hashlib.sha256(raw.encode()).hexdigest() == digest, "local image descriptor bytes differ")
    return json.loads(raw)


def inspect(node, image_digest, architecture, run=execute):
    document = read(node, image_digest, run)
    manifest_digest = image_digest
    if document.get("mediaType") == INDEX:
        platforms = [m for m in document["manifests"] if m.get("platform", {}).get("os") == "linux"
                     and m.get("platform", {}).get("architecture") == architecture]
        require(len(platforms) == 1, "local image platform is missing or ambiguous")
        manifest_digest = platforms[0]["digest"]
        document = read(node, manifest_digest, run)
    require(document.get("mediaType") == MANIFEST, "local image manifest type is unsupported")
    config_digest = document["config"]["digest"]
    config = read(node, config_digest, run)
    require(config.get("os") == "linux" and config.get("architecture") == architecture, "local image architecture differs")
    return {"imageDigest": image_digest, "platformManifestDigest": manifest_digest,
            "imageConfigDigest": config_digest, "architecture": architecture}
