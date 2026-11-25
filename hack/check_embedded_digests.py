#!/usr/bin/env python3
"""Report container image digests that Dependabot cannot see.

Dependabot updates Dockerfiles and `uses:` lines. It does not read an image
reference inside a workflow `run:` step, a shell script or a chart template.
This script finds references of the form name:tag@sha256:digest in those
files and asks the registry what the tag points at now. A different digest
means the pin is out of date. A reference with no tag cannot be checked and is
listed so that someone looks at it.

Run by .github/workflows/embedded-digests.yml on a schedule. It needs Docker
with buildx and network access; the unit tests do not.
"""
import json
import re
import subprocess
import sys

SCANNED = (".github/workflows/", "charts/", "hack/", "Makefile")
# Dockerfiles are Dependabot's. Tests and test data hold made-up pins.
SKIPPED = ("Dockerfile", "/testdata/", "hack/test_", "hack/test-", "hack/check_embedded_digests.py")
PINNED = re.compile(r"(?P<name>[a-z0-9][a-z0-9./_-]*[a-z0-9])(?::(?P<tag>[A-Za-z0-9._-]+))?@(?P<digest>sha256:[a-f0-9]{64})")


def references(text):
    """Return the distinct (name, tag, digest) pins in text; tag may be None."""
    return sorted({(m["name"], m["tag"], m["digest"]) for m in PINNED.finditer(text)},
                  key=lambda pin: (pin[0], pin[1] or "", pin[2]))


def scanned(path):
    return path.startswith(SCANNED) and not any(part in path for part in SKIPPED)


def registry_digest(name, tag):
    """Digest the registry serves for name:tag, or None when it cannot be read."""
    try:
        result = subprocess.run(
            ["docker", "buildx", "imagetools", "inspect", f"{name}:{tag}", "--format", "{{json .Manifest}}"],
            capture_output=True, text=True, timeout=120, check=False)
    except subprocess.TimeoutExpired:
        return None
    if result.returncode != 0:
        return None
    try:
        return json.loads(result.stdout)["digest"]
    except (ValueError, KeyError, TypeError):
        return None


def classify(pins, resolve):
    """Split {(name, tag, digest): [paths]} into current, stale, unreadable and untagged."""
    report = {"current": [], "stale": [], "unreadable": [], "untagged": []}
    for (name, tag, digest), paths in sorted(pins.items(), key=lambda item: (item[0][0], item[0][1] or "")):
        entry = {"image": name, "tag": tag, "pinned": digest, "paths": sorted(paths)}
        if tag is None:
            report["untagged"].append(entry)
            continue
        entry["registry"] = resolve(name, tag)
        if entry["registry"] is None:
            report["unreadable"].append(entry)
        elif entry["registry"] == digest:
            report["current"].append(entry)
        else:
            report["stale"].append(entry)
    return report


def collect(paths, read):
    pins = {}
    for path in paths:
        if not scanned(path):
            continue
        for pin in references(read(path)):
            pins.setdefault(pin, []).append(path)
    return pins


def main():
    tracked = subprocess.run(["git", "ls-files", "-z"], capture_output=True, check=True).stdout.decode().split("\0")

    def read(path):
        with open(path, encoding="utf-8", errors="replace") as handle:
            return handle.read()

    report = classify(collect([path for path in tracked if path], read), registry_digest)
    for entry in report["stale"]:
        print(f"STALE {entry['image']}:{entry['tag']} is pinned to {entry['pinned']} but the registry serves "
              f"{entry['registry']}; update {', '.join(entry['paths'])}")
    for entry in report["unreadable"]:
        print(f"UNREADABLE {entry['image']}:{entry['tag']} could not be read from its registry; "
              f"pinned in {', '.join(entry['paths'])}")
    for entry in report["untagged"]:
        print(f"UNTAGGED {entry['image']}@{entry['pinned']} has no tag, so it cannot be checked; "
              f"review {', '.join(entry['paths'])} by hand")
    print(f"{len(report['current'])} current, {len(report['stale'])} stale, "
          f"{len(report['unreadable'])} unreadable, {len(report['untagged'])} untagged")
    return 1 if report["stale"] or report["unreadable"] else 0


if __name__ == "__main__":
    sys.exit(main())
