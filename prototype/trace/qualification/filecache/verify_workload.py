"""Verify the local I/O fixture without tracing; remove only owned test resources."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import uuid


FILE_BYTES = 8 * 1024 * 1024


def verify(image):
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image):
        raise ValueError("an immutable local image ID is required")
    # Resolve before creating resources. Never pull a tag or change credentials.
    subprocess.run(["docker", "image", "inspect", image], check=True,
                   stdout=subprocess.DEVNULL)
    volume = "kml-bpf005-io-" + uuid.uuid4().hex
    container = volume + "-run"
    subprocess.run(["docker", "volume", "create", "--label",
                    "kube-memlens.qualification=bpf005-filecache", volume],
                   check=True, stdout=subprocess.DEVNULL)
    common = ["docker", "run", "--rm", "--name", container, "--network=none",
              "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only",
              "--cpus=1", "--memory=64m", "--pids-limit=16", "--ulimit", "core=0",
              "--user", "65532:65532", "--mount",
              f"type=volume,source={volume},target=/work"]

    def run(entrypoint, *args):
        return subprocess.run(common + ["--entrypoint", entrypoint, image, *args],
                              capture_output=True, text=True, timeout=15)

    def workload(mode):
        return run("/usr/local/bin/kml-io-workload", mode)

    results = []
    try:
        for mode in ("prepare", "cached", "uncached", "cached", "write", "noise"):
            result = workload(mode)
            if result.returncode != 0 or result.stderr:
                raise RuntimeError(f"fixture mode {mode} failed")
            data = json.loads(result.stdout)
            read = {"prepare": 0, "cached": 1, "uncached": 1, "write": 0, "noise": 4}[mode]
            write = {"prepare": 1, "cached": 0, "uncached": 0, "write": 1, "noise": 4}[mode]
            page = data["pageBytes"]
            if page not in (4096, 65536):
                raise RuntimeError("unsupported fixture page size")
            before = FILE_BYTES // page
            if mode in ("prepare", "uncached"):
                before = 0
            expected = {"mode": mode, "fileBytes": FILE_BYTES, "pageBytes": page,
                        "residentPagesBefore": before,
                        "residentPagesAfter": FILE_BYTES // page,
                        "readBytes": read * FILE_BYTES, "writeBytes": write * FILE_BYTES}
            if data != expected:
                raise RuntimeError(f"fixture mode {mode} did not establish its I/O/cache contract")
            results.append(data)

        result = workload("prepare")
        if (result.returncode != 2 or result.stdout or
                result.stderr != "workload failed: open owned fixture\n"):
            raise RuntimeError("fixture replacement was not rejected")
        results.append({"negativeCase": "existing fixture replacement", "passed": True})

        # Corrupt one byte of the test-owned file without changing its length.
        result = run("/bin/sh", "-ec",
                     "printf x | dd of=/work/fixed-seed.bin bs=1 conv=notrunc 2>/dev/null")
        if result.returncode != 0:
            raise RuntimeError("could not prepare corrupt fixture")
        result = workload("cached")
        if (result.returncode != 2 or result.stdout or
                result.stderr != "workload failed: data integrity\n"):
            raise RuntimeError("corrupted fixture was not rejected")
        results.append({"negativeCase": "corrupted bytes", "passed": True})
    finally:
        # Also stop the exact owned container if a CLI timeout left it running.
        subprocess.run(["docker", "rm", "-f", container],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
        subprocess.run(["docker", "volume", "rm", volume], check=True,
                       stdout=subprocess.DEVNULL, timeout=15)
    return {"status": "fixture verified; no incident tracing attempted", "image": image,
            "capabilities": "all dropped", "network": "none", "ownedVolumeRemoved": True,
            "results": results}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    # Reserve a new evidence file before running, never overwrite earlier results.
    with args.output.open("x") as output:
        result = verify(args.image)
        json.dump(result, output, indent=2)
        output.write("\n")
    print("Verified workload I/O, cache residency and failure cases; owned resources removed.")
