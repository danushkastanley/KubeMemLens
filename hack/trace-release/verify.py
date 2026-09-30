#!/usr/bin/env python3
"""Read-only optional trace bundle verification; never installs a candidate."""
import argparse
from pathlib import Path
import sys

from consumer import verify_bundle
from manifest import ManifestError, canonical
from signatures import GitHubTrust, LocalTrust


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bundle', type=Path, required=True)
    parser.add_argument('--snapshot', type=Path, required=True, help='new private directory; must not exist')
    parser.add_argument('--version', required=True)
    parser.add_argument('--source-commit', required=True)
    trust = parser.add_mutually_exclusive_group(required=True)
    trust.add_argument('--local-key', type=Path)
    trust.add_argument('--github-trusted-root', type=Path)
    parser.add_argument('--trust-sha256', required=True)
    for name in ('cosign', 'inspector', 'helm', 'chart-checker'):
        parser.add_argument('--'+name, type=Path, required=True, help='absolute caller-owned tool path')
        parser.add_argument('--'+name+'-sha256', required=True)
    args = parser.parse_args()
    selected = (LocalTrust(args.local_key, args.trust_sha256, args.version, args.source_commit)
                if args.local_key else GitHubTrust(args.github_trusted_root, args.trust_sha256,
                                                   args.version, args.source_commit))
    tools = {name: (getattr(args, name.replace('-', '_')), getattr(args, name.replace('-', '_')+'_sha256'))
             for name in ('cosign', 'inspector', 'helm', 'chart-checker')}
    try:
        result = verify_bundle(args.bundle, args.snapshot, tools, selected)
    except (ManifestError, OSError):
        # Do not echo source paths, credentials or arbitrary tool diagnostics.
        print('trace bundle verification failed; snapshot remains unverified', file=sys.stderr)
        return 1
    sys.stdout.buffer.write(canonical(result))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
