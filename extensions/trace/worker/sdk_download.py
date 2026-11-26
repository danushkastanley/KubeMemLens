"""Fetch pinned module metadata within one bounded preparation window."""

import json
import subprocess
import time

TRANSIENT = ("i/o timeout", "tls handshake timeout", "context deadline exceeded",
             "connection reset", "connection refused", "unexpected eof",
             "temporary failure", "no such host", "429 too many requests",
             "500 internal server error", "502 bad gateway", "503 service unavailable",
             "504 gateway timeout")


def transient_failure(output, stderr):
    text = (output + stderr).lower()
    if "checksum mismatch" in text or "security error" in text:
        return False
    return any(reason in text for reason in TRANSIENT)


def download(module, directory, environment):
    # Preserve the original total bound; retries share it rather than multiply it.
    deadline = time.monotonic() + 180
    for attempt in range(3):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        try:
            result = subprocess.run(["go", "mod", "download", "-json", module],
                                    cwd=directory, env=environment, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    timeout=remaining, check=False)
        except subprocess.TimeoutExpired:
            raise ValueError("SDK module download exceeded its preparation deadline") from None
        if result.returncode == 0:
            document = json.loads(result.stdout)
            if not isinstance(document, dict) or document.get("Error"):
                raise ValueError("SDK module download returned invalid metadata")
            return document
        if not transient_failure(result.stdout, result.stderr):
            raise ValueError("SDK module download failed; integrity or unclassified failure")
        if attempt < 2:
            time.sleep(min(5, max(0, deadline - time.monotonic())))
    raise ValueError("SDK module download unavailable within the bounded attempts")
