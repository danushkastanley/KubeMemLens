#!/usr/bin/env python3
"""Classify NUL-delimited Git paths for local-kind integration checks."""
from pathlib import Path
import fnmatch
import sys

PREFIXES = ("cmd/", "internal/", "charts/kube-memlens/", "hack/lib/",
            "hack/scale-profiles/", "hack/fixtures/", "hack/kind-profiles/")
EXACT = {"Dockerfile", "go.mod", "go.sum", ".github/workflows/ci.yml",
         ".github/workflows/node-context.yml",
         "hack/ci_scope.py", "hack/test_ci_scope.py", "hack/e2e-kind.sh",
         "hack/e2e-tui-kind.sh", "hack/soak-live-density.sh",
         "hack/observe_kind_telemetry.py", "hack/measure-tui-latency.exp",
         "hack/tui-smoke.exp"}


# Documentation inside a source prefix that never reaches a running cluster.
# The chart README ships in the chart package but changes no rendered object.
IGNORED = {"charts/kube-memlens/README.md"}

# Opt-in kind verifiers that no CI lane runs, so the verify-*-kind.sh pattern
# must not match them: starting every lane would test nothing they changed.
# Each maps to the names a workflow would have to mention to run it: the
# script or make target, and the hack/e2e-kind.sh flag that gates it.
# hack/test_ci_scope.py fails when a workflow mentions one; remove the entry
# then, so that a change to the script runs the lanes again.
OPT_IN_VERIFIERS = {
    "hack/verify-evidence-discovery-kind.sh": ("verify-evidence-discovery-kind",
                                               "E2E_RUN_EVIDENCE_DISCOVERY_SMOKE"),
    "hack/verify-auth-architecture-kind.sh": ("verify-auth-architecture-kind",),
}


def needs_kind(paths):
    return any(path not in IGNORED and path not in OPT_IN_VERIFIERS
               and (path in EXACT or path.startswith(PREFIXES)
                    or (Path(path).parent == Path("hack")
                        and fnmatch.fnmatchcase(Path(path).name, "verify-*-kind.sh")))
               for path in paths)


if __name__ == "__main__":
    paths = Path(sys.argv[1]).read_bytes().decode("utf-8", "surrogateescape").split("\0")
    print("true" if needs_kind(paths) else "false")
