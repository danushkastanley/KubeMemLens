"""Caller-pinned tool copies with bounded, credential-free local execution."""
from dataclasses import dataclass
import hashlib
import os
from pathlib import Path
import resource
import subprocess
import tempfile

from bundle import open_directory, open_regular
from manifest import ManifestError, SHA256, match, require


@dataclass(frozen=True)
class Tool:
    path: Path
    sha256: str

    @classmethod
    def copy(cls, source, digest, private, name):
        source, private = Path(source), Path(private)
        require(source.is_absolute() and match(SHA256, digest), 'invalid tool identity')
        descriptor = open_directory(source.parent)
        target = private / name
        actual = hashlib.sha256()
        size = 0
        try:
            with open_regular(descriptor, source.name, 256 << 20) as incoming, target.open('xb') as outgoing:
                for block in iter(lambda: incoming.read(1 << 20), b''):
                    size += len(block)
                    require(size <= 256 << 20, 'tool exceeds byte bound')
                    actual.update(block)
                    outgoing.write(block)
        except OSError:
            raise ManifestError('tool copy failed') from None
        finally:
            os.close(descriptor)
        require(actual.hexdigest() == digest, 'tool identity mismatch')
        target.chmod(0o700)
        return cls(target, digest)

    def run(self, arguments, private, data=b'', maximum=4 << 20, timeout=60, working_directory=None, scratch_file_limit=None):
        """Only exit 0/1 are meaningful; timeout, signals and output excess fail.

        The supported verifier hosts are Linux/macOS. RLIMIT_FSIZE bounds every
        child file. Scanners can request a separate bounded scratch-file ceiling;
        returned stdout/stderr must still satisfy the smaller report limit.
        """
        private = Path(private)
        file_limit = maximum if scratch_file_limit is None else scratch_file_limit
        require(type(file_limit) is int and maximum <= file_limit <= 256 << 20,
                'invalid child file bound')
        environment = {'PATH': '/usr/bin:/bin', 'HOME': str(private),
                       'TMPDIR': str(private), 'XDG_CACHE_HOME': str(private/'cache'),
                       'HELM_CACHE_HOME': str(private/'helm-cache'),
                       'HELM_CONFIG_HOME': str(private/'helm-config'),
                       'HELM_DATA_HOME': str(private/'helm-data'),
                       'GOMAXPROCS': '2'}

        def output_bound():
            resource.setrlimit(resource.RLIMIT_FSIZE, (file_limit, file_limit))

        with tempfile.TemporaryFile(dir=private) as output, tempfile.TemporaryFile(dir=private) as errors:
            try:
                result = subprocess.run([str(self.path), *arguments], input=data,
                                        cwd=working_directory or private, env=environment, stdout=output, stderr=errors,
                                        timeout=timeout, check=False, preexec_fn=output_bound)
            except (OSError, subprocess.TimeoutExpired):
                raise ManifestError('bounded tool execution failed') from None
            require(result.returncode in (0, 1), 'tool terminated abnormally')
            require(output.tell() < maximum and errors.tell() < maximum, 'tool output exceeds bound')
            output.seek(0)
            return result.returncode, output.read(maximum)
