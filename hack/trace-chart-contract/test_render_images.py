"""Refuse a substituted image in each real Helm-rendered Pod, including hooks."""

import re
import subprocess
import sys
from pathlib import Path


def main():
    folder = Path(sys.argv[1])
    expected = sys.argv[2]
    original = (folder / "enabled.yaml").read_text()
    pattern = re.compile(r'(?m)^(\s+image: )"' + re.escape(expected) + r'"$')
    matches = list(pattern.finditer(original))
    if len(matches) != 6:
        raise SystemExit("expected one API, two node and three preflight images")
    substitutions = ["example.invalid/other@sha256:" + "f" * 64, "example.invalid/trace:latest"]
    for index, match in enumerate(matches):
        for replacement in substitutions:
            mutated = folder / "substituted.yaml"
            mutated.write_text(original[:match.start()] + match[1] + '"' + replacement + '"' + original[match.end():])
            result = subprocess.run(
                [str(folder.resolve() / "verify"), str(folder / "disabled.yaml"), str(mutated),
                 "trace-admin", expected],
                capture_output=True, timeout=10, check=False,
            )
            if result.returncode != 1 or result.stderr != b"workload image does not match the exact release image\n":
                raise SystemExit(f"image substitution was not rejected at workload {index}")
    print("all six workload image substitutions rejected (other digest and mutable tag)")
    cases = [
        (original + '\n---\napiVersion: v1\nkind: Pod\nmetadata: {name: hidden, namespace: trace-admin}\n',
         b"unexpected rendered resource identity\n"),
        (original.replace('namespace: "trace-admin"', 'namespace: "other"', 1),
         b"unexpected rendered resource scope or duplicate\n"),
        (original + original, b"unexpected rendered resource scope or duplicate\n"),
    ]
    elevated, count = re.subn(r'(roleRef:\n  apiGroup: rbac.authorization.k8s.io\n  kind: ClusterRole\n  name: )[^\n]+',
                              r'\g<1>cluster-admin', original, count=1)
    if count != 1:
        raise SystemExit("expected one trace cluster binding")
    cases.append((elevated, b"unexpected trace binding or tenant grant\n"))
    aggregated = original.replace('kind: ClusterRole\n',
        'kind: ClusterRole\naggregationRule:\n  clusterRoleSelectors:\n    - matchLabels: {privilege: escalated}\n', 1)
    if aggregated == original:
        raise SystemExit("expected a trace cluster role")
    cases.append((aggregated, b"trace role does not match reviewed permissions\n"))
    for content, error in cases:
        mutated = folder / "substituted.yaml"
        mutated.write_text(content)
        result = subprocess.run(
            [str(folder.resolve() / "verify"), str(folder / "disabled.yaml"), str(mutated),
             "trace-admin", expected],
            capture_output=True, timeout=10, check=False,
        )
        if result.returncode != 1 or result.stderr != error:
            raise SystemExit("resource inventory substitution was not rejected")
    print("hidden workload, wrong namespace, duplicate resource and widened RBAC rejected")


if __name__ == "__main__":
    main()
