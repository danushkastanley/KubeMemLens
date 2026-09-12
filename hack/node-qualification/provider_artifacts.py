"""Bind approved provider artefacts to the existing signed candidate authority."""

from candidate_manifest import verify as verify_candidate
from common import require
from oci_binary import verify as verify_image
from source_digest import production_source


def verify(bundle, candidate_directory, candidate_tag, image_archive, host_platform, binding):
    configuration = bundle.configuration
    candidate = verify_candidate(candidate_directory, candidate_tag, configuration, host_platform)
    image = verify_image(image_archive, candidate, configuration, binding["runtime"]["architecture"])
    require(image["imageDigest"] == configuration["imageDigest"], "verified image differs from the proposal")
    return {"candidate": candidate, "image": image, "sourceTreeDigest": production_source(configuration["sourceCommit"])}
