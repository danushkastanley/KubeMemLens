"""Run the exact OOM hook functions in a native, capability-free test harness.

Kernel helpers are test doubles. This verifies control flow and privacy ordering,
not kernel verifier acceptance, attachment behaviour or helper implementation.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

HERE = Path(__file__).resolve().parent
PROGRAMMES = HERE.parents[1] / "programmes/filecache"


def run(image, output):
    if not image.startswith("sha256:") or len(image) != 71:
        raise ValueError("use an immutable local compiler image ID")
    output.mkdir(mode=0o700)
    sources = {"oom.bpf.c": PROGRAMMES / "oom.bpf.c",
               "common.h": HERE / "semantics_common.h", "test.c": HERE / "semantics_test.c"}
    for name, path in sources.items():
        shutil.copyfile(path, output / name)
    command = ["docker", "run", "--rm", "--network=none", "--cap-drop=ALL",
               "--security-opt=no-new-privileges", "--read-only", "--cpus=2",
               "--memory=512m", "--pids-limit=64", "--user", f"{os.getuid()}:{os.getgid()}",
               "--tmpfs", "/tmp:rw,exec,nosuid,size=128m,mode=1777",
               "--mount", f"type=bind,source={output},target=/source,readonly", image,
               "sh", "-ec", "gcc -std=c11 -Wall -Wextra -Werror -O2 /source/test.c -o /tmp/test && /tmp/test"]
    result = subprocess.run(command, capture_output=True, text=True, timeout=30)
    (output / "test.log").write_text(result.stdout + result.stderr)
    result.check_returncode()
    record = {"compilerImage": image, "sourceSHA256": {
        name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in sources.items()},
        "result": "passed", "kernelLoad": False, "kernelHelpers": "native test doubles"}
    (output / "result.json").write_text(json.dumps(record, indent=2) + "\n")
    print(result.stdout.strip())


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--compiler-image", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    run(args.compiler_image, args.output.resolve())
