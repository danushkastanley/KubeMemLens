"""Compile the candidate file/cache programmes twice without loading BPF.

Uses only the accepted arm64 builder platform and pinned upstream headers. Output
must be a new private directory. Neither this command nor its container loads,
attaches, signs, approves or publishes a programme.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

BUILDER = "ghcr.io/inspektor-gadget/gadget-builder@sha256:d55d33bfd2583e721ac78972a8039fb4b207d157e06f0c623c5f8a55cee597b4"
UPSTREAM = "e5a2855f270ca6557f4bd7e4fabaddf6760d8f50"
SOURCE = Path(__file__).resolve().parent / "filecache"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def clean_upstream(path):
    revision = subprocess.check_output(["git", "-C", str(path), "rev-parse", "HEAD"], text=True).strip()
    changed = subprocess.check_output(["git", "-C", str(path), "status", "--porcelain"], text=True)
    if revision != UPSTREAM or changed:
        raise ValueError("requires the clean accepted upstream source revision")


def build(upstream, output):
    clean_upstream(upstream)
    output.mkdir(mode=0o700)
    shutil.copytree(SOURCE, output / "source")
    inputs = {p.name: digest(p) for p in sorted((output / "source").iterdir()) if p.is_file()}
    headers = {str(p.relative_to(upstream)): digest(p) for p in sorted((upstream / "include").rglob("*.h"))}
    prefix = ["docker", "run", "--rm", "--platform", "linux/arm64", "--network=none",
              "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only",
              "--cpus=2", "--memory=1g", "--pids-limit=64", f"--user={os.getuid()}:{os.getgid()}",
              "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m,mode=1777",
              "-v", f"{upstream}:/upstream:ro", "-v", f"{output / 'source'}:/src:ro"]
    objects = []
    for arch, target in (("amd64", "x86"), ("arm64", "arm64")):
        for name in ("files", "cache"):
            hashes = []
            for repeat in (1, 2):
                directory = output / f"{name}-{arch}-{repeat}"
                directory.mkdir(mode=0o700)
                command = prefix + ["-v", f"{directory}:/out:rw", "--workdir=/src", "--entrypoint=clang", BUILDER,
                                    "-target", "bpfel", "-mcpu=v3", "-Wall", "-Werror", "-g", "-O2",
                                    "-I", f"/upstream/include/gadget/{arch}", "-I", "/upstream/include",
                                    "-I", "/usr/include/aarch64-linux-gnu", "-D", f"__TARGET_ARCH_{target}",
                                    "-c", f"{name}.bpf.c", "-o", "/out/program.bpf.o"]
                subprocess.run(command, check=True, timeout=120)
                subprocess.run(prefix + ["-v", f"{directory}:/out:rw", "--entrypoint=llvm-strip", BUILDER,
                                         "-g", "/out/program.bpf.o"], check=True, timeout=30)
                hashes.append(digest(directory / "program.bpf.o"))
            if hashes[0] != hashes[1]:
                raise ValueError("programme build is not reproducible")
            objects.append({"kind": name, "architecture": arch, "sha256": hashes[0],
                            "path": f"{name}-{arch}-1/program.bpf.o", "reproduced": True})
    clean_upstream(upstream)
    if any(digest(upstream / p) != sha for p, sha in headers.items()):
        raise ValueError("upstream headers changed during compilation")
    if any(digest(SOURCE / p) != sha for p, sha in inputs.items()):
        raise ValueError("programme source changed during compilation")
    (output / "build.json").write_text(json.dumps({
        "builder": BUILDER, "upstreamCommit": UPSTREAM, "sourceSHA256": inputs,
        "headerSHA256": headers, "objects": objects, "loaded": False,
        "approval": "not granted by this build", "isa": "bpfel v3",
    }, indent=2) + "\n")
    print("Four candidate objects reproduced without loading BPF.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--upstream", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    build(args.upstream.resolve(), args.output.resolve())
