"""Control a fixed-seed workload already started before a trace's observation window."""
import json
import os
import select
import subprocess
import time


class GatedWorkload:
    """The caller must supply an immutable, independently verified fixture command."""
    def __init__(self, command):
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL)
        self.buffer = b''
        try:
            if self.line() != {'ready': True}:
                raise RuntimeError('gated workload readiness is invalid')
        except BaseException:
            self.close()
            raise

    def line(self):
        deadline = time.monotonic()+10
        while b'\n' not in self.buffer:
            remaining = deadline-time.monotonic()
            if remaining <= 0 or not select.select([self.process.stdout], [], [], remaining)[0]:
                raise RuntimeError('gated workload response deadline exceeded')
            block = os.read(self.process.stdout.fileno(), 4096)
            if not block or len(self.buffer)+len(block) > 4096:
                raise RuntimeError('gated workload response is absent or oversized')
            self.buffer += block
        line, self.buffer = self.buffer.split(b'\n', 1)
        try:
            value = json.loads(line)
        except (ValueError, UnicodeError):
            raise RuntimeError('gated workload response is invalid') from None
        if not isinstance(value, dict):
            raise RuntimeError('gated workload response is not an object')
        return value

    def run(self):
        self.process.stdin.write(b'R')
        self.process.stdin.flush()
        return self.line()

    def finish(self):
        self.process.stdin.write(b'Q')
        self.process.stdin.flush()
        self.process.stdin.close()
        self.process.stdin = None
        output, _ = self.process.communicate(timeout=10)
        if self.process.returncode != 0 or output or self.buffer:
            raise RuntimeError('gated workload completion failed')

    def close(self):
        if self.process.stdin is not None:
            self.process.stdin.close()
            self.process.stdin = None
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=3)
        if self.process.stdout is not None:
            self.process.stdout.close()
