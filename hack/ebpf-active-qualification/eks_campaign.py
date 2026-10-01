"""Run a full frozen active case on an explicitly approved disposable EKS host."""
import argparse
import os
from pathlib import Path
import signal

from campaign import Campaign, PROFILES
from eks_case import EKSCase
from preflight import read_configuration


class EKSActiveCampaign(Campaign):
    execution_scope = 'owned EKS host workload; this case does not establish full provider qualification'

    def make_case(self, cfg):
        return EKSCase(cfg)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--case', choices=tuple(PROFILES), required=True)
    parser.add_argument('--acknowledge-owned-eks-host', action='store_true', required=True)
    args = parser.parse_args()
    os.umask(0o077)
    cfg = read_configuration(args.config)
    profile = PROFILES[args.case]()
    # Same setup/restoration allowance used by the certificate lifetime check.
    deadline = profile['pairs'] * (2 * (profile['windowSeconds'] + profile['warmupSeconds']) + 600) + 600
    def interrupted(_signal, _frame):
        raise InterruptedError('EKS active campaign interrupted; restore owned resources')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(deadline)
    try:
        EKSActiveCampaign(cfg, args.output, profile).run()
    finally:
        signal.alarm(0)


if __name__ == '__main__':
    main()
