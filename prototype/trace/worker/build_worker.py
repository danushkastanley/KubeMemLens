"""Reproduce both Linux worker binaries in the pinned capability-free builder.

Builds only: no signing, installation, programme loading or publication.
The prepared SDK must already match its source receipt. Outputs require a new
directory. Module cache and source are read-only; the container has no network.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[3]
WORKER = ROOT / "prototype/trace/worker"
BUILDER = "golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_sdk():
    sdk = WORKER / ".sdk"
    receipt = json.loads((sdk / ".kml-source-receipt.json").read_text())
    files = {str(p.relative_to(sdk)): sha(p) for p in sorted(sdk.rglob("*"))
             if p.is_file() and p.name != ".kml-source-receipt.json"}
    if (receipt["moduleSum"] != "h1:Ps4HXfpq4qUMrWlCSqdxl3ldP1LMF+tn9VHip637pAg="
            or receipt["patchSHA256"] != sha(WORKER / "sdk-policy.patch")
            or receipt["files"] != files):
        raise ValueError("prepared SDK does not match its receipt")
    return sha(sdk / ".kml-source-receipt.json")


def inputs():
    files = [ROOT / name for name in ("go.mod", "go.sum", "LICENSE", "NOTICE")]
    for directory in (ROOT / "internal", ROOT / "prototype/trace"):
        for p in directory.rglob("*"):
            if not p.is_file() or ".sdk" in p.parts:
                continue
            if p.suffix == ".go" or p.name in ("go.mod", "go.sum", "LICENSE", "NOTICE", "sdk-policy.patch"):
                files.append(p)
    return {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}


def build(output, modules):
    output, modules = output.resolve(), modules.resolve(strict=True)
    receipt, source = verify_sdk(), inputs()
    output.mkdir(mode=0o700)
    mounts = {ROOT / "go.mod": "/workspace/go.mod", ROOT / "go.sum": "/workspace/go.sum",
              ROOT / "LICENSE": "/workspace/LICENSE", ROOT / "NOTICE": "/workspace/NOTICE",
              ROOT / "internal": "/workspace/internal", ROOT / "prototype/trace": "/workspace/prototype/trace",
              modules: "/gomod"}
    command = ["docker", "run", "--rm", "--network=none", "--cap-drop=ALL",
               "--security-opt=no-new-privileges", "--read-only", "--cpus=4", "--memory=6g",
               "--pids-limit=256", "--ulimit", "core=0", "--user", f"{os.getuid()}:{os.getgid()}",
               "--tmpfs", "/tmp:rw,noexec,nosuid,size=3g,mode=1777",
               "--tmpfs", "/build:rw,exec,nosuid,size=2g,mode=1777"]
    for host, container in mounts.items():
        command += ["--mount", f"type=bind,source={host},target={container},readonly"]
    command += ["--mount", f"type=bind,source={output},target=/output",
                "-e", "GOMODCACHE=/gomod", "-e", "GOCACHE=/tmp/gocache", "-e", "GOTMPDIR=/build",
                "-e", "GOPROXY=off", "-e", "GOTOOLCHAIN=local", "-e", "CGO_ENABLED=0",
                "-e", "GOOS=linux", "--workdir=/workspace/prototype/trace/worker", BUILDER,
                "sh", "-ec", '''
go mod verify
for architecture in arm64 amd64; do
  for attempt in 1 2; do
    GOARCH="$architecture" go build -mod=readonly -trimpath -buildvcs=false -ldflags=-buildid= -o "/output/worker-$architecture-$attempt" ./cmd/memlens-filecache-worker
  done
done
''']
    with (output / "build.log").open("xb") as log:
        subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True)
    if inputs() != source or verify_sdk() != receipt:
        raise ValueError("source changed during worker build")
    workers = {}
    for architecture in ("arm64", "amd64"):
        first, second = output / f"worker-{architecture}-1", output / f"worker-{architecture}-2"
        digest = sha(first)
        if sha(second) != digest or not 0 < first.stat().st_size <= 128 << 20:
            raise ValueError("worker reproduction or executable size failed")
        workers[architecture] = {"sha256": digest, "bytes": first.stat().st_size,
                                 "first": first.name, "repeat": second.name}
    record = {"status": "unapproved build candidate", "builder": BUILDER,
              "sdkReceiptSHA256": receipt, "sourceSHA256": source, "workers": workers}
    (output / "build.json").write_text(json.dumps(record, indent=2) + "\n")
    print("Reproduced Linux arm64 and amd64 workers; no programme was loaded.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--module-cache", required=True, type=Path)
    args = parser.parse_args()
    build(args.output, args.module_cache)
