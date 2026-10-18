"""Check real local signatures against synthetic archives; never contact a cluster."""

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import secrets
from types import SimpleNamespace
from unittest.mock import patch

from common import ContractError, require
from development_candidate import MANIFEST, SIGNATURE, verify
from prepare_provider import local_command
from process import execute
from source_digest import production_source
from test_development_candidate import DevelopmentCandidateTest
from test_oci_binary import BinaryImage, sha


def rejected(operation):
    try:
        operation()
    except ContractError:
        return
    raise ContractError("tampered development signature was accepted")


def check(tool):
    require(tool.is_file() and tool.is_absolute(), "absolute cosign binary required")
    case = DevelopmentCandidateTest()
    case.setUp()
    try:
        commit = local_command(["git", "rev-parse", "HEAD"]).decode().strip()
        epoch = int(local_command(["git", "show", "-s", "--format=%ct", commit]).decode().strip())
        date = datetime.fromtimestamp(epoch, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        # Only the synthetic fixture's image labels vary. Verification below
        # uses real Git source, cosign, snapshots and strict archive inspection.
        with patch.multiple("test_oci_binary", VERSION="dev", COMMIT=commit, BUILD_DATE=date):
            image = BinaryImage().write(case.image)
        case.config.update(sourceCommit=commit, imageDigest=image)
        case.value.update(sourceCommit=commit, sourceTreeDigest=production_source(commit),
                          buildDate=date, imageDigest=image, imageArchive=case.entry(case.image))
        case.save()
        keys = case.root / "keys"
        keys.mkdir(mode=0o700)
        environment = {"PATH": "/usr/bin:/bin", "HOME": str(keys), "TMPDIR": str(keys),
                       "XDG_CACHE_HOME": str(keys / "cache"), "COSIGN_PASSWORD": secrets.token_urlsafe(32),
                       "GOMAXPROCS": "2"}

        def sign_command(*args):
            return execute([str(tool), *map(str, args)], timeout=35, maximum=65536, environment=environment)

        sign_command("generate-key-pair", "--output-key-prefix", keys / "local")
        sign_command("signing-config", "create", "--out", keys / "signing.json")
        signing = json.loads((keys / "signing.json").read_bytes())
        require(signing == {"mediaType": "application/vnd.dev.sigstore.signingconfig.v0.2+json",
                            "rekorTlogConfig": {}, "tsaConfig": {}},
                "local test signing must not use remote services")
        signature = case.bundle / SIGNATURE
        signature.unlink()  # This temporary fixture file belongs to this test.
        sign_command("sign-blob", "--yes", "--key", keys / "local.key", "--bundle", signature,
                     "--signing-config", keys / "signing.json", case.bundle / MANIFEST)
        case.args.cosign = str(tool)
        case.args.cosign_digest = sha(tool.read_bytes())
        case.args.development_key = str(keys / "local.pub")
        case.args.development_key_digest = sha((keys / "local.pub").read_bytes())
        bundle = SimpleNamespace(configuration=case.config)

        def verified():
            return verify(bundle, case.args, case.host)

        proof = verified()
        require(proof["authority"] == "local-development" and not proof["releaseQualificationGranted"],
                "test unexpectedly granted release authority")
        # Re-pinning a modified manifest must still fail its real signature.
        original = (case.bundle / MANIFEST).read_bytes()
        case.value["sourceTreeDigest"] = "sha256:" + "f" * 64
        case.save()
        rejected(verified)
        (case.bundle / MANIFEST).write_bytes(original)
        case.args.development_manifest_digest = sha(original)
        # An independently pinned but different valid public key is insufficient.
        sign_command("generate-key-pair", "--output-key-prefix", keys / "other")
        case.args.development_key = str(keys / "other.pub")
        case.args.development_key_digest = sha((keys / "other.pub").read_bytes())
        rejected(verified)
        case.args.development_key = str(keys / "local.pub")
        case.args.development_key_digest = sha((keys / "local.pub").read_bytes())
        case.image.write_bytes(case.image.read_bytes() + b"tampered")
        rejected(verified)
        require(not (case.root / "must-not-execute").exists(), "candidate was executed by verification")
        print("Real development signature passed; changed manifest, wrong key and changed payload rejected. No target access.")
    finally:
        case.doCleanups()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cosign", required=True, type=Path)
    check(parser.parse_args().cosign.resolve(strict=True))
