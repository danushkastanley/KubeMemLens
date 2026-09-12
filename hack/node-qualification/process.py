"""Bound subprocess streams before they can fill memory or expose diagnostics."""

import os
import selectors
import signal
import subprocess
import tempfile
import time

from common import ContractError, require


def execute(argv, data=None, timeout=12, maximum=16 * 1024 * 1024, environment=None):
    require(data is None or isinstance(data, bytes) and len(data) <= 512 * 1024,
            "command input exceeds its bound")
    with tempfile.TemporaryFile() as source:
        if data:
            source.write(data)
        source.seek(0)
        try:
            child = subprocess.Popen(argv, stdin=source, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, start_new_session=True, env=environment)
        except OSError as error:
            raise ContractError("qualification command could not start") from error
        output, diagnostics = bytearray(), bytearray()
        started = time.monotonic()
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(child.stdout, selectors.EVENT_READ, (output, maximum))
                selector.register(child.stderr, selectors.EVENT_READ, (diagnostics, 64 * 1024))
                while selector.get_map():
                    remaining = timeout - (time.monotonic() - started)
                    require(remaining > 0, "qualification command timed out")
                    for key, _ in selector.select(min(remaining, 0.2)):
                        block = os.read(key.fileobj.fileno(), 65536)
                        if not block:
                            selector.unregister(key.fileobj)
                            continue
                        target, limit = key.data
                        require(len(target) + len(block) <= limit, "qualification command output exceeded its bound")
                        target.extend(block)
            remaining = timeout - (time.monotonic() - started)
            require(remaining > 0, "qualification command timed out")
            try:
                result = child.wait(timeout=remaining)
            except subprocess.TimeoutExpired as error:
                raise ContractError("qualification command timed out") from error
            require(result == 0, "qualification command failed")
            return output.decode("utf-8")
        finally:
            # Do not signal a reaped PID: the OS could have reused it. Until wait
            # reaps this child, its process-group identity belongs to this call.
            try:
                if child.returncode is None:
                    try:
                        os.killpg(child.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    try:
                        child.wait(timeout=2)
                    except subprocess.TimeoutExpired as error:
                        raise ContractError("qualification command cleanup is pending") from error
            finally:
                child.stdout.close()
                child.stderr.close()
