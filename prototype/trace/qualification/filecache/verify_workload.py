"""Verify the local I/O fixture without tracing; remove only owned test resources."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import uuid

from gated import GatedWorkload


FILE_BYTES = 8 * 1024 * 1024


def expected_observation(data, mode):
    read = {"prepare": 0, "cached": 1, "uncached": 1, "write": 0, "noise": 4}[mode]
    write = {"prepare": 1, "cached": 0, "uncached": 0, "write": 1, "noise": 4}[mode]
    page = data.get("pageBytes")
    if type(page) is not int or page not in (4096, 65536):
        raise ValueError("unsupported fixture page size")
    before = 0 if mode in ("prepare", "uncached") else FILE_BYTES // page
    return {"mode": mode, "fileBytes": FILE_BYTES, "pageBytes": page,
            "residentPagesBefore": before, "residentPagesAfter": FILE_BYTES // page,
            "readBytes": read * FILE_BYTES, "writeBytes": write * FILE_BYTES}


def require_observation(data, expected):
    if (any(type(data.get(key)) is not int for key in expected if key != "mode")
            or data != expected):
        raise ValueError("fixture did not establish its I/O/cache contract")


def validate_observation(data, mode):
    require_observation(data, expected_observation(data, mode))


def validate_timed_observation(data, mode):
    expected = expected_observation(data, mode)
    start, end, elapsed = (data.get(key) for key in (
        "operationStartedMonotonicNanos", "operationEndedMonotonicNanos", "operationNanos"))
    if (any(type(value) is not int or not 0 < value < 2**64 for value in (start, end, elapsed))
            or end - start != elapsed or elapsed >= 10_000_000_000):
        raise ValueError("invalid operation clock evidence")
    expected.update(operationStartedMonotonicNanos=start,
                    operationEndedMonotonicNanos=end, operationNanos=elapsed)
    require_observation(data, expected)


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

    def workload(*args):
        return run("/usr/local/bin/kml-io-workload", *args)

    results = []
    try:
        for idle_mode in ("idle", "paired-idle"):
            idle = subprocess.run(common + ["--detach", "--entrypoint", "/usr/local/bin/kml-io-workload", image, idle_mode],
                                  capture_output=True, text=True, check=True, timeout=15).stdout.strip()
            if not re.fullmatch(r"[a-f0-9]{64}", idle):
                raise RuntimeError("idle fixture identity unavailable")
            try:
                state = json.loads(subprocess.check_output(["docker", "inspect", idle], text=True, timeout=15))[0]
                if state["Id"] != idle or not state["State"]["Running"]:
                    raise RuntimeError("idle fixture did not remain running")
                subprocess.run(["docker", "exec", idle, "/bin/sh", "-ec", "test ! -e /work/fixed-seed.bin"],
                               capture_output=True, check=True, timeout=15)
                logs = subprocess.run(["docker", "logs", idle], capture_output=True, check=True, timeout=15)
                if logs.stdout or logs.stderr:
                    raise RuntimeError("idle fixture emitted unexpected output")
                results.append({"mode": idle_mode, "running": True, "fixtureFileAbsent": True, "outputEmpty": True})
            finally:
                subprocess.run(["docker", "rm", "-f", idle], check=True,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
        for mode in ("prepare", "cached", "uncached", "cached", "write", "noise"):
            result = workload(mode)
            if result.returncode != 0 or result.stderr:
                raise RuntimeError(f"fixture mode {mode} failed")
            data = json.loads(result.stdout)
            validate_observation(data, mode)
            page = data["pageBytes"]
            results.append(data)

        # Persistent process, deterministic absolute deadlines, buffered reports.
        from verify_series import validate_series
        for mode in ("cached", "uncached", "write", "noise"):
            result = workload("series", mode, "3", "500")
            if result.returncode != 0 or result.stderr:
                raise RuntimeError(f"fixture series {mode} failed")
            rows = validate_series(result.stdout, mode, 3, 500)
            results.append({"series": mode, "records": rows})
        for args in (("series",), ("series", "prepare", "1", "100"),
                     ("series", "cached", "0", "100"),
                     ("series", "cached", "1801", "100"),
                     ("series", "cached", "1", "99"),
                     ("series", "cached", "1", "10001"),
                     ("series", "cached", "1800", "1001"),
                     ("series", "cached", "-1", "100"),
                     ("series", "cached", "99999999999999999999", "100")):
            result = workload(*args)
            if (result.returncode != 2 or result.stdout or result.stderr not in (
                    "workload failed: series arguments\n", "workload failed: series mode\n",
                    "workload failed: series schedule bound\n")):
                raise RuntimeError("unbounded or invalid series was not rejected")
        results.append({"negativeCase": "series arguments and schedule ceilings", "passed": True})
        for mode in ("cached", "uncached"):
            gated = GatedWorkload(common + ["--interactive", "--entrypoint",
                                  "/usr/local/bin/kml-io-workload", image, mode, "--gated"])
            try:
                if gated.process.poll() is not None:
                    raise RuntimeError("gated fixture exited before its command")
                receipt = gated.run()
                expected = {"mode": mode, "fileBytes": FILE_BYTES, "pageBytes": page,
                            "residentPagesBefore": 0 if mode == "uncached" else FILE_BYTES // page,
                            "residentPagesAfter": FILE_BYTES // page,
                            "readBytes": FILE_BYTES, "writeBytes": 0}
                if receipt != expected or gated.process.poll() is not None:
                    raise RuntimeError("gated fixture contract or lifetime failed")
                gated.finish()
                results.append({"gated": True, "receipt": receipt, "retainedUntilCompletion": True})
            finally:
                gated.close()

        rejected = subprocess.run(common + ["--interactive", "--entrypoint",
                                  "/usr/local/bin/kml-io-workload", image, "cached", "--gated"],
                                  input="X", capture_output=True, text=True, timeout=15)
        if (rejected.returncode != 2 or rejected.stdout != '{"ready":true}\n' or
                rejected.stderr != "workload failed: bounded command\n"):
            raise RuntimeError("invalid gated command was not rejected")
        results.append({"negativeCase": "invalid gated command", "passed": True})

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
