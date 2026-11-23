"""Read again when the collector sheds load, as its response asks."""

import time

from common import ContractError

# The collector answers a read beyond its concurrency limit with HTTP 429 and
# `Retry-After: 1`. A refused read is the documented response to load, not a
# failed measurement, so a qualification read waits that long and tries again.
# The attempts are bounded: a collector that keeps refusing still fails the run.
RETRY_AFTER_SECONDS = 1
ATTEMPTS = 3


class RateLimited(ContractError):
    pass


def read_with_retry_after(read):
    for _ in range(ATTEMPTS - 1):
        try:
            return read()
        except RateLimited:
            time.sleep(RETRY_AFTER_SECONDS)
    return read()
