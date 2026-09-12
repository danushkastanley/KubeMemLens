"""Validate an approved proposal and freeze its executable inputs before target access."""

import os
import re
from dataclasses import replace
from pathlib import Path

from candidate_manifest import MAX_BINARY, snapshot
from common import load, require, write_new
from prepare_provider import REPOSITORY, local_command
from process import execute
from provider_artifacts import verify
from provider_bundle import validate_bundle
from provider_plan import file_digest
from verify_provider_artifacts import host_platform

ACKNOWLEDGEMENT = "run-reviewed-node-context-plan"


def prepare(args):
    require(args.acknowledge == ACKNOWLEDGEMENT and args.replacement_acknowledge == "provider-action-approved",
            "explicit run and operator-replacement approval are required")
    profile = load(args.profile)
    bundle = validate_bundle(args.proposal, profile, args.plan_digest)
    require(type(args.replacement_slot) is int and 0 <= args.replacement_slot < profile["workload"]["linuxNodes"],
            "replacement slot is outside the approved pool")
    root = Path(args.output_dir)
    require(root.is_absolute() and not root.exists() and root.parent.is_dir(), "output must be a new absolute directory")
    if root.resolve().is_relative_to(REPOSITORY):
        require(local_command(["git", "check-ignore", "--", str(root)]).strip(), "provider output inside the repository must be ignored")
    inventory = load(REPOSITORY / "hack/provider-profiles" / (bundle.configuration["inventoryProfile"] + ".json"))
    require(re.fullmatch(inventory["expectations"]["architecturePattern"], args.architecture), "artefact architecture differs from the profile")
    host = host_platform()
    proof = verify(bundle, args.candidate_bundle, args.candidate_tag, args.image_archive, host,
                   {"runtime": {"architecture": args.architecture}})
    root.mkdir(mode=0o700)
    private, public = root / "private", root / "evidence"
    private.mkdir(mode=0o700); public.mkdir(mode=0o700)
    configuration = dict(bundle.configuration)
    for source, name, maximum, mode, expected in (
        ("chartArchive", "chart.tgz", 20 * 1024 * 1024, 0o400, "chartDigest"),
        ("cliBinary", "kubectl-memlens", MAX_BINARY, 0o500, "cliDigest")):
        target = private / name
        require(snapshot(Path(configuration[source]), target, maximum) == configuration[expected],
                "candidate input changed before execution")
        target.chmod(mode)
        configuration[source] = str(target)
    # This copy changes paths only. Its bytes still match the original signed
    # proposal, which is revalidated separately before and after the live run.
    frozen = replace(bundle, configuration=configuration)
    write_new(public / "artefacts.json", {"schemaVersion": 1, "scope": "provider-run-artefacts", "qualified": False,
              "planDigest": args.plan_digest, "toolCommit": bundle.plan["qualificationToolCommit"], "artefacts": proof})
    return frozen, proof, private, public


def build_helpers(private, run=execute):
    system, architecture = host_platform().split("_", 1)
    environment = dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=architecture,
                       GOTOOLCHAIN="local", GOFLAGS="", GOWORK="off", KUBECONFIG=os.devnull,
                       GIT_TERMINAL_PROMPT="0", GIT_NO_LAZY_FETCH="1")
    result = {}
    for name in ("api-bridge", "chart-inventory"):
        path = private / name
        require(not path.exists(), "qualification helper output already exists")
        run(["go", "-C", str(REPOSITORY), "build", "-trimpath", "-o", str(path),
             "./hack/node-qualification/" + name], timeout=180, maximum=16 * 1024, environment=environment)
        path.chmod(0o500)
        result[name] = file_digest(path, MAX_BINARY)
    write_new(private / "helper-digests.json", result)
    return result
