"""Read-only release dry run from a clean, committed checkout on Linux arm64.

Generates only ephemeral local keys and public development artefacts. It cannot
publish, request OIDC credentials, install a chart or load a BPF programme.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import subprocess
import sys

from assemble import assemble
from build_image import build as build_image
from build_launchers import build as build_launchers
from finalise import finalise
from manifest import VERSION, canonical, match, require
from package_chart import prepare as package_chart

UPSTREAM = 'e5a2855f270ca6557f4bd7e4fabaddf6760d8f50'


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            digest.update(block)
    return digest.hexdigest()


def command(args, cwd, *, timeout=120, environment=None):
    subprocess.run([str(arg) for arg in args], cwd=cwd, env=environment, check=True, timeout=timeout)


def capture(args, cwd):
    return subprocess.check_output(args, cwd=cwd, text=True, timeout=30).strip()


def clean_source(source):
    require(capture(['git', 'status', '--porcelain', '--untracked-files=normal'], source) == '',
            'dry run requires a clean committed checkout')


def run(source, output, modules, version, inspector, syft, cosign):
    require(platform.system() == 'Linux' and platform.machine() in ('aarch64', 'arm64'),
            'the reviewed BPF builder requires a native Linux arm64 host')
    require(match(VERSION, version) and '-dev.' in version, 'dry run is development-only')
    clean_source(source)
    commit = capture(['git', 'rev-parse', 'HEAD'], source)
    epoch = int(capture(['git', 'show', '-s', '--format=%ct', 'HEAD'], source))
    output.mkdir(mode=0o700)
    public = output/'public'; public.mkdir(mode=0o700)
    keys = output/'private'; keys.mkdir(mode=0o700)
    programme_key, engine_key = keys/'programmes.pem', keys/'engine.pem'
    environment = dict(os.environ, COSIGN_PASSWORD=secrets.token_urlsafe(32))
    # Tokens are not needed by local signing, and never enter the build containers.
    signing_environment = {key: environment[key] for key in ('PATH', 'COSIGN_PASSWORD')}
    signing_environment.update(HOME=str(keys), TMPDIR=str(keys), XDG_CACHE_HOME=str(keys/'cache'))
    environment.pop('COSIGN_PASSWORD')
    try:
        upstream = output/'upstream'
        command(['git', 'init', '-q', upstream], source)
        command(['git', '-C', upstream, 'fetch', '--depth=1', 'https://github.com/inspektor-gadget/inspektor-gadget', UPSTREAM], source)
        command(['git', '-C', upstream, 'checkout', '--detach', UPSTREAM], source)
        require(capture(['git', 'rev-parse', 'HEAD'], upstream) == UPSTREAM, 'upstream commit differs')
        command([sys.executable, 'prototype/trace/worker/prepare_sdk.py'], source)
        for module in (source, source/'prototype/trace', source/'prototype/trace/worker'):
            command(['go', 'mod', 'download'], module, timeout=300)
            command(['go', 'mod', 'verify'], module, timeout=120)
        clean_source(source)
        workers, launchers = output/'workers', output/'launchers'
        command([sys.executable, source/'prototype/trace/worker/build_worker.py', '--output', workers,
                 '--module-cache', modules], source, timeout=900)
        build_launchers(source, launchers, modules)
        reproduced = output/'reproduced-programmes'
        command([sys.executable, source/'prototype/trace/programmes/build_filecache.py', '--upstream', upstream,
                 '--output', reproduced, '--set', 'all'], source, timeout=600)
        for key in (programme_key, engine_key):
            command(['go', '-C', 'prototype/trace', 'run', './cmd/build-filecache-bundle', 'keygen', '--key', key], source)
        programmes = output/'programmes'
        command(['go', '-C', 'prototype/trace', 'run', './cmd/build-filecache-bundle', 'build', '--key', programme_key,
                 '--build', reproduced, '--sdk-patch', source/'prototype/trace/worker/sdk-policy.patch', '--output', programmes], source)
        worker = json.loads((workers/'build.json').read_bytes())
        sign_engine = ['go', '-C', 'prototype/trace', 'run', './cmd/sign-trace-engine', '--key', engine_key,
                       '--output', output/'verification']
        for architecture in ('amd64', 'arm64'):
            record = worker['workers'][architecture]
            sign_engine += ['--'+architecture, workers/record['first'], '--'+architecture+'-sha256', record['sha256']]
        command(sign_engine, source)
        programme_key.unlink(); engine_key.unlink()
        assembled, images = output/'assembled', output/'images'
        assemble(source, workers, launchers, programmes, reproduced, output/'verification', upstream, assembled, epoch)
        build_image(assembled, images, version, commit, epoch)
        image = json.loads((images/'receipt.json').read_bytes())
        digest = image['images'][0]['indexDigest']
        chart = output/'chart'
        package_chart(source/'charts/kube-memlens-trace', chart, version, digest, epoch)
        payloads = output/'payloads'; payloads.mkdir(mode=0o700)
        paths = {'chart': chart/('kube-memlens-trace-'+version+'.tgz'), 'engine': assembled/'trace-engine.tar',
                 'image': images/image['images'][0]['name'], 'programmes': assembled/'trace-programmes.tar'}
        request = {'schemaVersion': 1, 'releaseVersion': version, 'sourceCommit': commit, 'imageDigest': digest, 'payloads': []}
        for role, path in paths.items():
            shutil.copyfile(path, payloads/path.name)
            request['payloads'].append({'role': role, 'name': path.name, 'size': path.stat().st_size, 'sha256': sha(path)})
        request_path = output/'request.json'; request_path.write_bytes(canonical(request))
        evidence = {'scope': 'development build-only CI dry run', 'applicationSourceCommit': commit,
                    'workers': worker, 'launchers': json.loads((launchers/'build.json').read_bytes()),
                    'programmes': json.loads((reproduced/'build.json').read_bytes()), 'image': image,
                    'buildToolSources': {str(path.relative_to(source)): sha(path)
                                         for path in sorted((source/'hack/trace-release').glob('*.py'))},
                    'kernelLoaded': False, 'published': False}
        evidence_path = output/'evidence.json'; evidence_path.write_bytes(canonical(evidence))
        bundle = public/'bundle'
        finalise(payloads, request_path, evidence_path, bundle, (inspector, sha(inspector)), (syft, sha(syft)))
        command([cosign, 'generate-key-pair', '--output-key-prefix', keys/'local'], source, environment=signing_environment)
        command([cosign, 'signing-config', 'create', '--out', keys/'signing.json'], source, environment=signing_environment)
        require('https://' not in (keys/'signing.json').read_text(), 'local signing must not use remote services')
        command([cosign, 'sign-blob', '--yes', '--key', keys/'local.key', '--bundle', bundle/'trace-release.sigstore.json',
                 '--signing-config', keys/'signing.json', bundle/'trace-release.json'], source, environment=signing_environment)
        (keys/'local.key').unlink()
        shutil.copyfile(keys/'local.pub', public/'local.pub')
        summary = {'schemaVersion': 1, 'sourceCommit': commit, 'releaseVersion': version,
                   'manifestSHA256': sha(bundle/'trace-release.json'), 'publicKeySHA256': sha(public/'local.pub'),
                   'imageDigest': digest, 'indexReproduced': image['indexReproduced'],
                   'archivesIdentical': image['archivesIdentical'], 'authority': 'local-development',
                   'published': False, 'candidateExecuted': False}
        (public/'build-summary.json').write_bytes(canonical(summary))
        clean_source(source)
        print('Development bundle ready for a separate clean consumer; no release authority granted.')
    finally:
        for key in (programme_key, engine_key, keys/'local.key'):
            key.unlink(missing_ok=True)
        signing_environment.pop('COSIGN_PASSWORD', None)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('source', 'output', 'module-cache', 'inspector', 'syft', 'cosign'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    run(args.source.resolve(), args.output.resolve(), args.module_cache.resolve(), args.version,
        args.inspector.resolve(), args.syft.resolve(), args.cosign.resolve())
