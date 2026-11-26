#!/usr/bin/env python3
"""Split the trace module's Go packages between its two checks (ADR 0031).

"make check-trace-preflight" gates every pull request and tests every package
of the prototype/trace module except the qualification harnesses below
prototype/trace/qualification/. "make check-trace-qualification" tests those
harnesses when their paths change and on a schedule.

Both lists come from one "go list ./..." in the module, so a new package lands
on the per-pull-request side unless it is under the harness prefix. The package
at prototype/trace/qualification itself stays on the per-pull-request side: its
test checks the seccomp profile the trace chart installs.

Usage: trace_packages.py preflight | qualification | check
"""
import subprocess
import sys

MODULE_DIR = "prototype/trace"
HARNESS_DIR = "qualification"


def go(*args):
    result = subprocess.run(["go", "-C", MODULE_DIR, *args], check=True,
                            stdout=subprocess.PIPE, text=True)
    return result.stdout.split()


def split(module, packages):
    prefix = f"{module}/{HARNESS_DIR}/"
    preflight = [package for package in packages if not package.startswith(prefix)]
    harnesses = [package for package in packages if package.startswith(prefix)]
    return preflight, harnesses


def problems(module, packages, harness_tree):
    """Name every way the split fails to partition the module.

    harness_tree is "go list ./qualification/..." in the module: a second,
    independent derivation of the harness side, so that an edit to the prefix
    logic cannot move or drop a package without failing here.
    """
    preflight, harnesses = split(module, packages)
    found = []
    if not harnesses:
        found.append(f"no package of {MODULE_DIR} is under {HARNESS_DIR}/; "
                     "check-trace-qualification would test nothing")
    for package in sorted(set(packages) - set(preflight) - set(harnesses)):
        found.append(f"{package} is tested by neither check")
    for package in sorted(set(preflight) & set(harnesses)):
        found.append(f"{package} is tested by both checks")
    expected = set(harness_tree) - {f"{module}/{HARNESS_DIR}"}
    for package in sorted(expected ^ set(harnesses)):
        found.append(f"{package}: harness split disagrees with go list ./{HARNESS_DIR}/...")
    return found


def main(argv):
    if len(argv) != 2 or argv[1] not in ("preflight", "qualification", "check"):
        print(__doc__.strip().splitlines()[-1], file=sys.stderr)
        return 2
    module = go("list", "-m")[0]
    packages = go("list", "./...")
    found = problems(module, packages, go("list", f"./{HARNESS_DIR}/..."))
    for problem in found:
        print(f"trace package split: {problem}", file=sys.stderr)
    if found:
        return 1
    preflight, harnesses = split(module, packages)
    if argv[1] != "check":
        print("\n".join(preflight if argv[1] == "preflight" else harnesses))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
