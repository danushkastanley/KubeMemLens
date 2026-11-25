#!/usr/bin/env python3
"""Fuzz every Go fuzz target in the main module for a fixed time each.

Pull requests fuzz a handful of targets for ten seconds. The rest only replay
their seeds there. This runs all of them, one after another, so the nightly
workflow gives each target real fuzzing time.

A failing target leaves its input under testdata/fuzz in the package; the
output names the package, the target and that file.
"""
import argparse
import json
import subprocess
import sys


def targets():
    """Return sorted (package, target) pairs for every Fuzz function."""
    listed = subprocess.run(["go", "test", "-list", "^Fuzz", "-json", "./..."],
                            capture_output=True, text=True, check=False)
    found = set()
    for line in listed.stdout.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        name = event.get("Output", "").strip()
        if event.get("Action") == "output" and name.startswith("Fuzz") and name.isidentifier():
            found.add((event["Package"], name))
    if listed.returncode != 0:
        # A test package that does not build would otherwise be left out and
        # the run would still report success.
        sys.stderr.write(listed.stdout[-4000:] + listed.stderr[-4000:])
        raise SystemExit("could not list fuzz targets: a test package failed to build")
    return sorted(found)


def fuzz(package, target, seconds):
    command = ["go", "test", package, "-run=^$", f"-fuzz=^{target}$", f"-fuzztime={seconds}s",
               "-fuzzminimizetime=10s", "-parallel=2", f"-timeout={seconds + 240}s"]
    return subprocess.run(command, capture_output=True, text=True, check=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--seconds", type=int, default=30, help="fuzzing time for each target")
    parser.add_argument("--list", action="store_true", help="print the targets and exit")
    options = parser.parse_args()

    found = targets()
    if not found:
        raise SystemExit("no fuzz targets found: the listing is broken")
    if options.list:
        for package, target in found:
            print(f"{package} {target}")
        return 0
    failed = []
    for package, target in found:
        result = fuzz(package, target, options.seconds)
        if result.returncode == 0:
            print(f"PASS {package} {target}")
            continue
        failed.append((package, target))
        print(f"FAIL {package} {target}")
        print((result.stdout + result.stderr)[-4000:])
    print(f"{len(found) - len(failed)} of {len(found)} fuzz targets passed {options.seconds}s each")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
