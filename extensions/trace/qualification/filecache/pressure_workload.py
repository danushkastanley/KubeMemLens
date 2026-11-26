"""Own a ready pressure fixture while a controller runs independent observers."""
import os
import select
import time

from gated import GatedWorkload
from verify_pressure import integer, require, validate_pressure


class PressureWorkload:
    def __init__(self, command, seconds):
        integer(seconds, 1, 1800)
        self.seconds, self.started, self.raw = seconds, None, None
        self.gated = GatedWorkload(command)
        if self.gated.buffer:
            self.gated.close()
            raise ValueError('pressure producer emitted data before its start command')

    @property
    def process(self):
        return self.gated.process

    def start(self):
        require(self.started is None and self.process.poll() is None, 'pressure start state changed')
        self.started = time.monotonic()
        self.process.stdin.write(b'R')
        self.process.stdin.flush()

    def collect(self):
        require(self.started is not None and self.raw is None, 'pressure collection state changed')
        deadline = self.started + self.seconds + 10
        chunks, size, lines = [], 0, 0
        while lines < self.seconds + 1:
            remaining = deadline - time.monotonic()
            require(remaining > 0 and select.select([self.process.stdout], [], [], remaining)[0],
                    'pressure report deadline expired')
            block = os.read(self.process.stdout.fileno(), 65536)
            require(block and size + len(block) <= 1 << 20, 'pressure output absent or oversized')
            chunks.append(block)
            size += len(block)
            lines += block.count(b'\n')
        raw = b''.join(chunks).decode()
        result = validate_pressure(raw, self.seconds)
        require(self.process.poll() is None, 'pressure producer lost its gated lifetime')
        self.raw = raw
        return result

    def finish(self):
        require(self.raw is not None, 'pressure report must be validated before completion')
        self.gated.finish()

    def close(self):
        # As with GatedWorkload, a remote exec client is not the workload owner.
        # On failure the outer controller must also tear down its owned fixture.
        self.gated.close()
