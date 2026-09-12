"""Verify candidate authority and local consumer bytes without executing candidate code."""

import hashlib
import json
import re
import stat
import tarfile
import tempfile
from pathlib import Path, PurePosixPath

from common import require
from prepare_provider import REPOSITORY
from process import execute
from provider_plan import file_digest

REPOSITORY_NAME = "danushkastanley/KubeMemLens"
ISSUER = "https://token.actions.githubusercontent.com"
MAX_BINARY = 128 * 1024 * 1024


def snapshot(source, destination, maximum):
    info = source.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= maximum, "candidate input type or size is invalid")
    total, digest = 0, hashlib.sha256()
    with source.open("rb") as source_stream, destination.open("xb") as destination_stream:
        for block in iter(lambda: source_stream.read(65536), b""):
            total += len(block)
            require(total <= maximum, "candidate input exceeded its bound")
            digest.update(block)
            destination_stream.write(block)
    require(total > 0, "candidate input is empty")
    destination.chmod(0o600)
    return "sha256:" + digest.hexdigest()


def cli_archive_digest(path):
    """Hash the single regular CLI member; never extract archive paths to disk."""
    found, total, members = None, 0, set()
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            require(not name.is_absolute() and ".." not in name.parts and bool(name.parts), "candidate archive path is invalid")
            require(name.as_posix() not in members and len(members) < 128, "candidate archive inventory is invalid")
            members.add(name.as_posix())
            require(member.isfile() or member.isdir(), "candidate archive contains a non-regular member")
            require(0 <= member.size <= MAX_BINARY, "candidate archive member exceeds its bound")
            total += member.size
            require(total <= 2 * MAX_BINARY, "candidate archive exceeds its expanded bound")
            if name.as_posix() != "kubectl-memlens":
                continue
            require(member.isfile() and member.size > 0, "candidate CLI is not a regular binary")
            digest, count = hashlib.sha256(), 0
            with archive.extractfile(member) as stream:
                for block in iter(lambda: stream.read(65536), b""):
                    count += len(block)
                    require(count <= member.size, "candidate CLI exceeded its declared size")
                    digest.update(block)
            require(count == member.size, "candidate CLI was truncated")
            found = "sha256:" + digest.hexdigest()
    require(found is not None, "candidate archive has no CLI binary")
    return found


def verify(directory, candidate_tag, configuration, host_platform, run=execute):
    require(isinstance(candidate_tag, str) and re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.[1-9][0-9]*", candidate_tag),
            "an exact candidate tag is required")
    require(host_platform in {"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"}, "unsupported qualification host platform")
    root = Path(directory)
    require(root.is_absolute() and root.is_dir() and not root.is_symlink(), "candidate bundle must be an absolute directory")
    ga_tag = candidate_tag.split("-rc.", 1)[0]
    identity = f"https://github.com/{REPOSITORY_NAME}/.github/workflows/candidate.yml@refs/tags/{candidate_tag}"
    with tempfile.TemporaryDirectory(prefix="node-candidate-") as private:
        private = Path(private)
        manifest_path, signature = private / "candidate-manifest.json", private / "candidate-manifest.sigstore.json"
        manifest_digest = snapshot(root / manifest_path.name, manifest_path, 64 * 1024)
        snapshot(root / signature.name, signature, 4 * 1024 * 1024)
        manifest = json.loads(run([str(REPOSITORY / "hack/release/validate_candidate_manifest.sh"), str(manifest_path),
                                  candidate_tag, ga_tag, configuration["sourceCommit"]]))
        require(manifest["image"]["digest"] == configuration["imageDigest"],
                "candidate image digest differs from the approved proposal")
        chart_digest = "sha256:" + manifest["chart"]["package"]["sha256"]
        require(chart_digest == configuration["chartDigest"] == file_digest(configuration["chartArchive"], 20 * 1024 * 1024),
                "candidate chart differs from the proposed bytes")
        run(["cosign", "verify-blob", "--bundle", str(signature), "--certificate-identity", identity,
             "--certificate-oidc-issuer", ISSUER, str(manifest_path)], timeout=60, maximum=2 * 1024 * 1024)
        archive_name = f"kube-memlens_{ga_tag[1:]}_{host_platform}.tar.gz"
        archive = private / archive_name
        snapshot(root / archive_name, archive, MAX_BINARY)
        require(file_digest(archive, MAX_BINARY) == "sha256:" + manifest["cli_archives"][archive_name], "signed CLI archive digest differs")
        cli_digest = cli_archive_digest(archive)
        require(cli_digest == configuration["cliDigest"] == file_digest(configuration["cliBinary"], MAX_BINARY),
                "candidate CLI differs from the proposed binary")
        # An approved mirror may serve the same immutable image. Verify source
        # authority at the signed candidate repository and match its exact digest.
        image = manifest["image"]["repository"] + "@" + configuration["imageDigest"]
        run(["cosign", "verify", "--certificate-identity", identity, "--certificate-oidc-issuer", ISSUER, image],
            timeout=60, maximum=2 * 1024 * 1024)
        run(["gh", "attestation", "verify", "oci://" + image, "--repo", REPOSITORY_NAME], timeout=60, maximum=2 * 1024 * 1024)
    return {"candidateTag": candidate_tag, "sourceCommit": configuration["sourceCommit"],
            "manifestDigest": manifest_digest, "imageDigest": configuration["imageDigest"],
            "chartDigest": chart_digest, "cliDigest": cli_digest, "hostPlatform": host_platform}
