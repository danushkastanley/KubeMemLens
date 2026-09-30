"""Own one pinned production-session helper and bounded private evidence files."""
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess

from transport import QualificationError, strict_object


def private_file(path, data):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'wb') as output:
        output.write(data)


def bounded_file(path, maximum):
    with path.open('rb') as source:
        data = source.read(maximum+1)
    if len(data) > maximum:
        raise QualificationError('qualification evidence exceeds bound')
    return data


class RunningTrace:
    def __init__(self, binary, expected_sha256, configuration, directory):
        binary, directory = Path(binary).resolve(), Path(directory).resolve()
        if not binary.is_file() or not re.fullmatch(r'[a-f0-9]{64}', expected_sha256):
            raise QualificationError('qualification client digest mismatch')
        digest = hashlib.sha256()
        with binary.open('rb') as source:
            for block in iter(lambda: source.read(1 << 20), b''):
                digest.update(block)
        if digest.hexdigest() != expected_sha256:
            raise QualificationError('qualification client digest mismatch')
        encoded = json.dumps(configuration, allow_nan=False).encode()
        if len(encoded) > 4096:
            raise QualificationError('qualification configuration exceeds bound')
        directory.mkdir(mode=0o700, exist_ok=False)
        self.directory, self.process, self.forced = directory, None, False
        private_file(directory/'config.private.json', encoded)
        private_file(directory/'report.json', b'')
        private_file(directory/'progress.private.log', b'')
        with (directory/'report.json').open('wb') as output, (directory/'progress.private.log').open('wb') as progress:
            self.process = subprocess.Popen([str(binary), '--config', str(directory/'config.private.json')],
                                            stdin=subprocess.DEVNULL, stdout=output, stderr=progress)

    def __repr__(self):
        return '[private running qualification trace]'

    def progress(self):
        data = bounded_file(self.directory/'progress.private.log', 4096)
        admitted, metadata = None, False
        for line in data.splitlines(keepends=True):
            if not line.endswith(b'\n'):
                continue
            if line == b'local isolation qualification failed\n':
                continue
            value = strict_object(line)
            if value.get('type') == 'admitted' and set(value) == {'type', 'id'} and not admitted:
                if not isinstance(value['id'], str) or not re.fullmatch(r'[a-f0-9]{32}', value['id']):
                    raise QualificationError('invalid private admission handle')
                admitted = value['id']
            elif value == {'type': 'metadata'} and admitted and not metadata:
                metadata = True
            else:
                raise QualificationError('invalid qualification progress sequence')
        return admitted, metadata

    def finish(self):
        try:
            code = self.process.wait(timeout=70)
        except subprocess.TimeoutExpired:
            self.close()
            raise QualificationError('qualification client deadline exceeded') from None
        data = bounded_file(self.directory/'report.json', 32768)
        value = strict_object(data)
        if value.get('schemaVersion') != 1 or value.get('kind') != 'TraceReport' or value.get('redacted') is not True:
            raise QualificationError('invalid redacted qualification report')
        # Nonzero remains visible: expiry can have valid evidence but an already
        # absent API handle, while an external census separately proves cleanup.
        return {'exitCode': code, 'forcedTermination': self.forced, 'report': value}

    def cancel(self):
        if self.process.poll() is None:
            self.process.send_signal(signal.SIGINT)

    def close(self):
        if self.process is None or self.process.poll() is not None:
            return
        self.cancel()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.forced = True
            self.process.kill()
            self.process.wait(timeout=3)
