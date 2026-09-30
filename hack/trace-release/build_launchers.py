"""Reproduce trace launchers and collect package licences from trusted source.

Build-only: no signing, candidate execution, installation or kernel access.
The source checkout and prepared SDK are caller-owned, never release-bundle inputs.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def build(source, output, modules):
    source, output, modules = source.resolve(strict=True), output.resolve(), modules.resolve(strict=True)
    worker = source/'prototype/trace/worker'
    sys.path.insert(0, str(worker))
    spec = importlib.util.spec_from_file_location('worker_builder', worker/'build_worker.py')
    builder = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(builder)
    sdk, inputs = builder.verify_sdk(), builder.inputs()
    script = sha(Path(__file__))
    output.mkdir(mode=0o700)
    mounts = {source/'go.mod': '/workspace/go.mod', source/'go.sum': '/workspace/go.sum',
              source/'LICENSE': '/workspace/LICENSE', source/'NOTICE': '/workspace/NOTICE',
              source/'internal': '/workspace/internal', source/'prototype/trace': '/workspace/prototype/trace',
              modules: '/gomod'}
    cidfile = output/'container.id'
    command = ['docker', 'run', '--rm', '--cidfile', str(cidfile), '--network=none', '--cap-drop=ALL',
               '--security-opt=no-new-privileges', '--read-only', '--cpus=4', '--memory=6g',
               '--pids-limit=256', '--ulimit', 'core=0', '--user', f'{os.getuid()}:{os.getgid()}',
               '--tmpfs', '/tmp:rw,noexec,nosuid,size=3g,mode=1777',
               '--tmpfs', '/build:rw,exec,nosuid,size=2g,mode=1777']
    for host, container in mounts.items():
        command += ['--mount', f'type=bind,source={host},target={container},readonly']
    command += ['--mount', f'type=bind,source={output},target=/output',
                '-e', 'GOMODCACHE=/gomod', '-e', 'GOCACHE=/tmp/gocache', '-e', 'GOTMPDIR=/build',
                '-e', 'GOPROXY=off', '-e', 'GOTOOLCHAIN=local', '-e', 'CGO_ENABLED=0', '-e', 'GOOS=linux',
                '--workdir=/workspace/prototype/trace', builder.BUILDER, 'sh', '-ec', '''
go mod verify
go -C worker mod verify
go build -mod=readonly -trimpath -o /build/licencebundle ./licencebundle
for architecture in arm64 amd64; do
  GOARCH="$architecture" /build/licencebundle --output "/output/licences-launcher-$architecture"
  (cd worker; GOARCH="$architecture" /build/licencebundle --package ./cmd/memlens-filecache-worker --output "/output/licences-worker-$architecture")
done
# Only this container's disposable cache is cleared between build stages.
go clean -cache
for architecture in arm64 amd64; do
  go clean -cache
  GOARCH="$architecture" go list -mod=readonly -deps ./cmd/memlens-trace > "/output/dependencies-$architecture.txt"
  for attempt in 1 2; do
    GOARCH="$architecture" go build "$@" -o "/output/memlens-trace-$architecture-$attempt" ./cmd/memlens-trace
  done
done
''', 'launcher-build', *builder.BUILD_FLAGS]
    try:
        with (output/'build.log').open('xb') as log:
            subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=900)
    except (subprocess.SubprocessError, KeyboardInterrupt):
        # Stop only the container whose ID Docker wrote for this invocation.
        if cidfile.exists():
            identity = cidfile.read_text().strip()
            if len(identity) == 64 and all(character in '0123456789abcdef' for character in identity):
                subprocess.run(['docker', 'rm', '-f', identity], stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, timeout=30, check=False)
        raise
    if builder.inputs() != inputs or builder.verify_sdk() != sdk or sha(Path(__file__)) != script:
        raise ValueError('source changed during launcher reproduction')
    artefacts = {}
    for architecture in ('amd64', 'arm64'):
        first, repeat = (output/f'memlens-trace-{architecture}-{attempt}' for attempt in (1, 2))
        if sha(first) != sha(repeat) or not 0 < first.stat().st_size <= 128 << 20:
            raise ValueError('launcher reproduction failed')
        artefacts[architecture] = {'sha256': sha(first), 'bytes': first.stat().st_size,
                                   'first': first.name, 'repeat': repeat.name,
                                   'dependenciesSHA256': sha(output/f'dependencies-{architecture}.txt')}
    (output/'build.json').write_text(json.dumps({'builder': builder.BUILDER, 'buildFlags': builder.BUILD_FLAGS,
        'sourceSHA256': inputs, 'sdkReceiptSHA256': sdk, 'scriptSHA256': script,
        'artifacts': artefacts, 'bpfLoaded': False}, indent=2)+'\n')
    print('Reproduced Linux amd64/arm64 launchers and retained package-derived licences; no BPF loaded.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--module-cache', required=True, type=Path)
    args = parser.parse_args()
    build(args.source, args.output, args.module_cache)
